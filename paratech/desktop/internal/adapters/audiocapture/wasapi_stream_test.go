package audiocapture

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

// The tests drive the stream logic of the WASAPI source through fake endpoints: no COM, no audio device.

var (
	fmt48f = Format{SampleRate: 48000, Channels: 2, BitsPerSample: 32, Float: true}
	fmt44s = Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
)

// fakeStep is what one wait of a fake endpoint yields.
type fakeStep struct {
	packets int           // packets pending after the wait
	silent  bool          // they are flagged silent
	sound   bool          // their data is not all zeros
	disc    bool          // they are flagged as a discontinuity
	readErr error         // read fails after the packets
	changed bool          // the default output device changed
	advance time.Duration // the fake clock moves on
}

// fakeEndpoint plays its steps, then idle packets per wait. Only the capture goroutine touches it until Close.
type fakeEndpoint struct {
	w     *fakeWorld
	mode  Mode
	f     Format
	note  string
	steps []fakeStep
	idle  int           // packets per wait after the steps
	tail  int           // packets left in the buffer when stopped (the final drain)
	name  string        // the output device
	plays bool          // the meters show sound on the target
	sound bool          // idle packets carry sound (not all zeros)
	tick  time.Duration // the fake clock moves on by this per wait

	cur     fakeStep
	pending int
	stopped int
	closed  int
	drained int // packets read after the stop
}

func (e *fakeEndpoint) format() Format  { return e.f }
func (e *fakeEndpoint) warning() string { return e.note }

func (e *fakeEndpoint) wait(time.Duration) error {
	if len(e.steps) > 0 {
		e.cur, e.steps = e.steps[0], e.steps[1:]
	} else {
		e.cur = fakeStep{packets: e.idle, sound: e.sound}
	}
	e.pending = e.cur.packets
	e.w.advance(e.cur.advance + e.tick)
	time.Sleep(time.Millisecond) // no busy loop while nothing is pending
	return nil
}

func (e *fakeEndpoint) read() (rawPacket, bool, error) {
	if e.closed > 0 {
		panic("read after close")
	}
	if e.pending > 0 {
		e.pending--
		if e.stopped > 0 {
			e.drained++
		}
		p := rawPacket{frames: 480, silent: e.cur.silent, discontinuity: e.cur.disc}
		if !p.silent {
			p.data = make([]byte, 480*e.f.BlockAlign())
			if e.cur.sound {
				p.data[len(p.data)-1] = 0x3f
			}
		}
		return p, true, nil
	}
	if err := e.cur.readErr; err != nil {
		e.cur.readErr = nil
		return rawPacket{}, false, err
	}
	return rawPacket{}, false, nil
}

func (e *fakeEndpoint) service(time.Time) (bool, error) {
	changed := e.cur.changed
	e.cur.changed = false
	return changed, nil
}

func (e *fakeEndpoint) stop() error {
	e.stopped++
	e.pending, e.cur = e.tail, fakeStep{}
	return nil
}

func (e *fakeEndpoint) close() { e.closed++ }

func (e *fakeEndpoint) device() string { return e.name }

func (e *fakeEndpoint) playing() bool {
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	e.w.meterReads++
	return e.plays
}

// openResult is the outcome of one open of a mode: an endpoint or an error.
type openResult struct {
	ep  *fakeEndpoint
	err error
}

// fakeWorld is the platform of the tests: scripted opens per mode, a fake clock moved by the sleeps.
type fakeWorld struct {
	mu         sync.Mutex
	results    map[Mode][]openResult
	opened     []Mode
	endpoints  []*fakeEndpoint
	sleeps     []time.Duration
	now        time.Time
	blockOpen  bool // open waits for the cancel
	blockSleep bool // sleep waits for the cancel
	threadErr  error
	muteAfter  time.Duration // the threshold of the mute detector; 0 = off
	meterReads int
}

func newWorld(results map[Mode][]openResult) *fakeWorld {
	w := &fakeWorld{results: results, now: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	for _, rs := range results {
		for _, r := range rs {
			if r.ep != nil {
				r.ep.w = w
			}
		}
	}
	return w
}

func (w *fakeWorld) open(cancel <-chan struct{}, t Target) (endpoint, error) {
	w.mu.Lock()
	block := w.blockOpen
	w.opened = append(w.opened, t.Mode)
	w.mu.Unlock()
	if block {
		<-cancel
		return nil, errStopped
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	rs := w.results[t.Mode]
	if len(rs) == 0 {
		return nil, hrError("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hrNotFound)
	}
	r := rs[0]
	w.results[t.Mode] = rs[1:]
	if r.err != nil {
		return nil, r.err
	}
	r.ep.mode = t.Mode
	w.endpoints = append(w.endpoints, r.ep)
	return r.ep, nil
}

func (w *fakeWorld) advance(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = w.now.Add(d)
}

func (w *fakeWorld) clock() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *fakeWorld) sleep(d time.Duration, cancel <-chan struct{}) bool {
	w.mu.Lock()
	block := w.blockSleep
	w.sleeps = append(w.sleeps, d)
	w.now = w.now.Add(d)
	w.mu.Unlock()
	if block {
		<-cancel
		return false
	}
	select {
	case <-cancel:
		return false
	default:
		return true
	}
}

func (w *fakeWorld) deps() streamDeps {
	return streamDeps{
		open: w.open,
		thread: func(body func()) error {
			if w.threadErr != nil {
				return w.threadErr
			}
			body()
			return nil
		},
		windows: testWindows, now: w.clock, sleep: w.sleep, muteAfter: w.muteAfter,
	}
}

var testWindows = values.WindowsVersion{Product: "Windows 10 Pro", DisplayVersion: "22H2", Major: 10, Build: 19045, UBR: 6456}

func (w *fakeWorld) snapshot() (opened []Mode, sleeps []time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.opened), slices.Clone(w.sleeps)
}

// recordingObserver keeps the reports of a source.
type recordingObserver struct {
	mu       sync.Mutex
	attempts []Attempt
	switches []Switch
}

func (o *recordingObserver) Attempt(a Attempt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.attempts = append(o.attempts, a)
}

func (o *recordingObserver) Switch(s Switch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.switches = append(o.switches, s)
}

func (o *recordingObserver) reports() ([]Attempt, []Switch) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.attempts), slices.Clone(o.switches)
}

func (o *recordingObserver) modes() []string {
	attempts, _ := o.reports()
	var out []string
	for _, a := range attempts {
		s := string(a.Mode) + ":ok"
		if !a.OK {
			s = string(a.Mode) + ":" + a.HResult
		}
		out = append(out, s)
	}
	return out
}

func openTest(t *testing.T, w *fakeWorld, target Target) (*wasapiStream, *recordingObserver) {
	t.Helper()
	obs := &recordingObserver{}
	st, err := openStream(context.Background(), w.deps(), target, obs)
	if err != nil {
		t.Fatalf("openStream = %v", err)
	}
	s := st.(*wasapiStream)
	t.Cleanup(func() { _ = s.Close() })
	return s, obs
}

func readN(t *testing.T, s Stream, n int) []Packet {
	t.Helper()
	var out []Packet
	for range n {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		p, err := s.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Read after %d packets: %v", len(out), err)
		}
		out = append(out, p)
	}
	return out
}

// readToEnd reads until Read fails and returns the packets and the error.
func readToEnd(t *testing.T, s Stream) ([]Packet, error) {
	t.Helper()
	var out []Packet
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		p, err := s.Read(ctx)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("the stream did not end")
		}
		if err != nil {
			return out, err
		}
		out = append(out, p)
	}
}

func TestWASAPIStreamRunsAndDrainsOnStop(t *testing.T) {
	ep := &fakeEndpoint{f: fmt48f, idle: 1, tail: 3}
	w := newWorld(map[Mode][]openResult{ModeInclude: {{ep: ep}}})
	s, obs := openTest(t, w, Target{Mode: ModeInclude, PID: 42})

	if got := s.Info(); got != (StreamInfo{Mode: ModeInclude, Format: fmt48f, Windows: testWindows}) {
		t.Fatalf("Info = %+v", got)
	}
	for _, p := range readN(t, s, 5) {
		if p.Format != fmt48f || p.Frames != 480 || len(p.Data) != 480*8 || p.Silent {
			t.Fatalf("packet %+v", p)
		}
	}
	s.Stop()
	s.Stop() // idempotent
	if _, err := readToEnd(t, s); !errors.Is(err, io.EOF) {
		t.Fatalf("after Stop: %v, want io.EOF", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if ep.stopped != 1 || ep.drained != 3 || ep.closed != 1 {
		t.Fatalf("endpoint stopped %d, drained %d, closed %d; want 1, 3, 1", ep.stopped, ep.drained, ep.closed)
	}
	if got := obs.modes(); !slices.Equal(got, []string{"include:ok"}) {
		t.Fatalf("attempts %v", got)
	}
	if _, sw := obs.reports(); len(sw) != 0 || s.Reopens() != 0 {
		t.Fatalf("switches %v, reopens %d", sw, s.Reopens())
	}
}

func TestWASAPIStreamFallsBackWhenProcessLoopbackDoesNotOpen(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		err    error
		hr     string
	}{
		{"include not supported", Target{Mode: ModeInclude, PID: 42}, hrError("ActivateAudioInterfaceAsync", hrNotImpl), "0x80004001"},
		{"exclude refused", Target{Mode: ModeExclude, PID: 7}, hrError("process loopback activation", hrAccessDenied), "0x80070005"},
		{"activation timeout", Target{Mode: ModeInclude, PID: 42}, ErrActivationTimeout, ""},
		{"second activation", Target{Mode: ModeExclude, PID: 7}, ErrActivationLimit, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := &fakeEndpoint{f: fmt44s, idle: 1}
			w := newWorld(map[Mode][]openResult{tt.target.Mode: {{err: tt.err}}, ModeSystem: {{ep: sys}}})
			s, obs := openTest(t, w, tt.target)

			if info := s.Info(); info.Mode != ModeSystem || info.Format != fmt44s {
				t.Fatalf("Info = %+v, want system in its mix format", info)
			}
			attempts, switches := obs.reports()
			if len(attempts) != 2 || attempts[0].Mode != tt.target.Mode || attempts[0].OK || attempts[0].HResult != tt.hr ||
				attempts[0].Error != tt.err.Error() || attempts[1] != (Attempt{Mode: ModeSystem, OK: true}) {
				t.Fatalf("attempts %+v", attempts)
			}
			if len(switches) != 1 || switches[0].From != tt.target.Mode || switches[0].To != ModeSystem ||
				!strings.Contains(switches[0].Reason, "activation failed: "+tt.err.Error()) {
				t.Fatalf("switches %+v", switches)
			}
			if p := readN(t, s, 1)[0]; p.Format != fmt44s {
				t.Fatalf("packet format %v", p.Format)
			}
		})
	}
}

func TestWASAPIStreamOpenFailures(t *testing.T) {
	notFound := hrError("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hrNotFound)
	tests := []struct {
		name       string
		target     Target
		results    map[Mode][]openResult
		threadErr  error
		wantOpened []Mode
		wantErr    []error
		wantTried  []string
	}{
		{
			name: "every mode fails", target: Target{Mode: ModeInclude, PID: 42},
			results:    map[Mode][]openResult{ModeInclude: {{err: hrError("ActivateAudioInterfaceAsync", hrNotImpl)}}, ModeSystem: {{err: notFound}}},
			wantOpened: []Mode{ModeInclude, ModeSystem}, wantErr: []error{ErrNotSupported, ErrNoDevice},
			wantTried: []string{"include:0x80004001", "system:0x80070490"},
		},
		{
			// The floor of a recording that fell back: classic loopback only, no process loopback activation.
			name: "a system target opens only classic loopback", target: Target{Mode: ModeSystem},
			results:    map[Mode][]openResult{ModeSystem: {{err: notFound}}},
			wantOpened: []Mode{ModeSystem}, wantErr: []error{ErrNoDevice}, wantTried: []string{"system:0x80070490"},
		},
		{
			name: "the COM thread cannot start", target: Target{Mode: ModeSystem}, threadErr: hrError("CoInitializeEx", hrChangedMode),
			wantErr: []error{ErrDevice},
		},
		{name: "invalid target", target: Target{Mode: ModeInclude}, wantErr: []error{ErrInvalidTarget}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(tt.results)
			w.threadErr = tt.threadErr
			obs := &recordingObserver{}
			st, err := openStream(context.Background(), w.deps(), tt.target, obs)
			if err == nil || st != nil {
				t.Fatalf("openStream = %v, %v; want an error", st, err)
			}
			for _, want := range tt.wantErr {
				if !errors.Is(err, want) {
					t.Errorf("err = %v, want %v", err, want)
				}
			}
			opened, _ := w.snapshot()
			if !slices.Equal(opened, tt.wantOpened) || !slices.Equal(obs.modes(), tt.wantTried) {
				t.Fatalf("opened %v, attempts %v; want %v, %v", opened, obs.modes(), tt.wantOpened, tt.wantTried)
			}
		})
	}
}

func TestWASAPIStreamOpenStopsWithItsContext(t *testing.T) {
	w := newWorld(nil)
	w.blockOpen = true // an activation that never completes
	obs := &recordingObserver{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	st, err := openStream(ctx, w.deps(), Target{Mode: ModeInclude, PID: 42}, obs)
	if !errors.Is(err, context.DeadlineExceeded) || st != nil {
		t.Fatalf("openStream = %v, %v; want the context error", st, err)
	}
	if attempts, _ := obs.reports(); len(attempts) != 0 {
		t.Fatalf("a try cut off by the stop was reported: %+v", attempts)
	}
}

// A classic loopback stream follows the default output device: it is reopened, counted, the gap is flagged and a new
// format reaches the packets.
func TestWASAPIStreamReopensOnADefaultDeviceChange(t *testing.T) {
	first := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 2}, {changed: true}}}
	second := &fakeEndpoint{f: fmt44s, idle: 1}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}, {ep: second}}})
	s, obs := openTest(t, w, Target{Mode: ModeSystem})

	got := readN(t, s, 4)
	for i, want := range []struct {
		f    Format
		disc bool
	}{{fmt48f, false}, {fmt48f, false}, {fmt44s, true}, {fmt44s, false}} {
		if got[i].Format != want.f || got[i].Discontinuity != want.disc {
			t.Fatalf("packet %d: %v discontinuity %v; want %v %v", i, got[i].Format, got[i].Discontinuity, want.f, want.disc)
		}
	}
	if s.Reopens() != 1 || s.Info().Format != fmt44s || s.Info().Mode != ModeSystem {
		t.Fatalf("reopens %d, info %+v", s.Reopens(), s.Info())
	}
	_, sleeps := w.snapshot()
	if !slices.Equal(sleeps, []time.Duration{250 * time.Millisecond}) {
		t.Fatalf("sleeps %v", sleeps)
	}
	if attempts, switches := obs.reports(); len(attempts) != 2 || len(switches) != 0 {
		t.Fatalf("attempts %+v, switches %+v", attempts, switches)
	}
	s.Stop()
	if _, err := readToEnd(t, s); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	_ = s.Close()
	if first.stopped != 0 || first.closed != 1 || second.stopped != 1 || second.closed != 1 {
		t.Fatalf("first stopped %d closed %d, second stopped %d closed %d", first.stopped, first.closed, second.stopped, second.closed)
	}
}

func TestWASAPIStreamReopensALostDeviceWithBackoff(t *testing.T) {
	lost := hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated)
	notFound := hrError("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hrNotFound)
	first := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: lost}}}
	second := &fakeEndpoint{f: fmt48f, idle: 1}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}, {err: notFound}, {err: notFound}, {ep: second}}})
	s, obs := openTest(t, w, Target{Mode: ModeSystem})

	if p := readN(t, s, 2); p[0].Discontinuity || !p[1].Discontinuity {
		t.Fatalf("packets %+v; want the one after the reopen flagged", p)
	}
	_, sleeps := w.snapshot()
	if !slices.Equal(sleeps, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}) {
		t.Fatalf("sleeps %v", sleeps)
	}
	if got := obs.modes(); !slices.Equal(got, []string{"system:ok", "system:0x80070490", "system:0x80070490", "system:ok"}) {
		t.Fatalf("attempts %v", got)
	}
	if s.Reopens() != 1 {
		t.Fatalf("reopens %d", s.Reopens())
	}
}

// When the reopen budget is used up the capture ends by itself: Read fails (not io.EOF), never a panic.
func TestWASAPIStreamFailsWhenTheReopenBudgetIsUsedUp(t *testing.T) {
	lost := hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated)
	first := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: lost}}}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}}}) // every reopen: no device
	s, _ := openTest(t, w, Target{Mode: ModeSystem})

	packets, err := readToEnd(t, s)
	if len(packets) != 1 || err == nil || errors.Is(err, io.EOF) || !errors.Is(err, ErrNoDevice) ||
		!strings.Contains(err.Error(), "after 5 tries") {
		t.Fatalf("%d packets, err %v; want a reopen failure", len(packets), err)
	}
	_, sleeps := w.snapshot()
	if !slices.Equal(sleeps, reopenBackoff()) {
		t.Fatalf("sleeps %v, want %v", sleeps, reopenBackoff())
	}
	if _, again := s.Read(context.Background()); !errors.Is(again, ErrNoDevice) {
		t.Fatalf("second Read = %v", again)
	}
	if err := s.Close(); err != nil || first.closed != 1 || first.stopped != 0 {
		t.Fatalf("Close = %v; first closed %d stopped %d", err, first.closed, first.stopped)
	}
}

// A stream that fails again soon after its reopen continues the incident; one that ran stableAfter starts a new one.
func TestWASAPIStreamReopenIncidents(t *testing.T) {
	lost := hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated)
	tests := []struct {
		name  string
		ran   time.Duration
		sleep []time.Duration
	}{
		{"flapping continues the incident", time.Second, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}},
		{"a stable stream starts a new one", stableAfter, []time.Duration{250 * time.Millisecond, 250 * time.Millisecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: lost}}}
			second := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, advance: tt.ran}, {readErr: lost}}}
			third := &fakeEndpoint{f: fmt48f, idle: 1}
			w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}, {ep: second}, {ep: third}}})
			s, _ := openTest(t, w, Target{Mode: ModeSystem})

			readN(t, s, 3)
			if _, sleeps := w.snapshot(); !slices.Equal(sleeps, tt.sleep) {
				t.Fatalf("sleeps %v, want %v", sleeps, tt.sleep)
			}
			if s.Reopens() != 2 {
				t.Fatalf("reopens %d", s.Reopens())
			}
		})
	}
}

// A failing process loopback stream switches to classic loopback once (one-way), without counting a reopen.
func TestWASAPIStreamSwitchesWhenProcessLoopbackFails(t *testing.T) {
	for _, cause := range []error{
		hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated),
		hrError("IAudioCaptureClient::GetNextPacketSize", hrFail),
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			proc := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: cause}}}
			sys := &fakeEndpoint{f: fmt44s, idle: 1}
			w := newWorld(map[Mode][]openResult{ModeExclude: {{ep: proc}}, ModeSystem: {{ep: sys}}})
			s, obs := openTest(t, w, Target{Mode: ModeExclude, PID: 7})

			p := readN(t, s, 2)
			if p[0].Format != fmt48f || p[1].Format != fmt44s || !p[1].Discontinuity {
				t.Fatalf("packets %+v", p)
			}
			_, switches := obs.reports()
			if len(switches) != 1 || switches[0].From != ModeExclude || switches[0].To != ModeSystem ||
				!strings.Contains(switches[0].Reason, "stream failed: "+cause.Error()) {
				t.Fatalf("switches %+v", switches)
			}
			if s.Info().Mode != ModeSystem || s.Reopens() != 0 || proc.closed != 1 || proc.stopped != 0 {
				t.Fatalf("info %+v, reopens %d, process endpoint closed %d stopped %d", s.Info(), s.Reopens(), proc.closed, proc.stopped)
			}
			if opened, _ := w.snapshot(); !slices.Equal(opened, []Mode{ModeExclude, ModeSystem}) {
				t.Fatalf("opened %v: process loopback must not be activated again", opened)
			}
		})
	}
}

// A stop while the device is being reopened ends the stream at once: nothing runs, so nothing is drained.
func TestWASAPIStreamStopDuringAReopen(t *testing.T) {
	lost := hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated)
	first := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: lost}}}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}}})
	w.blockSleep = true
	s, _ := openTest(t, w, Target{Mode: ModeSystem})

	readN(t, s, 1)
	waitFor(t, "the reopen backoff", func() bool { _, sleeps := w.snapshot(); return len(sleeps) == 1 })
	s.Stop()
	if _, err := readToEnd(t, s); !errors.Is(err, io.EOF) {
		t.Fatalf("Read = %v, want io.EOF", err)
	}
	_ = s.Close()
	if first.stopped != 0 || first.closed != 1 {
		t.Fatalf("first stopped %d, closed %d", first.stopped, first.closed)
	}
}

func TestWASAPIStreamPassesThePacketFlags(t *testing.T) {
	ep := &fakeEndpoint{f: fmt48f, note: "keep-alive render stream failed", steps: []fakeStep{{packets: 1, silent: true}, {packets: 1, disc: true}, {packets: 1}}}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: ep}}})
	s, obs := openTest(t, w, Target{Mode: ModeSystem})

	p := readN(t, s, 3)
	if !p[0].Silent || p[0].Data != nil || p[0].Frames != 480 || !p[1].Discontinuity || p[1].Silent || p[2].Discontinuity {
		t.Fatalf("packets %+v", p)
	}
	if attempts, _ := obs.reports(); len(attempts) != 1 || !attempts[0].OK || attempts[0].Error != "keep-alive render stream failed" {
		t.Fatalf("attempts %+v: the warning belongs to the ok attempt", attempts)
	}
}

func TestWASAPIStreamReadAndCloseAreBounded(t *testing.T) {
	quiet := &fakeEndpoint{f: fmt48f}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: quiet}}})
	s, _ := openTest(t, w, Target{Mode: ModeSystem})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Read(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Read of a quiet stream = %v, want the context error", err)
	}

	// Nobody reads: the queue fills up, and Close still ends the capture goroutine.
	busy := &fakeEndpoint{f: fmt48f, idle: 4, tail: 10}
	w2 := newWorld(map[Mode][]openResult{ModeSystem: {{ep: busy}}})
	s2, _ := openTest(t, w2, Target{Mode: ModeSystem})
	waitFor(t, "a full queue", func() bool { return len(s2.packets) == packetQueue })
	done := make(chan struct{})
	go func() { _ = s2.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return")
	}
	if busy.closed != 1 {
		t.Fatalf("endpoint closed %d times", busy.closed)
	}
}

func TestActivationBudget(t *testing.T) {
	b := &activationBudget{max: maxProcessActivations}
	if err := b.take(); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	for range 2 {
		if err := b.take(); !errors.Is(err, ErrActivationLimit) {
			t.Fatalf("another activation = %v, want ErrActivationLimit", err)
		}
	}
	if b.used.Load() != 1 {
		t.Fatalf("used %d", b.used.Load())
	}
}

func TestReopenBackoffAndSleep(t *testing.T) {
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
	if !slices.Equal(reopenBackoff(), want) {
		t.Fatalf("backoff %v", reopenBackoff())
	}
	if !sleepOrCancel(time.Millisecond, make(chan struct{})) {
		t.Fatal("sleep without a cancel was cut off")
	}
	cancel := make(chan struct{})
	close(cancel)
	if sleepOrCancel(time.Hour, cancel) {
		t.Fatal("sleep ignored the cancel")
	}
	var o Observer = nopObserver{}
	o.Attempt(Attempt{})
	o.Switch(Switch{})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// readUntil reads until cond holds for a packet (true) or the fake clock passed limit (false); the clock must move.
func readUntil(t *testing.T, s Stream, w *fakeWorld, limit time.Time, cond func(Packet) bool) bool {
	t.Helper()
	for range 10000 {
		if !w.clock().Before(limit) {
			return false
		}
		if cond(readN(t, s, 1)[0]) {
			return true
		}
	}
	t.Fatal("the fake clock does not move")
	return false
}

func (w *fakeWorld) meterCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.meterReads
}

// The healthy-but-mute detector (ADR-0002 Decision 3) on fake endpoints: only a process loopback stream that delivers
// zeros while its meters show sound falls back, after the threshold; a muted target or a stream with sound stays.
func TestWASAPIStreamFallsBackWhenHealthyButMute(t *testing.T) {
	tests := []struct {
		name      string
		target    Target
		plays     bool
		sound     bool
		muteAfter time.Duration
		silentFlg bool // the packets are flagged silent instead of carrying zeros
		wantSw    bool
		who       string
	}{
		{name: "include: zeros while the app plays", target: Target{Mode: ModeInclude, PID: 42}, plays: true,
			muteAfter: 15 * time.Second, wantSw: true, who: "the app"},
		{name: "include: silent flags while the app plays", target: Target{Mode: ModeInclude, PID: 42}, plays: true,
			silentFlg: true, muteAfter: 15 * time.Second, wantSw: true, who: "the app"},
		{name: "exclude: zeros while other apps play", target: Target{Mode: ModeExclude, PID: 7}, plays: true,
			muteAfter: 15 * time.Second, wantSw: true, who: "other apps"},
		{name: "include: a muted app is not expected to play", target: Target{Mode: ModeInclude, PID: 42},
			muteAfter: 15 * time.Second},
		{name: "include: sound is captured", target: Target{Mode: ModeInclude, PID: 42}, plays: true, sound: true,
			muteAfter: 15 * time.Second},
		{name: "the detector is off", target: Target{Mode: ModeInclude, PID: 42}, plays: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := &fakeEndpoint{f: fmt48f, idle: 1, sound: tt.sound, plays: tt.plays, tick: 100 * time.Millisecond,
				name: "Speakers (Realtek(R) Audio)"}
			if tt.silentFlg {
				proc.idle, proc.steps = 0, slices.Repeat([]fakeStep{{packets: 1, silent: true}}, 400)
			}
			sys := &fakeEndpoint{f: fmt44s, idle: 1, name: "Speakers (Realtek(R) Audio)", tick: 100 * time.Millisecond}
			w := newWorld(map[Mode][]openResult{tt.target.Mode: {{ep: proc}}, ModeSystem: {{ep: sys}}})
			w.muteAfter = tt.muteAfter
			start := w.clock()
			s, obs := openTest(t, w, tt.target)

			switched := readUntil(t, s, w, start.Add(40*time.Second), func(p Packet) bool { return p.Format == fmt44s })
			_, switches := obs.reports()
			if !tt.wantSw {
				if switched || len(switches) != 0 || s.Info().Mode != tt.target.Mode || proc.closed != 0 {
					t.Fatalf("switched %v, switches %+v, info %+v", switched, switches, s.Info())
				}
				if n := w.meterCount(); tt.muteAfter == 0 && n != 0 {
					t.Fatalf("the meters were read %d times with the detector off", n)
				}
				return
			}

			if !switched || len(switches) != 1 {
				t.Fatalf("switched %v, switches %+v", switched, switches)
			}
			sw := switches[0]
			if sw.From != tt.target.Mode || sw.To != ModeSystem || sw.Cause != values.SwitchHealthyButMute ||
				!strings.Contains(sw.Reason, "only zeros for 15s while "+tt.who+" played") {
				t.Fatalf("switch %+v", sw)
			}
			// The threshold is measured on the stream clock: 15 s of readings, the first one after 1 s.
			if took := w.clock().Sub(start); took < 15*time.Second || took > 20*time.Second {
				t.Fatalf("switched after %v", took)
			}
			if s.Info().Mode != ModeSystem || s.Reopens() != 0 || proc.closed != 1 || proc.stopped != 0 {
				t.Fatalf("info %+v, reopens %d, process endpoint closed %d stopped %d", s.Info(), s.Reopens(), proc.closed, proc.stopped)
			}
			if opened, _ := w.snapshot(); !slices.Equal(opened, []Mode{tt.target.Mode, ModeSystem}) {
				t.Fatalf("opened %v", opened)
			}
			attempts, _ := obs.reports()
			if len(attempts) != 2 || !attempts[1].OK || attempts[1].Mode != ModeSystem || attempts[1].Device != "Speakers (Realtek(R) Audio)" {
				t.Fatalf("attempts %+v", attempts)
			}

			// One-way: classic loopback has no detector, even while it hears nothing.
			reads := w.meterCount()
			readUntil(t, s, w, w.clock().Add(30*time.Second), func(Packet) bool { return false })
			if _, again := obs.reports(); len(again) != 1 || w.meterCount() != reads {
				t.Fatalf("switches %+v, meter reads %d -> %d", again, reads, w.meterCount())
			}
		})
	}
}

// The ok attempt and the stream info carry the output device name.
func TestWASAPIStreamReportsTheOutputDevice(t *testing.T) {
	first := &fakeEndpoint{f: fmt48f, name: "Speakers", steps: []fakeStep{{packets: 1}, {changed: true}}}
	second := &fakeEndpoint{f: fmt48f, name: "Headphones", idle: 1}
	w := newWorld(map[Mode][]openResult{ModeSystem: {{ep: first}, {ep: second}}})
	s, obs := openTest(t, w, Target{Mode: ModeSystem})
	if s.Info().Device != "Speakers" {
		t.Fatalf("info %+v", s.Info())
	}
	readN(t, s, 3)
	attempts, _ := obs.reports()
	if len(attempts) != 2 || attempts[0].Device != "Speakers" || attempts[1].Device != "Headphones" || s.Info().Device != "Headphones" {
		t.Fatalf("attempts %+v, info %+v", attempts, s.Info())
	}
}

// The cause of every switch is set: activation failed and stream failed (healthy but mute: above).
func TestWASAPIStreamSwitchCauses(t *testing.T) {
	w := newWorld(map[Mode][]openResult{ModeInclude: {{err: ErrActivationTimeout}}, ModeSystem: {{ep: &fakeEndpoint{f: fmt44s, idle: 1}}}})
	_, obs := openTest(t, w, Target{Mode: ModeInclude, PID: 42})
	if _, sw := obs.reports(); len(sw) != 1 || sw[0].Cause != values.SwitchActivationFailed || !sw[0].IsFallback() {
		t.Fatalf("switches %+v", sw)
	}

	lost := hrError("IAudioCaptureClient::GetBuffer", hrDeviceInvalidated)
	proc := &fakeEndpoint{f: fmt48f, steps: []fakeStep{{packets: 1, readErr: lost}}}
	w2 := newWorld(map[Mode][]openResult{ModeInclude: {{ep: proc}}, ModeSystem: {{ep: &fakeEndpoint{f: fmt44s, idle: 1}}}})
	s2, obs2 := openTest(t, w2, Target{Mode: ModeInclude, PID: 42})
	readN(t, s2, 2)
	if _, sw := obs2.reports(); len(sw) != 1 || sw[0].Cause != values.SwitchStreamFailed {
		t.Fatalf("switches %+v", sw)
	}
}
