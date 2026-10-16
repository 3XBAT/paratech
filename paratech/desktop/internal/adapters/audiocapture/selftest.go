package audiocapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

// The "Check sound" self-test (TASK-058, AC-3; ADR-0002 Consequences): a recorder child plays a short sine tone on the
// default output device and captures it with process loopback INCLUDE of its own process and with classic loopback; a
// Goertzel filter tells whether each capture heard it. Our tone is not in the chosen app's process tree, so the test
// proves that this Windows build captures, not that the chosen app is audible. Nothing is written to disk.

const (
	selfTestFreq      = 1000.0 // Hz
	selfTestAmplitude = 0.25   // -12 dBFS

	// toneBlocksPerSecond makes Goertzel blocks of 100 ms: 1 kHz then falls exactly on a bin at 8, 16, 44.1 and 48 kHz.
	toneBlocksPerSecond = 10
	// toneMinRatio is the share of a block's energy that must be at the tone frequency: about 1 for our tone alone,
	// about 2/N for noise, so other sounds mixed into classic loopback still let the tone through.
	toneMinRatio = 0.3
	// toneMinPower is the least mean square of a block: about -60 dBFS.
	toneMinPower = 1e-6
	// toneBlocksNeeded is how many blocks in a row must carry the tone (300 ms).
	toneBlocksNeeded = 3

	// Defaults of SelfTestTiming: the budget of the child.
	defaultSelfTestTone = 1500 * time.Millisecond
	defaultSelfTestLead = 300 * time.Millisecond
	defaultSelfTestTail = 500 * time.Millisecond
	defaultSelfTestOpen = 7 * time.Second // the 5 s activation of process loopback and the classic fallback

	// selfTestMargin covers what the child's budget does not: the process start, the drain and the report.
	selfTestMargin = 5 * time.Second

	// DefaultSelfTestTimeout bounds a whole self-test in the parent: the child's budget (opening the captures, the
	// lead, the tone and the tail: 9.3 s) and the margin, 14.3 s. The child is killed after it.
	DefaultSelfTestTimeout = defaultSelfTestOpen + defaultSelfTestLead + defaultSelfTestTone + defaultSelfTestTail +
		selfTestMargin

	// checkStopWait bounds how long a recording start waits for a stopped sound check to end.
	checkStopWait = 2 * time.Second

	// opCheckSound names the self-test in errors.
	opCheckSound = "check sound"
)

// ErrSelfTestRunning means another self-test runs; one runs at a time.
var ErrSelfTestRunning = errors.New("audiocapture: a sound check is already running")

// errRecordingStarted stops a running sound check: a recording starts, and the tone must not get into it.
var errRecordingStarted = errors.New("a recording started")

// Tone is a sine to play.
type Tone struct {
	Freq      float64 // Hz
	Amplitude float64 // 0..1 of full scale
	Duration  time.Duration
}

// OutputDevice describes the default output device.
type OutputDevice struct {
	Name string // friendly name; a hardware name
	// HandsFree reports a Bluetooth endpoint of the hands-free form factors (headset, handset): the call mode with
	// low quality. A hint only (docs/refs/audio.md: not confirmed).
	HandsFree bool
}

// TonePlayer plays the self-test tone. PlatformTone is the implementation of this OS.
type TonePlayer interface {
	// Output describes the default output device; an error wrapping ErrNoDevice when there is none.
	Output() (OutputDevice, error)

	// Play renders t on the default output device in shared mode and returns when it has played or ctx ended.
	Play(ctx context.Context, t Tone) error
}

// SelfTestTiming tunes the child side of the self-test; zero fields take the defaults.
type SelfTestTiming struct {
	Tone time.Duration // how long the tone plays; 1.5 s
	Lead time.Duration // how long the captures run before the tone; 300 ms
	Tail time.Duration // how long they run after it, for the output latency; 500 ms
	Open time.Duration // bound of opening the captures; 7 s (the 5 s activation and the classic fallback)
}

func (t SelfTestTiming) withDefaults() SelfTestTiming {
	if t.Tone <= 0 {
		t.Tone = defaultSelfTestTone
	}
	if t.Lead <= 0 {
		t.Lead = defaultSelfTestLead
	}
	if t.Tail <= 0 {
		t.Tail = defaultSelfTestTail
	}
	if t.Open <= 0 {
		t.Open = defaultSelfTestOpen
	}
	return t
}

// selfTest runs a self-test request in the child and reports one selftest event per mode, then "stopped". A closed
// stdin or an ended ctx stops it; the modes not finished then are failed.
func (c *child) selfTest(ctx context.Context, stop <-chan struct{}, src Source) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	dev, checks := runSelfTest(ctx, src, c.cfg)
	attrs := []any{slog.String("device", shortText(dev.Name)), slog.Bool("hands_free", dev.HandsFree)}
	for _, m := range checks {
		c.out.emit(event{Type: evSelfTest, Mode: string(m.Mode), Cause: string(m.Cause), HResult: m.HResult,
			Device: shortText(dev.Name), HandsFree: dev.HandsFree})
		attrs = append(attrs, slog.String(string(m.Mode), string(m.Cause)))
	}
	c.log.Info("self-test finished", attrs...)
	c.out.emit(event{Type: evStopped})
	return ChildExitOK
}

// runSelfTest opens both captures, plays the tone while they run and returns what each heard.
func runSelfTest(ctx context.Context, src Source, cfg ChildConfig) (OutputDevice, []values.ModeCheck) {
	targets := []Target{{Mode: ModeInclude, PID: cfg.OwnPID}, {Mode: ModeSystem}}
	all := func(cause values.SoundCause) []values.ModeCheck {
		out := make([]values.ModeCheck, 0, len(targets))
		for _, t := range targets {
			out = append(out, values.ModeCheck{Mode: t.Mode, Cause: cause})
		}
		return out
	}
	if cfg.Tone == nil {
		return OutputDevice{}, all(values.SoundFailed)
	}
	dev, err := cfg.Tone.Output()
	switch {
	case errors.Is(err, ErrNoDevice):
		return OutputDevice{}, all(values.SoundNoDevice)
	case errors.Is(err, recording.ErrCaptureUnavailable):
		return OutputDevice{}, all(values.SoundUnsupported)
	case err != nil:
		return OutputDevice{}, all(values.SoundFailed)
	}

	listeners := make([]*listener, len(targets))
	var wg sync.WaitGroup
	openCtx, cancelOpen := context.WithTimeout(ctx, cfg.SelfTest.Open)
	for i, t := range targets {
		l := &listener{target: t}
		listeners[i] = l
		wg.Go(func() { l.open(openCtx, src) })
	}
	wg.Wait()
	cancelOpen()

	for _, l := range listeners {
		if l.stream != nil {
			wg.Go(func() { l.listen(ctx) })
		}
	}
	playErr := playTone(ctx, cfg.Tone, cfg.SelfTest)
	for _, l := range listeners {
		if l.stream != nil {
			l.stream.Stop() // the drained packets follow, then io.EOF ends listen
		}
	}
	wg.Wait()

	out := make([]values.ModeCheck, 0, len(listeners))
	for _, l := range listeners {
		out = append(out, l.result(playErr))
	}
	return dev, out
}

// playTone waits the lead, plays the tone and waits the tail.
func playTone(ctx context.Context, p TonePlayer, t SelfTestTiming) error {
	if err := sleepCtx(ctx, t.Lead); err != nil {
		return err
	}
	if err := p.Play(ctx, Tone{Freq: selfTestFreq, Amplitude: selfTestAmplitude, Duration: t.Tone}); err != nil {
		return err
	}
	return sleepCtx(ctx, t.Tail)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// listener is one capture of the self-test.
type listener struct {
	target Target

	stream  Stream
	cause   values.SoundCause // set when the capture did not open as target
	hresult string
	heard   bool
	err     error // the capture ended with an error before the stop
}

// open opens the capture. A process loopback target that falls back to classic loopback is unsupported here: the
// fallback is the other mode of the test.
func (l *listener) open(ctx context.Context, src Source) {
	obs := &selfTestObserver{}
	st, err := src.Open(ctx, l.target, obs)
	l.hresult = obs.failedHResult(l.target.Mode)
	switch {
	case err != nil:
		l.cause = openCause(ctx, l.target.Mode, err)
	case st.Info().Mode != l.target.Mode:
		_ = st.Close()
		l.cause = values.SoundUnsupported
	default:
		l.stream = st
	}
}

func openCause(ctx context.Context, mode Mode, err error) values.SoundCause {
	switch {
	case ctx.Err() != nil:
		return values.SoundFailed
	case errors.Is(err, recording.ErrCaptureUnavailable):
		return values.SoundUnsupported
	case mode.IsProcessLoopback():
		return values.SoundUnsupported // process loopback needs no device: its activation failed
	case errors.Is(err, ErrNoDevice):
		return values.SoundNoDevice
	}
	return values.SoundFailed
}

// listen reads the capture until it ends and feeds the tone detector.
func (l *listener) listen(ctx context.Context) {
	var (
		det  *toneDetector
		last Format
		mono []float32
	)
	for {
		p, err := l.stream.Read(ctx)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			l.err = err
			return
		}
		if det == nil || p.Format != last {
			det, last = newToneDetector(selfTestFreq, p.Format.SampleRate), p.Format
		}
		if p.Silent {
			det.silence(p.Frames)
		} else {
			mono = toMono(p.Format, p.Data, mono[:0])
			det.push(mono)
		}
		l.heard = l.heard || det.heard
	}
}

// result is the check of the mode; the stream is closed here.
func (l *listener) result(playErr error) values.ModeCheck {
	m := values.ModeCheck{Mode: l.target.Mode, Cause: l.cause, HResult: l.hresult}
	if l.stream == nil {
		return m
	}
	_ = l.stream.Close()
	switch {
	case l.heard:
		m.Cause = values.SoundOK
	case l.err != nil || playErr != nil:
		m.Cause = values.SoundFailed
	default:
		m.Cause = values.SoundSilence
	}
	return m
}

// selfTestObserver keeps the HRESULT of the failed tries.
type selfTestObserver struct {
	mu       sync.Mutex
	attempts []Attempt
}

func (o *selfTestObserver) Attempt(a Attempt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.attempts = append(o.attempts, a)
}

func (o *selfTestObserver) Switch(Switch) {}

func (o *selfTestObserver) failedHResult(mode Mode) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.attempts) - 1; i >= 0; i-- {
		if a := o.attempts[i]; a.Mode == mode && !a.OK {
			return shortText(a.HResult)
		}
	}
	return ""
}

// toneDetector finds the tone in blocks of 100 ms with a Goertzel filter: a block carries the tone when it is loud
// enough and most of its energy is at the tone frequency; the tone is heard after toneBlocksNeeded such blocks in a
// row. It is pure, so it is tested on synthetic signals.
type toneDetector struct {
	coeff float64 // 2 cos(2 pi f / rate)
	size  int

	n              int
	s1, s2, energy float64
	run            int
	heard          bool
}

func newToneDetector(freq float64, rate uint32) *toneDetector {
	return &toneDetector{
		coeff: 2 * math.Cos(2*math.Pi*freq/float64(max(rate, 1))),
		size:  max(int(rate)/toneBlocksPerSecond, 1),
	}
}

func (d *toneDetector) push(samples []float32) {
	for _, x := range samples {
		d.add(float64(x))
	}
}

func (d *toneDetector) silence(frames int) {
	for range frames {
		d.add(0)
	}
}

func (d *toneDetector) add(x float64) {
	s := x + d.coeff*d.s1 - d.s2
	d.s2, d.s1 = d.s1, s
	d.energy += x * x
	d.n++
	if d.n < d.size {
		return
	}

	n := float64(d.n)
	power := d.s1*d.s1 + d.s2*d.s2 - d.coeff*d.s1*d.s2
	if d.energy/n >= toneMinPower && 2*power/(n*d.energy) >= toneMinRatio {
		d.run++
	} else {
		d.run = 0
	}
	d.heard = d.heard || d.run >= toneBlocksNeeded
	d.n, d.s1, d.s2, d.energy = 0, 0, 0, 0
}

// sineFrames returns frames interleaved frames of a sine in f (the same on every channel) from frame start: the
// samples a TonePlayer renders.
func sineFrames(f Format, freq, amp float64, start int64, frames int) []byte {
	le := binary.LittleEndian
	out := make([]byte, 0, frames*f.BlockAlign())
	for i := start; i < start+int64(frames); i++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(f.SampleRate))
		for range f.Channels {
			switch {
			case f.Float:
				out = le.AppendUint32(out, math.Float32bits(float32(v)))
			case f.BitsPerSample == 16:
				out = le.AppendUint16(out, uint16(int16(math.Round(v*math.MaxInt16)))) //nolint:gosec // a sample within -1..1
			case f.BitsPerSample == 24:
				x := int32(math.Round(v * (1<<23 - 1)))
				out = append(out, byte(x), byte(x>>8), byte(x>>16)) //nolint:gosec // the three low bytes of a 24-bit sample
			default:
				out = le.AppendUint32(out, uint32(int32(math.Round(v*math.MaxInt32)))) //nolint:gosec // a sample within -1..1
			}
		}
	}
	return out
}

// Endpoint form factors (mingw-w64 mmdeviceapi.h EndpointFormFactor) of the hands-free profile.
const (
	formFactorHeadset = 5
	formFactorHandset = 6
)

// handsFreeEndpoint reports a hands-free endpoint: a headset or handset form factor (PKEY_AudioEndpoint_FormFactor)
// enumerated by a Bluetooth enumerator (PKEY_Device_EnumeratorName "BTHENUM"/"BTHHFENUM"), so a wired USB headset is
// not one. Whether the endpoint store has the enumerator name is not confirmed; without it the answer is no.
func handsFreeEndpoint(formFactor uint32, formFactorOK bool, enumerator string) bool {
	return formFactorOK && (formFactor == formFactorHeadset || formFactor == formFactorHandset) &&
		strings.HasPrefix(strings.ToUpper(enumerator), "BTH")
}

// CheckSound runs the self-test in a recorder child (the --recorder run mode with a self-test request) and returns
// what each mode heard. While a capture of a recording runs it refuses (Busy): the tone would disturb the lecture; a
// recording that starts meanwhile stops the check, which then answers Busy too. One check runs at a time
// (ErrSelfTestRunning); the child is killed after Config.SelfTestTimeout. A build without capture fails with
// ErrUnavailable through the child's answer (unsupported).
func (r *Recorder) CheckSound(ctx context.Context) (values.SoundCheck, error) {
	if !r.checking.TryLock() {
		return values.SoundCheck{}, ErrSelfTestRunning
	}
	defer r.checking.Unlock()

	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	running := &runningCheck{stop: stop, done: make(chan struct{})}
	defer close(running.done)
	if !r.beginCheck(running) {
		return values.SoundCheck{Busy: true}, nil
	}
	defer r.endCheck(running)

	ctx, cancel := context.WithTimeoutCause(ctx, r.cfg.SelfTestTimeout,
		fmt.Errorf("the sound check did not finish within %v", r.cfg.SelfTestTimeout))
	defer cancel()
	check, err := r.runSelfTest(ctx)
	if errors.Is(context.Cause(ctx), errRecordingStarted) {
		r.log.Info("sound check stopped: a recording started")
		return values.SoundCheck{Busy: true}, nil
	}
	if err != nil {
		return values.SoundCheck{}, fmt.Errorf("audiocapture: %s: %w", opCheckSound, err)
	}
	r.log.Info("sound check", slog.String("device", check.Device), slog.Bool("hands_free", check.HandsFree),
		slog.String("include", string(check.Result(ModeInclude).Cause)),
		slog.String("system", string(check.Result(ModeSystem).Cause)),
		slog.String("include_hresult", check.Result(ModeInclude).HResult))
	return check, nil
}

// runningCheck is the sound check that runs, for a recording start to stop.
type runningCheck struct {
	stop context.CancelCauseFunc
	done chan struct{} // closed when CheckSound returns, its child reaped
}

// beginCheck registers c unless a capture of a recording runs.
func (r *Recorder) beginCheck(c *runningCheck) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cp := range r.captures {
		if !cp.isEnded() {
			return false
		}
	}
	r.check = c
	return true
}

func (r *Recorder) endCheck(c *runningCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.check == c {
		r.check = nil
	}
}

// stopCheck stops the running sound check c, if any, and waits a little for it to end, so its tone stays out of the
// recording that starts. The check's child is killed at once; a check that still has not ended is left behind.
func (r *Recorder) stopCheck(c *runningCheck) {
	if c == nil {
		return
	}
	c.stop(errRecordingStarted)
	select {
	case <-c.done:
	case <-r.after(checkStopWait):
		r.log.Warn("the sound check did not stop in time; the recording starts anyway")
	}
}

// runSelfTest runs one self-test child until it has exited and is reaped.
func (r *Recorder) runSelfTest(ctx context.Context) (values.SoundCheck, error) {
	p, err := r.deps.Launcher.Launch()
	if err != nil {
		return values.SoundCheck{}, err
	}
	log := r.log.With(slog.Int("pid", p.PID()))
	log.Info("sound check child launched")

	events := make(chan event)
	var readers sync.WaitGroup
	readers.Go(func() {
		defer close(events)
		scanEvents(p.Stdout(), events, log, nil)
	})
	readers.Go(func() { logStderr(p.Stderr(), log) })
	exited := make(chan int, 1)
	go func() {
		readers.Wait()
		code, err := p.Wait()
		if err != nil {
			log.Warn("reaping the sound check child failed", slog.String("error", shortText(err.Error())))
		}
		exited <- code
	}()

	kill := func(why string) {
		log.Warn("killing the sound check child", slog.String("reason", why))
		if err := p.Kill(); err != nil {
			log.Error("killing the sound check child failed", slog.String("error", shortText(err.Error())))
		}
	}
	if _, err := p.Stdin().Write(encodeSelfTest()); err != nil {
		kill("send the request: " + shortText(err.Error()))
	}

	var (
		check values.SoundCheck
		seen  = map[Mode]bool{}
		done  = ctx.Done()
		cause error
	)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			switch ev.Type {
			case evSelfTest:
				if m := Mode(ev.Mode); (m == ModeInclude || m == ModeSystem) && !seen[m] {
					seen[m] = true
					check.Modes = append(check.Modes, values.ModeCheck{Mode: m, Cause: values.SoundCause(ev.Cause), HResult: ev.HResult})
					check.Device, check.HandsFree = ev.Device, ev.HandsFree
				}
			case evStopped, evFailed:
				_ = p.Stdin().Close() // the child is done; a closed stdin lets a lingering one end
			}

		case <-done:
			done, cause = nil, context.Cause(ctx)
			kill(shortText(cause.Error()))

		case code := <-exited:
			_ = p.Stdin().Close()
			log.Info("sound check child exited", slog.Int("code", code))
			switch {
			case len(check.Modes) == 2:
				return check, nil
			case cause != nil:
				return values.SoundCheck{}, cause
			}
			return values.SoundCheck{}, fmt.Errorf("the sound check child ended without a result (code %d)", code)
		}
	}
}
