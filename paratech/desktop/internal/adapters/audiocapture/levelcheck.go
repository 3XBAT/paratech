package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

// The "Check sound" level check (TASK-075, ADR-0002 Consequences): a recorder child with a level check request
// captures the user's source as a recording would, for a bounded time, and reports the peak of each short window.
// Nothing is played, recorded or written to disk.

const (
	// DefaultLevelInterval is how often the child reports a level: 20 times a second.
	DefaultLevelInterval = 50 * time.Millisecond

	// DefaultCheckGrace is how long past its duration the parent waits for a level check child (its start, the open
	// of the capture and the stop) before it kills it.
	DefaultCheckGrace = 5 * time.Second

	// checkStopWait bounds how long a recording start waits for a stopped sound check to end.
	checkStopWait = 2 * time.Second

	// opCheckLevels names the level check in errors.
	opCheckLevels = "check sound levels"
)

// ErrSelfTestRunning means another sound check runs; one runs at a time.
var ErrSelfTestRunning = errors.New("audiocapture: a sound check is already running")

// errRecordingStarted stops a running sound check: a recording starts and takes the capture.
var errRecordingStarted = errors.New("a recording started")

// levelCheck runs a level check request in the child: it opens one capture of the target, reports a level event
// every LevelInterval and ends after the request's duration, when stdin closes or when ctx ends, whichever is first.
// The stream is always stopped and closed before the last event ("stopped", or "failed" with a sound cause).
func (c *child) levelCheck(ctx context.Context, stop <-chan struct{}, src Source, req LevelRequest) int {
	// The child's own bound: it ends even when the parent hangs and never closes stdin.
	ctx, cancel := context.WithTimeout(ctx, req.Duration)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	stream, err := src.Open(ctx, req.Target, &childObserver{out: c.out})
	if err != nil {
		if ctx.Err() != nil {
			c.out.emit(event{Type: evStopped})
			return ChildExitOK
		}
		c.out.emit(event{Type: evFailed, Cause: string(openCause(err)), Reason: shortText("open capture: " + err.Error()),
			Unavailable: errors.Is(err, recording.ErrCaptureUnavailable)})
		return ChildExitFailed
	}
	info := stream.Info()
	c.out.emit(event{Type: evStarted, Mode: string(info.Mode), Format: toWireFormat(info.Format), Device: shortText(info.Device)})
	c.log.Info("level check started", slog.String("target", string(req.Target.Mode)), slog.String("mode", string(info.Mode)),
		slog.Duration("duration", req.Duration))

	m := &levelMeter{}
	var bg sync.WaitGroup
	ticking := make(chan struct{})
	bg.Go(func() { c.reportLevels(m, ticking) })
	stopper := context.AfterFunc(ctx, stream.Stop)

	failure := meterStream(ctx, stream, m)

	stopper()
	stream.Stop()
	close(ticking)
	bg.Wait()
	if err := stream.Close(); err != nil {
		c.log.Warn("closing the capture failed", slog.String("error", shortText(err.Error())))
	}
	if failure != nil {
		c.out.emit(event{Type: evFailed, Cause: string(values.SoundFailed), Reason: shortText(failure.Error())})
		return ChildExitFailed
	}
	c.out.emit(event{Type: evStopped})
	return ChildExitOK
}

// openCause is the sound cause of a capture that did not open. Process loopback falls back to classic loopback
// inside the source, so an error here means that no mode opened.
func openCause(err error) values.SoundCause {
	switch {
	case errors.Is(err, recording.ErrCaptureUnavailable):
		return values.SoundUnsupported
	case errors.Is(err, ErrNoDevice):
		return values.SoundNoDevice
	}
	return values.SoundFailed
}

// meterStream feeds the packets to m until the stream ends: nil after a stop, the cause when it ended by itself.
// Silent packets add nothing; malformed ones are skipped. After ctx ends, Read returns the drained packets and
// io.EOF, or ctx's error soon.
func meterStream(ctx context.Context, stream Stream, m *levelMeter) error {
	var mono []float32
	for {
		p, err := stream.Read(ctx)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil && ctx.Err() != nil:
			return nil
		case err != nil:
			return fmt.Errorf("capture ended: %w", err)
		}
		if p.Silent || p.Frames <= 0 || p.Format.Validate() != nil || len(p.Data) != p.Frames*p.Format.BlockAlign() {
			continue
		}
		mono = toMono(p.Format, p.Data, mono[:0])
		m.add(samplePeak(mono))
	}
}

// reportLevels emits the level of each window until done is closed: at most one event per LevelInterval, and a
// window without packets reports 0.
func (c *child) reportLevels(m *levelMeter, done <-chan struct{}) {
	t := time.NewTicker(c.cfg.LevelInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			v := float64(m.take())
			c.out.emit(event{Type: evLevel, Level: &v})
		}
	}
}

// levelMeter keeps the peak of the current window; it is safe for concurrent use.
type levelMeter struct {
	mu   sync.Mutex
	peak float64
}

func (m *levelMeter) add(v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peak = max(m.peak, v)
}

// take returns the peak of the window, bounded to 0..1, and starts the next window.
func (m *levelMeter) take() values.SoundLevel {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := values.ClampSoundLevel(m.peak)
	m.peak = 0
	return v
}

// samplePeak is the largest magnitude of samples.
func samplePeak(samples []float32) float64 {
	var p float64
	for _, x := range samples {
		p = max(p, math.Abs(float64(x)))
	}
	return p
}

// CheckLevels captures source in a recorder child (the --recorder run mode with a level check request) for d, without
// recording, and calls levels with each level it reports (at most 20 a second, from this goroutine). source is
// captured as Start would: the whole computer is process loopback EXCLUDE of this app, an app process loopback
// INCLUDE of the process tree Deps.Apps finds (an app that does not run fails with an error wrapping
// recording.ErrSourceNotRunning); either may fall back to classic loopback in the child.
//
// The end tells how the capture ended: SoundOK when it ran for d; SoundBusy when a capture of a recording runs (no
// child is started) or a recording started meanwhile (the child is killed before the recording's starts);
// SoundNoDevice, SoundUnsupported or SoundFailed when the child could not capture. One check runs at a time
// (ErrSelfTestRunning). When ctx ends the child is killed and ctx's cause returned; past d and Config.CheckGrace it is
// killed too. The child is always reaped before CheckLevels returns.
func (r *Recorder) CheckLevels(ctx context.Context, source values.Source, d time.Duration,
	levels func(values.SoundLevel)) (values.SoundCause, error) {
	if d < time.Millisecond || d > MaxCheckDuration {
		return "", fmt.Errorf("audiocapture: %s: duration %v outside 1 ms..%v", opCheckLevels, d, MaxCheckDuration)
	}
	if !r.checking.TryLock() {
		return "", ErrSelfTestRunning
	}
	defer r.checking.Unlock()

	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	running := &runningCheck{stop: stop, done: make(chan struct{})}
	defer close(running.done)
	if !r.beginCheck(running) {
		return values.SoundBusy, nil
	}
	defer r.endCheck(running)

	busy := func() bool {
		if errors.Is(context.Cause(ctx), errRecordingStarted) {
			r.log.Info("sound check stopped: a recording started")
			return true
		}
		return false
	}
	target, err := r.checkTarget(ctx, source)
	if busy() {
		return values.SoundBusy, nil
	}
	if err != nil {
		return "", fmt.Errorf("audiocapture: %s: %w", opCheckLevels, err)
	}

	ctx, cancel := context.WithTimeoutCause(ctx, d+r.cfg.CheckGrace,
		fmt.Errorf("the sound check did not finish within %v", d+r.cfg.CheckGrace))
	defer cancel()
	end, err := r.runLevelCheck(ctx, LevelRequest{Target: target, Duration: d.Truncate(time.Millisecond)}, levels)
	if busy() {
		return values.SoundBusy, nil
	}
	if err != nil {
		return "", fmt.Errorf("audiocapture: %s: %w", opCheckLevels, err)
	}
	r.log.Info("sound check", slog.String("source", source.String()), slog.String("target", string(target.Mode)),
		slog.String("end", string(end)))
	return end, nil
}

// checkTarget is the target a recording of source would start with.
func (r *Recorder) checkTarget(ctx context.Context, source values.Source) (Target, error) {
	if source.IsWholeComputer() {
		return Target{Mode: ModeExclude, PID: r.cfg.OwnPID}, nil
	}
	if r.deps.Apps == nil {
		return Target{}, fmt.Errorf("%w: this build cannot find %s", ErrInvalidTarget, source.App())
	}
	proc, err := r.deps.Apps.Find(ctx, source.App())
	if err != nil {
		return Target{}, err
	}
	target := Target{Mode: ModeInclude, PID: proc.PID}
	return target, target.Validate()
}

// runningCheck is the sound check that runs, for a recording start to stop.
type runningCheck struct {
	stop context.CancelCauseFunc
	done chan struct{} // closed when CheckLevels returns, its child reaped
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

// stopCheck stops the running sound check c, if any, and waits a little for it to end, so that its capture is gone
// before the recording's child starts. The check's child is killed at once; a check that still has not ended is left
// behind.
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

// runLevelCheck runs one level check child until it has exited and is reaped. Levels are forwarded as they come;
// malformed ones were dropped by the decoder.
func (r *Recorder) runLevelCheck(ctx context.Context, req LevelRequest, levels func(values.SoundLevel)) (values.SoundCause, error) {
	line, err := encodeLevelCheck(req)
	if err != nil {
		return "", err
	}
	p, err := r.deps.Launcher.Launch()
	if err != nil {
		return "", err
	}
	log := r.log.With(slog.Int("pid", p.PID()))
	log.Info("sound check child launched", slog.String("target", string(req.Target.Mode)))

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
	if _, err := p.Stdin().Write(line); err != nil {
		kill("send the request: " + shortText(err.Error()))
	}

	var (
		end   values.SoundCause
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
			case evLevel:
				if l, err := values.NewSoundLevel(*ev.Level); err == nil && cause == nil && levels != nil {
					levels(l)
				}
			case evStarted:
				log.Info("sound check capture started", slog.String("mode", ev.Mode), slog.String("device", ev.Device))
			case evSwitch:
				log.Info("sound check capture switched", slog.String("from", ev.From), slog.String("to", ev.To),
					slog.String("cause", ev.Cause))
			case evStopped:
				end = values.SoundOK
				_ = p.Stdin().Close() // the child is done; a closed stdin lets a lingering one end
			case evFailed:
				end = failedCause(ev)
				log.Warn("sound check capture failed", slog.String("cause", string(end)), slog.String("reason", ev.Reason))
				_ = p.Stdin().Close()
			}

		case <-done:
			done, cause = nil, context.Cause(ctx)
			kill(shortText(cause.Error()))

		case code := <-exited:
			_ = p.Stdin().Close()
			log.Info("sound check child exited", slog.Int("code", code))
			switch {
			case cause != nil:
				return "", cause
			case end != "":
				return end, nil
			}
			return "", fmt.Errorf("the sound check child ended without a result (code %d)", code)
		}
	}
}

// failedCause is the sound cause of a failed event: its cause, unsupported for a build without capture, failed
// otherwise.
func failedCause(ev event) values.SoundCause {
	c := values.SoundCause(ev.Cause)
	switch {
	case ev.Unavailable:
		return values.SoundUnsupported
	case c == values.SoundNoDevice || c == values.SoundUnsupported:
		return c
	}
	return values.SoundFailed
}
