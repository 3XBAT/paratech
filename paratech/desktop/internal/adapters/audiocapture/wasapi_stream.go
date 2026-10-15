package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// The WASAPI stream logic: the fallback chain, the reopen policy and the hand-over of packets to Read. It runs
// against the endpoint seam, so it is tested on every OS with fakes; the COM endpoints are in wasapi_*_windows.go.

const (
	// waitStep bounds every wait of the capture thread, so Stop and Close are seen within it.
	waitStep = 100 * time.Millisecond

	// stableAfter ends a reopen incident: a stream that fails again sooner keeps using the same reopen budget.
	stableAfter = 30 * time.Second

	// packetQueue is how many packets wait between the capture thread and Read (about 640 ms of 10 ms packets).
	packetQueue = 64

	// maxDrainPackets bounds one drain of the endpoint buffer, so the periodic duties run even if packets never stop.
	maxDrainPackets = 64

	// maxProcessActivations is the process loopback budget of a recorder process (see activationBudget).
	maxProcessActivations = 1
)

// reopenBackoff is the delay before each reopen try of one incident; when it is used up the capture fails.
func reopenBackoff() []time.Duration {
	return []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
}

var (
	// errStopped means a stop interrupted an open or a reopen.
	errStopped = errors.New("audiocapture: capture stopped")

	// errClosed means Close abandoned a packet nobody reads anymore.
	errClosed = errors.New("audiocapture: capture closed")
)

// endpoint is one opened and started capture client in one mode. Only the capture thread uses it.
type endpoint interface {
	format() Format

	// warning is a non-fatal problem of the open (the keep-alive render stream failed), "" when none.
	warning() string

	// wait blocks until the client signals new packets or step passed.
	wait(step time.Duration) error

	// read returns the next pending packet; ok is false when the buffer is empty.
	read() (p rawPacket, ok bool, err error)

	// service runs the periodic duties (keep-alive top-up, default device check) and reports a changed default
	// output device.
	service(now time.Time) (changed bool, err error)

	// stop stops the client before the final drain (IAudioClient::Stop).
	stop() error

	// close releases every interface and handle of the client, exactly once.
	close()
}

// rawPacket is a packet copied out of the endpoint buffer; data is nil for a silent packet.
type rawPacket struct {
	frames        int
	data          []byte
	silent        bool
	discontinuity bool
}

// streamDeps are the platform parts of a stream.
type streamDeps struct {
	// open opens target (ModeSystem: classic loopback); it gives up with errStopped soon after cancel closes.
	open func(cancel <-chan struct{}, target Target) (endpoint, error)

	// thread runs body on a locked OS thread joined to the COM MTA and returns an error if that setup failed (body
	// did not run then).
	thread func(body func()) error

	build string
	now   func() time.Time

	// sleep waits d and reports false if cancel closed first.
	sleep func(d time.Duration, cancel <-chan struct{}) bool
}

// sleepOrCancel is the production sleep.
func sleepOrCancel(d time.Duration, cancel <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-cancel:
		return false
	}
}

// activationBudget bounds the process loopback activations of a recorder process. Each activation keeps a
// completion handler, its parameters and an event alive for the process lifetime (Windows may still call the handler
// after a timeout), and each recorder child serves one recording with a one-way fallback, so one is enough.
type activationBudget struct {
	used atomic.Int32
	max  int32
}

// take uses one activation or returns ErrActivationLimit.
func (b *activationBudget) take() error {
	if b.used.Add(1) > b.max {
		b.used.Add(-1)
		return ErrActivationLimit
	}
	return nil
}

// wasapiStream is a running capture. Its capture goroutine owns the endpoint; Read takes packets from a queue.
type wasapiStream struct {
	deps streamDeps
	obs  Observer

	mu   sync.Mutex
	info StreamInfo

	reopens atomic.Uint64

	packets   chan Packet // closed by the capture goroutine when it ends; err is set before
	err       error
	stopCh    chan struct{}
	stopOnce  sync.Once
	closing   chan struct{}
	closeOnce sync.Once
	done      chan struct{}

	// Capture goroutine only.
	discontinuity bool      // the next packet follows a gap (reopen or switch)
	tries         int       // reopen tries used in the current incident
	lastOpen      time.Time // when the stream was last reopened or switched
}

// openStream starts the capture goroutine and returns once target, or the classic loopback fallback, runs. It
// reports every try through obs. When ctx ends first, the goroutine is stopped and ctx's error returned.
func openStream(ctx context.Context, deps streamDeps, target Target, obs Observer) (Stream, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if obs == nil {
		obs = nopObserver{}
	}
	s := &wasapiStream{
		deps: deps, obs: obs, info: StreamInfo{Build: deps.build},
		packets: make(chan Packet, packetQueue),
		stopCh:  make(chan struct{}), closing: make(chan struct{}), done: make(chan struct{}),
	}

	started := make(chan error, 1)
	go func() {
		defer close(s.done)
		if err := deps.thread(func() { s.run(target, started) }); err != nil {
			started <- err
		}
	}()

	select {
	case err := <-started:
		if err != nil {
			<-s.done
			return nil, err
		}
		return s, nil
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	}
}

// run opens the stream, reports the start, then captures until stop or a failure.
func (s *wasapiStream) run(target Target, started chan<- error) {
	ep, mode, err := s.openFirst(target)
	if err != nil {
		started <- err
		return
	}
	s.setEndpoint(ep, mode)
	started <- nil

	s.err = s.loop(ep, mode)
	close(s.packets)
}

// openFirst opens target and falls back to classic loopback when process loopback does not open (ADR-0002
// Decision 3). A ModeSystem target (also the floor of a recording that fell back) opens only classic loopback.
func (s *wasapiStream) openFirst(target Target) (endpoint, Mode, error) {
	ep, err := s.attempt(target)
	if err == nil {
		return ep, target.Mode, nil
	}
	if target.Mode == ModeSystem || errors.Is(err, errStopped) {
		return nil, "", err
	}

	s.obs.Switch(Switch{From: target.Mode, To: ModeSystem, Reason: "activation failed: " + errorText(err)})
	ep, serr := s.attempt(Target{Mode: ModeSystem})
	if serr != nil {
		if errors.Is(serr, errStopped) {
			return nil, "", serr
		}
		return nil, "", fmt.Errorf("%s: %w; %s: %w", target.Mode, err, ModeSystem, serr)
	}
	return ep, ModeSystem, nil
}

// attempt opens one target and reports the try; a try interrupted by a stop is not reported.
func (s *wasapiStream) attempt(t Target) (endpoint, error) {
	ep, err := s.deps.open(s.stopCh, t)
	if errors.Is(err, errStopped) {
		return nil, err
	}
	a := Attempt{Mode: t.Mode, OK: err == nil}
	if err != nil {
		a.HResult, a.Error = hresultText(err), err.Error()
	} else {
		a.Error = ep.warning()
	}
	s.obs.Attempt(a)
	return ep, err
}

// loop moves packets to Read until a stop (then it drains the endpoint once and returns nil) or a failure that
// cannot be recovered. A lost or changed device is reopened in classic loopback; a failed process loopback stream
// switches to classic loopback.
func (s *wasapiStream) loop(ep endpoint, mode Mode) error {
	for {
		if s.stopping() {
			s.finish(ep)
			return nil
		}

		err := ep.wait(waitStep)
		if err == nil {
			err = s.drain(ep)
		}
		changed := false
		if err == nil {
			changed, err = ep.service(s.deps.now())
		}
		if errors.Is(err, errClosed) {
			ep.close()
			return nil
		}
		if err == nil && !changed {
			continue
		}

		ep.close()
		next, rerr := s.recover(mode, err)
		if errors.Is(rerr, errStopped) {
			return nil // nothing was started, so there is nothing to drain
		}
		if rerr != nil {
			return rerr
		}
		ep, mode = next, ModeSystem
	}
}

// finish stops the client and drains its buffer once, so the last packets reach the chunk. ep was started (only
// started endpoints exist), so the drain is safe; its errors only lose the tail.
func (s *wasapiStream) finish(ep endpoint) {
	defer ep.close()
	if err := ep.stop(); err != nil {
		return
	}
	_ = s.drain(ep)
}

// drain hands every pending packet to Read.
func (s *wasapiStream) drain(ep endpoint) error {
	for range maxDrainPackets {
		p, ok, err := ep.read()
		if err != nil || !ok {
			return err
		}
		pkt := Packet{Format: ep.format(), Frames: p.frames, Data: p.data, Silent: p.silent,
			Discontinuity: p.discontinuity || s.discontinuity}
		s.discontinuity = false
		select {
		case s.packets <- pkt:
		case <-s.closing:
			return errClosed
		}
	}
	return nil
}

// recover opens classic loopback after the stream of mode failed with cause (nil: the default device changed). The
// tries of one incident are bounded by reopenBackoff; a stream that fails again within stableAfter of its reopen
// continues the incident, so a flapping device cannot loop forever.
func (s *wasapiStream) recover(mode Mode, cause error) (endpoint, error) {
	if s.lastOpen.IsZero() || s.deps.now().Sub(s.lastOpen) >= stableAfter {
		s.tries = 0
	}
	if mode != ModeSystem {
		s.obs.Switch(Switch{From: mode, To: ModeSystem, Reason: "stream failed: " + errorText(cause)})
	}

	backoff := reopenBackoff()
	last := cause
	for s.tries < len(backoff) {
		d := backoff[s.tries]
		s.tries++
		if !s.deps.sleep(d, s.stopCh) {
			return nil, errStopped
		}
		ep, err := s.attempt(Target{Mode: ModeSystem})
		if errors.Is(err, errStopped) {
			return nil, err
		}
		if err == nil {
			s.lastOpen, s.discontinuity = s.deps.now(), true
			if mode == ModeSystem {
				s.reopens.Add(1)
			}
			s.setEndpoint(ep, ModeSystem)
			return ep, nil
		}
		last = err
	}
	if last == nil {
		last = ErrDeviceLost
	}
	return nil, fmt.Errorf("audiocapture: the output device could not be reopened after %d tries: %w", len(backoff), last)
}

func (s *wasapiStream) setEndpoint(ep endpoint, mode Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info.Mode, s.info.Format = mode, ep.format()
}

func (s *wasapiStream) stopping() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

// Info describes the stream now.
func (s *wasapiStream) Info() StreamInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// Read returns the next packet; after Stop the drained ones, then io.EOF.
func (s *wasapiStream) Read(ctx context.Context) (Packet, error) {
	select {
	case p, ok := <-s.packets:
		if ok {
			return p, nil
		}
		if s.err != nil {
			return Packet{}, s.err
		}
		return Packet{}, io.EOF
	case <-ctx.Done():
		return Packet{}, ctx.Err()
	}
}

// Reopens counts the reopens of classic loopback.
func (s *wasapiStream) Reopens() uint64 { return s.reopens.Load() }

// Stop ends the capture; the drained packets follow.
func (s *wasapiStream) Stop() { s.stopOnce.Do(func() { close(s.stopCh) }) }

// Close stops the capture, abandons the packets not read and waits until the capture goroutine has released the
// device.
func (s *wasapiStream) Close() error {
	s.Stop()
	s.closeOnce.Do(func() { close(s.closing) })
	<-s.done
	return nil
}

// errorText is the cause of a switch or reopen; nil is a default device change.
func errorText(err error) string {
	if err == nil {
		return "the default output device changed"
	}
	return err.Error()
}

type nopObserver struct{}

func (nopObserver) Attempt(Attempt) {}
func (nopObserver) Switch(Switch)   {}
