//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// devicePollInterval: classic loopback is bound to one endpoint, so the default output device is polled and the stream
// reopened when it changes (as the spike; IMMNotificationClient would be the event-driven way).
const devicePollInterval = 2 * time.Second

// comEndpoint is one opened and started WASAPI capture client. Every acquired interface and handle has a cleanup,
// run in reverse order by close, so each is released exactly once on every path.
type comEndpoint struct {
	cleanups []func()

	event     windows.Handle
	client    unsafe.Pointer
	capture   unsafe.Pointer
	fmt       Format
	maxFrames uint32 // a packet longer than one second is refused (the endpoint buffer is 200 ms)
	note      string

	// Classic loopback only.
	enum     unsafe.Pointer // polls the default output device
	deviceID string
	keep     *keepAlive
	nextPoll time.Time
}

func (e *comEndpoint) push(f func()) { e.cleanups = append(e.cleanups, f) }

func (e *comEndpoint) close() {
	for i := len(e.cleanups) - 1; i >= 0; i-- {
		e.cleanups[i]()
	}
	e.cleanups = nil
}

func (e *comEndpoint) format() Format  { return e.fmt }
func (e *comEndpoint) warning() string { return e.note }

func (e *comEndpoint) wait(step time.Duration) error {
	if _, err := windows.WaitForSingleObject(e.event, uint32(step/time.Millisecond)); err != nil { //nolint:gosec // step is waitStep, 100 ms
		return fmt.Errorf("%w: wait for packets: %v", ErrDevice, err)
	}
	return nil
}

// errPacketSize means WASAPI reported a packet the stream refuses to copy.
var errPacketSize = fmt.Errorf("%w: packet larger than one second", ErrDeviceLost)

func (e *comEndpoint) read() (rawPacket, bool, error) {
	n, err := uint32Out("IAudioCaptureClient::GetNextPacketSize", e.capture, slotCaptureGetNextPacketSize)
	if err != nil || n == 0 {
		return rawPacket{}, false, err
	}

	data, frames, flags, h := captureGetBuffer(e.capture)
	switch {
	case h == hrBufferEmpty:
		return rawPacket{}, false, nil
	case h.failed():
		return rawPacket{}, false, hrError("IAudioCaptureClient::GetBuffer", h)
	case frames > e.maxFrames:
		_ = call("IAudioCaptureClient::ReleaseBuffer", e.capture, slotCaptureReleaseBuffer, 0)
		return rawPacket{}, false, errPacketSize
	}

	p := rawPacket{
		frames:        int(frames),
		silent:        flags&bufferFlagsSilent != 0 || data == nil,
		discontinuity: flags&bufferFlagsDiscontinuity != 0,
	}
	if !p.silent && frames > 0 {
		// The content of a silent packet is undefined; the child writes zeros for it.
		size := int(frames) * e.fmt.BlockAlign()
		p.data = make([]byte, size)
		copy(p.data, unsafe.Slice((*byte)(data), size)) //nolint:gosec // G103: view of the WASAPI packet, size from GetBuffer and bounded
	}
	if err := call("IAudioCaptureClient::ReleaseBuffer", e.capture, slotCaptureReleaseBuffer, uintptr(frames)); err != nil {
		return rawPacket{}, false, err
	}
	return p, frames > 0, nil
}

func (e *comEndpoint) service(now time.Time) (bool, error) {
	if e.keep != nil {
		if err := e.keep.topUp(); isDeviceLost(err) {
			return false, err
		} else if err != nil {
			e.keep = nil // a failing keep-alive only risks gaps in silence; the capture goes on
		}
	}
	if e.enum == nil || now.Before(e.nextPoll) {
		return false, nil
	}
	e.nextPoll = now.Add(devicePollInterval)
	id, err := defaultDeviceID(e.enum)
	return err != nil || id != e.deviceID, nil
}

func (e *comEndpoint) stop() error { return call("IAudioClient::Stop", e.client, slotClientStop) }

// openEndpoint opens target on the capture thread; process loopback when it names a process tree, else classic
// loopback. On an error everything acquired so far is released.
func (s *wasapiSource) openEndpoint(cancel <-chan struct{}, target Target) (endpoint, error) {
	e := &comEndpoint{}
	// The event is created first so that it is closed last, after every client that could signal it.
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: CreateEvent: %v", ErrDevice, err)
	}
	e.event = ev
	e.push(func() { _ = windows.CloseHandle(ev) })

	if target.Mode == ModeSystem {
		err = s.openClassic(e)
	} else {
		err = s.openProcess(e, target, cancel)
	}
	if err == nil {
		e.maxFrames = e.fmt.SampleRate
		err = startCapture(e)
	}
	if err != nil {
		e.close()
		return nil, err
	}
	return e, nil
}

// openClassic opens loopback on the default render endpoint in its mix format, plus the keep-alive.
func (s *wasapiSource) openClassic(e *comEndpoint) error {
	enum, err := s.procs.createDeviceEnumerator()
	if err != nil {
		return err
	}
	e.push(func() { release(enum) })
	e.enum = enum

	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return err
	}
	e.push(func() { release(dev) })
	if e.deviceID, err = deviceID(dev); err != nil {
		return err
	}

	client, err := activateAudioClient(dev)
	if err != nil {
		return err
	}
	e.push(func() { release(client) })
	e.client = client

	mix, f, err := mixFormat(client)
	if err != nil {
		return err
	}
	e.push(func() { windows.CoTaskMemFree(mix) })
	e.fmt = f

	if err := initialize(client, classicFlags, mix); err != nil {
		return err
	}

	// Without a render stream the loopback client gets no packets while nothing plays (OBS and GStreamer do this).
	k, err := startKeepAlive(e, dev, mix)
	if err != nil {
		e.note = "keep-alive render stream failed, silence may be missing: " + err.Error()
	}
	e.keep = k
	e.nextPoll = time.Now().Add(devicePollInterval)
	return nil
}

// openProcess activates process loopback and initializes it with the first format it accepts. GetMixFormat is
// E_NOTIMPL on this virtual device, so the format is chosen here. The formats are tried on the one activated client
// (the activation budget allows no second one); whether Initialize may be retried after a refused format is not
// confirmed, a failure then falls back to classic loopback.
func (s *wasapiSource) openProcess(e *comEndpoint, target Target, cancel <-chan struct{}) error {
	client, err := s.activateProcessLoopback(target, cancel)
	if err != nil {
		return err
	}
	e.push(func() { release(client) })
	e.client = client

	var errs []error
	for _, f := range processFormats() {
		wfx := encodeWaveFormat(f)
		err := initialize(client, processFlags, unsafe.Pointer(&wfx[0])) //nolint:gosec // G103: WAVEFORMATEX passed to Initialize
		runtime.KeepAlive(wfx)
		if err == nil {
			e.fmt = f
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", f, err))
	}
	return errors.Join(errs...)
}

// startCapture attaches the event, gets the capture client and starts the stream.
func startCapture(e *comEndpoint) error {
	if err := call("IAudioClient::SetEventHandle", e.client, slotClientSetEventHandle, uintptr(e.event)); err != nil {
		return err
	}
	capture, err := getService(e.client, iidIAudioCaptureClient(), "IAudioCaptureClient")
	if err != nil {
		return err
	}
	e.push(func() { release(capture) })
	e.capture = capture

	if err := call("IAudioClient::Start", e.client, slotClientStart); err != nil {
		return err
	}
	client := e.client
	e.push(func() { _ = call("IAudioClient::Stop", client, slotClientStop) })
	return nil
}

// keepAlive is a render stream on the loopback endpoint that plays silence.
type keepAlive struct {
	client unsafe.Pointer
	render unsafe.Pointer
	frames uint32
}

// startKeepAlive starts the keep-alive; its cleanups go to e, so a failure halfway is released with the endpoint.
func startKeepAlive(e *comEndpoint, dev, mix unsafe.Pointer) (*keepAlive, error) {
	client, err := activateAudioClient(dev)
	if err != nil {
		return nil, err
	}
	e.push(func() { release(client) })

	if err := initialize(client, 0, mix); err != nil {
		return nil, err
	}
	frames, err := uint32Out("IAudioClient::GetBufferSize(keep-alive)", client, slotClientGetBufferSize)
	if err != nil {
		return nil, err
	}
	render, err := getService(client, iidIAudioRenderClient(), "IAudioRenderClient")
	if err != nil {
		return nil, err
	}
	e.push(func() { release(render) })

	k := &keepAlive{client: client, render: render, frames: frames}
	if err := k.topUp(); err != nil {
		return nil, err
	}
	if err := call("IAudioClient::Start(keep-alive)", client, slotClientStart); err != nil {
		return nil, err
	}
	e.push(func() { _ = call("IAudioClient::Stop(keep-alive)", client, slotClientStop) })
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
