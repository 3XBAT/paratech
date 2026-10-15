//go:build windows && (amd64 || arm64)

package wasapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// devicePollInterval: classic loopback is bound to one endpoint, so the default endpoint id is polled and the
	// stream reopened when it changes. IMMNotificationClient (OnDefaultDeviceChanged) is the event-driven
	// alternative; choosing between the two is left to TASK-017.
	devicePollInterval = 2 * time.Second

	classicFlags = streamFlagsLoopback | streamFlagsEventCallback
	processFlags = streamFlagsLoopback | streamFlagsEventCallback | streamFlagsAutoConvertPCM | streamFlagsSrcDefaultQuality
)

// reopenBackoff bounds how long a lost stream is retried before the capture fails.
func reopenBackoff() []time.Duration {
	return []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
}

// Options configures the opener.
type Options struct {
	Log *slog.Logger // status events; nil discards them
}

// opener keeps every object Windows may still reference (completion handlers, activation parameters) reachable
// for its own lifetime, which is the lifetime of the process in the spike.
type opener struct {
	log *slog.Logger

	mu   sync.Mutex
	kept []any
}

// NewOpener returns the Windows capture opener.
func NewOpener(opts Options) Opener {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	o := &opener{log: log}
	return o.open
}

func (o *opener) keep(objs ...any) {
	o.mu.Lock()
	o.kept = append(o.kept, objs...)
	o.mu.Unlock()
}

func (o *opener) open(ctx context.Context, spec Spec, sink Sink) (Stream, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	s := &stream{
		o:    o,
		spec: spec,
		sink: sink,
		log:  o.log.With("mode", spec.String()),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	started := make(chan error, 1)
	go s.run(started)

	select {
	case err := <-started:
		if err != nil {
			<-s.done
			return nil, err
		}
		return s, nil
	case <-ctx.Done():
		// Every wait inside run checks stop at least every waitStepMS, so this returns soon.
		s.requestStop()
		<-s.done
		return nil, ctx.Err()
	}
}

// stream runs one capture on its own locked OS thread.
type stream struct {
	o    *opener
	spec Spec
	sink Sink
	log  *slog.Logger

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}

	mu     sync.Mutex
	format Format
	err    error

	packets, silent, frames, discontinuities, emptyWaits, reopens atomic.Uint64

	scratch []byte // capture goroutine only
}

func (s *stream) Spec() Spec { return s.spec }

func (s *stream) Format() Format {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.format
}

func (s *stream) Stats() Stats {
	return Stats{
		Packets: s.packets.Load(), SilentPackets: s.silent.Load(), Frames: s.frames.Load(),
		Discontinuities: s.discontinuities.Load(), EmptyWaits: s.emptyWaits.Load(), Reopens: s.reopens.Load(),
	}
}

func (s *stream) Done() <-chan struct{} { return s.done }

func (s *stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *stream) Stop(ctx context.Context) error {
	s.requestStop()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wasapi: capture goroutine did not stop: %w", ctx.Err())
	}
}

func (s *stream) requestStop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *stream) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// run owns the OS thread and every COM object of the stream. COM objects are created, used and released on this
// thread only, and CoUninitialize runs after the last release.
func (s *stream) run(started chan<- error) {
	defer close(s.done)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninit, err := coInitialize()
	if err != nil {
		started <- err
		return
	}
	defer uninit()

	sess, err := s.openSession()
	if err == nil {
		err = s.announce(sess)
	}
	if err != nil {
		sess.close()
		started <- err
		return
	}
	started <- nil

	err = s.loop(sess)
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// announce passes the stream format to the sink before the first frame.
func (s *stream) announce(sess *session) error {
	if err := s.sink.SetFormat(sess.format); err != nil {
		return fmt.Errorf("wasapi: sink: %w", err)
	}
	s.mu.Lock()
	s.format = sess.format
	s.mu.Unlock()
	s.log.Info("stream started", "format", sess.format.String(), "device", sess.deviceID, "keepalive", sess.keep != nil)
	return nil
}

// loop moves packets to the sink until stop, reopening the stream when the device is lost or (classic loopback)
// the default endpoint changed. It returns nil after a stop.
func (s *stream) loop(sess *session) error {
	defer func() { sess.close() }()

	nextPoll := time.Now().Add(devicePollInterval)
	for !s.stopping() {
		ev, err := windows.WaitForSingleObject(sess.event, waitStepMS)
		if err != nil {
			return fmt.Errorf("wasapi: WaitForSingleObject: %w", err)
		}

		got, err := s.drain(sess)
		if err == nil && got == 0 && ev == uint32(windows.WAIT_TIMEOUT) {
			s.emptyWaits.Add(1)
		}
		if err == nil && sess.keep != nil {
			if kerr := sess.keep.topUp(); isDeviceLost(kerr) {
				err = kerr
			} else if kerr != nil {
				s.log.Warn("keep-alive stopped", "hresult", hrText(kerr), "error", kerr.Error())
				sess.keep = nil
			}
		}

		changed := false
		if err == nil && sess.enum != nil && time.Now().After(nextPoll) {
			nextPoll = time.Now().Add(devicePollInterval)
			id, perr := defaultDeviceID(sess.enum)
			if changed = perr != nil || id != sess.deviceID; changed {
				s.log.Warn("default render endpoint changed", "old", sess.deviceID, "new", id, "error", errText(perr))
			}
		}

		if err != nil && !isDeviceLost(err) {
			return err
		}
		if err != nil || changed {
			s.log.Warn("reopening stream", "reason", reason(err, changed))
			sess.close()
			if sess, err = s.reopen(); err != nil {
				if errors.Is(err, errStopped) {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

func reason(err error, changed bool) string {
	if err != nil {
		if hr, ok := HRESULTOf(err); ok {
			return hr.String()
		}
		return err.Error()
	}
	if changed {
		return "default endpoint changed"
	}
	return ""
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reopen retries openSession with a bounded backoff. It always returns a session that can be closed.
func (s *stream) reopen() (*session, error) {
	var last error
	for i, d := range reopenBackoff() {
		select {
		case <-s.stop:
			return &session{}, errStopped
		case <-time.After(d):
		}

		sess, err := s.openSession()
		if err == nil {
			err = s.announce(sess)
		}
		if err == nil {
			s.reopens.Add(1)
			return sess, nil
		}
		sess.close()
		last = err
		hr, _ := HRESULTOf(err)
		s.log.Warn("reopen failed", "attempt", i+1, "hresult", hr.String(), "error", err.Error())
	}
	return &session{}, fmt.Errorf("wasapi: reopen failed after %d attempts: %w", len(reopenBackoff()), last)
}

// drain moves every pending packet to the sink and returns the number of frames.
func (s *stream) drain(sess *session) (int, error) {
	total := 0
	for {
		n, err := uint32Out("IAudioCaptureClient::GetNextPacketSize", sess.capture, slotCaptureGetNextPacketSize)
		if err != nil || n == 0 {
			return total, err
		}

		data, frames, flags, hr := captureGetBuffer(sess.capture)
		if hr == hrBufferEmpty {
			return total, nil
		}
		if hr.Failed() {
			return total, hrError("IAudioCaptureClient::GetBuffer", hr)
		}

		size := int(frames) * sess.format.BlockAlign()
		if cap(s.scratch) < size {
			s.scratch = make([]byte, size)
		}
		buf := s.scratch[:size]
		if flags&bufferFlagsSilent != 0 || data == nil {
			// The buffer content of a silent packet is undefined; it must be read as zeros.
			clear(buf)
			s.silent.Add(1)
		} else {
			copy(buf, unsafe.Slice((*byte)(data), size)) //nolint:gosec // G103: view of the WASAPI packet, size from GetBuffer
		}
		if flags&bufferFlagsDiscontinuity != 0 {
			s.discontinuities.Add(1)
		}

		if err := call("IAudioCaptureClient::ReleaseBuffer", sess.capture, slotCaptureReleaseBuffer, uintptr(frames)); err != nil {
			return total, err
		}
		s.packets.Add(1)
		s.frames.Add(uint64(frames))
		total += int(frames)

		// The sink runs on the capture thread; the product would hand frames to a writer goroutine instead.
		if err := s.sink.Write(buf); err != nil {
			return total, fmt.Errorf("wasapi: sink: %w", err)
		}
	}
}

// session is one opened stream. cleanups run in reverse order of acquisition.
type session struct {
	cleanups []func()

	event   windows.Handle
	client  unsafe.Pointer
	capture unsafe.Pointer
	format  Format

	enum     unsafe.Pointer // classic loopback only: polls the default endpoint
	deviceID string
	keep     *keepAlive
}

func (s *session) push(f func()) { s.cleanups = append(s.cleanups, f) }

func (s *session) close() {
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
	s.cleanups = nil
}

func (s *stream) openSession() (*session, error) {
	sess := &session{}

	// The event is created first so that it is closed last, after every client that could signal it.
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return sess, fmt.Errorf("wasapi: CreateEvent: %w", err)
	}
	sess.event = ev
	sess.push(func() { _ = windows.CloseHandle(ev) })

	if s.spec.Mode == ModeSystem {
		err = s.openClassic(sess)
	} else {
		err = s.openProcess(sess)
	}
	if err != nil {
		return sess, err
	}
	return sess, s.startCapture(sess)
}

// openClassic opens loopback on the default render endpoint in its mix format, plus the keep-alive.
func (s *stream) openClassic(sess *session) error {
	enum, err := createDeviceEnumerator()
	if err != nil {
		return err
	}
	sess.push(func() { release(enum) })
	sess.enum = enum

	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return err
	}
	sess.push(func() { release(dev) })
	if sess.deviceID, err = deviceID(dev); err != nil {
		return err
	}

	client, err := activateAudioClient(dev)
	if err != nil {
		return err
	}
	sess.push(func() { release(client) })
	sess.client = client

	mix, f, err := mixFormat(client)
	if err != nil {
		return err
	}
	sess.push(func() { windows.CoTaskMemFree(mix) })
	sess.format = f

	if err := initialize(client, classicFlags, mix); err != nil {
		return err
	}

	// Without a render stream the loopback client gets no packets while nothing plays (OBS, GStreamer do this).
	k, err := startKeepAlive(sess, dev, mix)
	if err != nil {
		hr, _ := HRESULTOf(err)
		s.log.Warn("keep-alive render stream failed, continuing without it", "hresult", hr.String(), "error", err.Error())
	}
	sess.keep = k
	return nil
}

// openProcess activates process loopback and initializes it with the first format it accepts. GetMixFormat is
// E_NOTIMPL on this virtual device, so the format is chosen here; each try uses a fresh activation.
func (s *stream) openProcess(sess *session) error {
	var errs []error
	for _, f := range processFormats() {
		client, err := s.o.activateProcessLoopback(s.spec, s.stop)
		if err != nil {
			s.log.Warn("process loopback activation failed", "hresult", hrText(err), "error", err.Error())
			return err
		}

		wfx := encodeWaveFormat(f)
		err = initialize(client, processFlags, unsafe.Pointer(&wfx[0])) //nolint:gosec // G103: WAVEFORMATEX passed to Initialize
		runtime.KeepAlive(wfx)
		if err == nil {
			sess.push(func() { release(client) })
			sess.client, sess.format = client, f
			return nil
		}
		release(client)
		s.log.Warn("process loopback format rejected", "format", f.String(), "hresult", hrText(err))
		errs = append(errs, fmt.Errorf("%s: %w", f, err))
	}
	return errors.Join(errs...)
}

func hrText(err error) string {
	if hr, ok := HRESULTOf(err); ok {
		return hr.String()
	}
	return ""
}

// startCapture attaches the event, gets the capture client and starts the stream.
func (s *stream) startCapture(sess *session) error {
	if err := call("IAudioClient::SetEventHandle", sess.client, slotClientSetEventHandle, uintptr(sess.event)); err != nil {
		return err
	}

	capture, err := getService(sess.client, &iidIAudioCaptureClient, "IAudioCaptureClient")
	if err != nil {
		return err
	}
	sess.push(func() { release(capture) })
	sess.capture = capture

	if err := call("IAudioClient::Start", sess.client, slotClientStart); err != nil {
		return err
	}
	client := sess.client
	sess.push(func() { _ = call("IAudioClient::Stop", client, slotClientStop) })
	return nil
}

// keepAlive is a render stream on the loopback endpoint that plays silence.
type keepAlive struct {
	client unsafe.Pointer
	render unsafe.Pointer
	frames uint32
}

func startKeepAlive(sess *session, dev, mix unsafe.Pointer) (*keepAlive, error) {
	client, err := activateAudioClient(dev)
	if err != nil {
		return nil, err
	}
	sess.push(func() { release(client) })

	if err := initialize(client, 0, mix); err != nil {
		return nil, err
	}
	frames, err := uint32Out("IAudioClient::GetBufferSize", client, slotClientGetBufferSize)
	if err != nil {
		return nil, err
	}
	render, err := getService(client, &iidIAudioRenderClient, "IAudioRenderClient")
	if err != nil {
		return nil, err
	}
	sess.push(func() { release(render) })

	k := &keepAlive{client: client, render: render, frames: frames}
	if err := k.topUp(); err != nil {
		return nil, err
	}
	if err := call("IAudioClient::Start(keep-alive)", client, slotClientStart); err != nil {
		return nil, err
	}
	sess.push(func() { _ = call("IAudioClient::Stop(keep-alive)", client, slotClientStop) })
	return k, nil
}

// topUp fills the free part of the render buffer with silence (AUDCLNT_BUFFERFLAGS_SILENT, no data written).
func (k *keepAlive) topUp() error {
	padding, err := uint32Out("IAudioClient::GetCurrentPadding(keep-alive)", k.client, slotClientGetCurrentPadding)
	if err != nil || padding >= k.frames {
		return err
	}
	n := k.frames - padding
	if err := renderGetBuffer(k.render, n); err != nil {
		return err
	}
	return call("IAudioRenderClient::ReleaseBuffer", k.render, slotRenderReleaseBuffer, uintptr(n), bufferFlagsSilent)
}

// coInitialize joins the MTA on the locked thread. S_FALSE (already initialized) still needs CoUninitialize.
func coInitialize() (func(), error) {
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	var errno syscall.Errno
	switch {
	case err == nil:
	case errors.As(err, &errno) && hresult(uintptr(errno)) == hrFalse:
	case errors.As(err, &errno):
		return nil, hrError("CoInitializeEx", hresult(uintptr(errno)))
	default:
		return nil, fmt.Errorf("wasapi: CoInitializeEx: %w", err)
	}
	return windows.CoUninitialize, nil
}
