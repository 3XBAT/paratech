package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

const (
	maxDiagEntries   = 32   // attempts and switches kept per capture
	maxStderrLines   = 200  // child stderr lines logged per child
	maxStderrLineLen = 1024 // longer stderr lines are skipped
)

// Diagnostics is a snapshot of one capture (TASK-058): the domain value the Recorder port returns. It holds no audio
// content and no window titles.
type Diagnostics = values.CaptureDiagnostics

// Stats are the counters a child reports.
type Stats = values.CaptureStats

// binding is the app a capture follows across restarts of the app (TASK-059). Only the supervisor uses it.
type binding struct {
	proc      ProcessIdentity // the process the target names now
	waiting   bool            // proc exited and no new instance is captured yet
	rebinds   int
	capLogged bool
}

// capture is one recording's capture: its supervisor goroutine and the children it runs one after another.
type capture struct {
	id     shared.RecordingID
	chunks shared.ArtifactRef
	// app, target and bind belong to the supervisor (newCapture sets them first): other goroutines read the
	// diagnostics. A re-bind changes target; a switch of the user (TASK-082) changes all three.
	app    shared.AppName // the app the capture follows; empty for none
	target Target
	bind   *binding // nil unless the capture follows an app

	ctx    context.Context // ends with a stop, an abort or the end: bounds the checks of the app
	cancel context.CancelFunc

	started chan error    // the outcome of the first child's start; buffered
	done    chan struct{} // closed when no goroutine or process of the capture is left

	stopOnce, abortOnce sync.Once
	stopCh              chan struct{} // Stop: close the child's stdin
	abortCh             chan struct{} // kill the child at once (abandoned start, Stop past its context)
	switchKick          chan struct{} // buffered: a switch of the user is pending

	mu        sync.Mutex
	ended     bool
	begun     bool       // the first child runs: switches are accepted
	pending   *switchReq // a switch the supervisor has not taken yet
	answering *switchReq // a switch taken and not answered yet
	err       error      // why the capture ended by itself; nil after a stop
	stopErr   error      // the stop was not clean
	diag      Diagnostics
	live      values.LiveSound // the levels the children report (TASK-099); only in memory
}

func newCapture(id shared.RecordingID, chunks shared.ArtifactRef, target Target, bind *binding) *capture {
	ctx, cancel := context.WithCancel(context.Background())
	c := &capture{
		id: id, chunks: chunks, target: target, bind: bind, ctx: ctx, cancel: cancel,
		started: make(chan error, 1), done: make(chan struct{}),
		stopCh: make(chan struct{}), abortCh: make(chan struct{}), switchKick: make(chan struct{}, 1),
		diag: Diagnostics{TargetMode: target.Mode, TargetPID: target.PID},
	}
	if bind != nil {
		c.app = bind.proc.App
	}
	c.diag.App = c.app
	return c
}

func (c *capture) requestStop() { c.stopOnce.Do(func() { close(c.stopCh) }); c.cancel() }
func (c *capture) abort()       { c.abortOnce.Do(func() { close(c.abortCh) }); c.cancel() }
func (c *capture) release()     { c.cancel() }

func (c *capture) stopping() bool {
	select {
	case <-c.stopCh:
		return true
	case <-c.abortCh:
		return true
	default:
		return false
	}
}

func (c *capture) isEnded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ended
}

// end records the final state; err is nil for a stop.
func (c *capture) end(err, stopErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ended, c.err, c.stopErr = true, err, stopErr
	c.diag.Running = false
	if err != nil {
		c.diag.Failure = shortText(err.Error())
	}
	c.refuseSwitches()
}

func (c *capture) result() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *capture) stopError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopErr
}

func (c *capture) snapshot() Diagnostics {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.diag
	d.Attempts = append([]Attempt(nil), d.Attempts...)
	d.Switches = append([]Switch(nil), d.Switches...)
	return d
}

// sound returns what the capture hears; ok is false once it ended.
func (c *capture) sound() (values.LiveSound, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live, !c.ended
}

func (c *capture) hear(l values.SoundLevel, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.live = c.live.Hear(l, at)
}

func (c *capture) update(f func(d *Diagnostics)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f(&c.diag)
}

// wait waits for ch and reports false if a stop came first. A pending switch of the user ends the wait early (true):
// the supervisor takes it next. A kick whose switch was taken already does not.
func (c *capture) wait(ch <-chan time.Time) bool {
	for {
		select {
		case <-ch:
			return true
		case <-c.switchKick:
			if c.switchPending() {
				return true
			}
		case <-c.stopCh:
			return false
		case <-c.abortCh:
			return false
		}
	}
}

func appendBounded[T any](s []T, v T) []T {
	if len(s) >= maxDiagEntries {
		s = append(s[:0], s[1:]...)
	}
	return append(s, v)
}

// endKind is how a child ended.
type endKind int

const (
	endCrashed endKind = iota // exited without a final event, or was killed as hung: restarted
	endStopped                // stopped on request
	endFailed                 // reported that the capture ended by itself: not restarted
)

// runOutcome is what the supervisor needs to know about a finished child.
type runOutcome struct {
	kind        endKind
	reason      string
	started     bool
	unavailable bool
	mode        Mode          // the mode the child ended in
	runStart    time.Time     // when the capture of the child began (or the child was launched)
	closedAudio time.Duration // audio in the chunks the child closed
	stopErr     error
	gone        bool             // the app's process exited while the child ran
	rebind      *ProcessIdentity // a new instance of the app: the child was stopped to capture it
}

// supervise runs the children of a capture until it is stopped, fails or runs out of restarts.
func (r *Recorder) supervise(c *capture) {
	defer close(c.done)
	defer c.release()
	var (
		lead     time.Duration
		floor    Mode
		restarts int
	)
	for first := true; ; first = false {
		// A switch of the user: the next child captures the new target from the start of the fallback chain.
		sw := c.takeSwitch()
		var prev switchPrev
		if sw != nil {
			prev = r.applySwitch(c, sw, floor)
			floor = ""
		}
		req := Request{Recording: c.id, Chunks: c.chunks, Target: c.target, ChunkSeconds: r.cfg.ChunkSeconds,
			LeadSilence: lead, Floor: floor}
		out := r.runChild(c, req, first, sw)

		if first && !out.started {
			c.started <- startFailure(out)
			c.end(nil, nil)
			return
		}

		if c.stopping() { // a failure during the stop still counts as the stop
			c.end(nil, out.stopErr)
			r.retire(c)
			return
		}
		// The child of a switch that never ran: back to the old source, without a restart.
		reverted := sw != nil && !out.started
		if reverted {
			r.revertSwitch(c, prev, out.reason)
			floor = prev.floor
			c.answer(sw, fmt.Errorf("%w: %s", ErrSwitchFailed, shortText(out.reason)))
		}
		next := out.rebind
		var waitApp bool
		if reverted {
			next = nil
			// Back to an app that had exited: wait for it again, not a child on its dead process.
			waitApp = c.bind != nil && c.bind.waiting
		}
		switched := (reverted && !waitApp) || c.switchPending()
		if !switched && next == nil && (waitApp || r.lostApp(c, out, floor)) {
			// The app exited: the end of its child is no crash and uses no restart; the capture waits for the app.
			found, ok := r.waitForApp(c)
			if !ok {
				c.end(nil, nil)
				r.retire(c)
				return
			}
			next = found
			switched = found == nil // the user chose another source meanwhile
		}
		switch {
		case switched: // the next child captures the source the user chose; no restart is counted
		case next != nil:
			r.rebind(c, *next)
			if c.stopping() {
				c.end(nil, nil)
				r.retire(c)
				return
			}
		default:
			if out.kind == endStopped { // nobody asked: the capture must go on
				out.kind, out.reason = endCrashed, "the recorder child stopped without a stop request"
			}
			switch {
			case out.kind == endFailed:
				r.fail(c, fmt.Errorf("%w: %s", ErrCaptureFailed, out.reason))
				return
			case restarts >= r.cfg.MaxRestarts:
				r.fail(c, fmt.Errorf("%w: %s; no restart left after %d", ErrCaptureFailed, out.reason, restarts))
				return
			}

			restarts++
			delay := r.cfg.RestartBackoff << (restarts - 1)
			r.log.Warn("restarting the recorder child", slog.String("recording", c.id.String()),
				slog.Int("restart", restarts), slog.Duration("delay", delay), slog.String("reason", shortText(out.reason)))
			if !c.wait(r.after(delay)) || c.stopping() { // both may be ready together
				c.end(nil, nil)
				r.retire(c)
				return
			}
			c.update(func(d *Diagnostics) { d.Restarts = restarts; d.Failure = shortText(out.reason) })
		}

		// The new child starts with the time the chunks miss as silence, in the mode the last one ended in.
		missed := req.LeadSilence + r.now().Sub(out.runStart) - out.closedAudio
		var capped bool
		if lead, capped = restartLead(missed); capped {
			r.log.Warn("the restart misses more audio than the lead silence covers; the track is shorter",
				slog.String("recording", c.id.String()), slog.Duration("missed", missed), slog.Duration("lead_silence", lead))
		}
		if out.mode == ModeSystem && c.target.Mode != ModeSystem {
			floor = ModeSystem // the fallback is one-way within a recording
		}
	}
}

// probe is one check of the app of a capture.
type probe struct {
	gone bool             // the process the target names is gone
	next *ProcessIdentity // a new instance of the app, when looked for and found
}

// probeApp checks proc unless it is known to be gone, and then looks for a new instance of its app if find is true.
// It may run on its own goroutine: it uses only its arguments.
func (r *Recorder) probeApp(ctx context.Context, proc ProcessIdentity, gone, find bool) probe {
	if !gone {
		same, err := r.deps.Apps.Same(ctx, proc)
		if err != nil || same { // an unknown answer keeps the capture as it is
			return probe{}
		}
	}
	p := probe{gone: true}
	if find {
		if next, err := r.deps.Apps.Find(ctx, proc.App); err == nil {
			p.next = &next
		}
	}
	return p
}

// mayRebind reports whether the capture may still follow a new instance of its app; past the cap it logs once and
// the capture keeps waiting.
func (r *Recorder) mayRebind(c *capture) bool {
	if c.bind.rebinds < r.cfg.MaxRebinds {
		return true
	}
	if !c.bind.capLogged {
		c.bind.capLogged = true
		r.log.Warn("the app restarted too often; the capture waits without following it",
			slog.String("recording", c.id.String()), slog.String("app", c.app.String()), slog.Int("rebinds", c.bind.rebinds))
	}
	return false
}

// markGone records that the process of the app exited; it logs once per exit.
func (r *Recorder) markGone(c *capture) {
	if c.bind.waiting {
		return
	}
	c.bind.waiting = true
	r.log.Info("capture target exited; waiting for the app", slog.String("recording", c.id.String()),
		slog.String("app", c.app.String()), slog.Uint64("pid", uint64(c.bind.proc.PID)))
	c.update(func(d *Diagnostics) { d.WaitingForApp = true })
}

// lostApp reports whether the child of an app capture ended because the app's process is gone. A capture that fell
// back to system mode records everything anyway and is restarted as usual.
func (r *Recorder) lostApp(c *capture, out runOutcome, floor Mode) bool {
	if c.bind == nil || floor == ModeSystem || out.mode == ModeSystem {
		return false
	}
	if !r.probeApp(c.ctx, c.bind.proc, out.gone, false).gone {
		return false
	}
	r.markGone(c)
	return true
}

// waitForApp looks for a new instance of the app every RebindInterval until one appears (ok), the user switches to
// another source (ok with nil) or the capture stops.
func (r *Recorder) waitForApp(c *capture) (*ProcessIdentity, bool) {
	for {
		if !c.wait(r.after(r.cfg.RebindInterval)) || c.stopping() {
			return nil, false
		}
		if c.switchPending() {
			return nil, true
		}
		if !r.mayRebind(c) {
			continue
		}
		if p := r.probeApp(c.ctx, c.bind.proc, true, true); p.next != nil {
			return p.next, true
		}
	}
}

// rebind makes next the target of the capture: the next child captures it.
func (r *Recorder) rebind(c *capture, next ProcessIdentity) {
	old := c.bind.proc
	c.bind.proc, c.bind.waiting = next, false
	c.bind.rebinds++
	c.target = Target{Mode: ModeInclude, PID: next.PID}
	r.log.Info("capture target re-bound by exe name", slog.String("recording", c.id.String()),
		slog.String("app", c.app.String()), slog.Uint64("old_pid", uint64(old.PID)), slog.Uint64("new_pid", uint64(next.PID)))
	target, rebinds := c.target, c.bind.rebinds
	c.update(func(d *Diagnostics) {
		d.TargetMode, d.TargetPID, d.WaitingForApp, d.Rebinds = target.Mode, target.PID, false, rebinds
	})
}

// restartLead is the lead silence of a restarted child for the audio the chunks missed, capped at maxLeadSilence.
func restartLead(missed time.Duration) (lead time.Duration, capped bool) {
	if missed > maxLeadSilence {
		return maxLeadSilence, true
	}
	return max(missed, 0).Truncate(time.Millisecond), false
}

func (r *Recorder) fail(c *capture, err error) {
	r.log.Error("capture ended by itself", slog.String("recording", c.id.String()), slog.String("error", shortText(err.Error())))
	c.end(err, nil)
	r.retire(c)
}

// startFailure is the error Start returns for a first child that never ran.
func startFailure(out runOutcome) error {
	reason := out.reason
	if reason == "" {
		reason = "the recorder child ended before the capture started"
	}
	if out.unavailable {
		return fmt.Errorf("%w: %s", ErrUnavailable, reason)
	}
	return errors.New(reason)
}

// runChild runs one child until it has exited and is reaped. sw is the switch of the user this child carries out: it
// is answered once the child runs.
func (r *Recorder) runChild(c *capture, req Request, first bool, sw *switchReq) runOutcome {
	out := runOutcome{runStart: r.now()}
	line, err := encodeRequest(req)
	if err != nil {
		out.kind, out.reason = endFailed, err.Error()
		return out
	}
	p, err := r.deps.Launcher.Launch()
	if err != nil {
		out.reason = shortText(err.Error())
		return out
	}
	log := r.log.With(slog.String("recording", c.id.String()), slog.Int("pid", p.PID()))
	log.Info("recorder child launched", slog.String("target", string(req.effectiveTarget().Mode)),
		slog.Duration("lead_silence", req.LeadSilence))

	events := make(chan event)
	var readers sync.WaitGroup
	readers.Go(func() {
		defer close(events)
		r.readEvents(c, p.Stdout(), events, log)
	})
	readers.Go(func() { logStderr(p.Stderr(), log) })
	exited := make(chan int, 1)
	go func() {
		readers.Wait()
		code, err := p.Wait()
		if err != nil {
			log.Warn("reaping the recorder child failed", slog.String("error", shortText(err.Error())))
		}
		exited <- code
	}()

	kill := func(why string) {
		log.Warn("killing the recorder child", slog.String("reason", why))
		if err := p.Kill(); err != nil {
			log.Error("killing the recorder child failed", slog.String("error", shortText(err.Error())))
		}
	}
	if _, err := p.Stdin().Write(line); err != nil {
		out.reason = "send the start request: " + shortText(err.Error())
		kill(out.reason)
	}

	hang := time.NewTimer(r.cfg.HangTimeout)
	defer hang.Stop()
	var (
		final   string
		hung    bool
		grace   <-chan time.Time
		stopCh  = c.stopCh
		abortCh = c.abortCh

		// The watcher of an app capture: one probe at a time on its own goroutine, so the events keep flowing.
		watch   <-chan time.Time
		probes  = make(chan probe, 1)
		probing bool
	)
	if c.bind != nil && req.Floor != ModeSystem {
		watch = r.after(r.cfg.RebindInterval)
	}
	closeStdin := func() {
		stopCh = nil
		_ = p.Stdin().Close()
		if grace == nil {
			grace = r.after(r.cfg.StopGrace)
		}
	}
	applyProbe := func(pr probe) {
		if pr.gone {
			out.gone = true
			r.markGone(c)
		}
		switch {
		case pr.next != nil && out.rebind == nil:
			out.rebind = pr.next
			if stopCh != nil {
				log.Info("stopping the recorder child to capture the new instance of the app")
				closeStdin()
			}
		case pr.gone:
			r.mayRebind(c) // logs the cap once
		}
	}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			hang.Reset(r.cfg.HangTimeout)
			r.apply(c, ev, &out, log)
			switch ev.Type {
			case evStarted:
				if first && !out.started {
					c.update(func(*Diagnostics) { c.begun = true })
					c.started <- nil
				}
				if sw != nil && !out.started {
					c.answer(sw, nil)
				}
				out.started = true
			case evFailed, evStopped:
				final = ev.Type
				if grace == nil {
					grace = r.after(r.cfg.StopGrace) // it should exit now; a lingering child is killed
				}
			}

		case <-stopCh:
			closeStdin()

		case <-c.switchKick:
			if stopCh != nil && c.switchPending() {
				log.Info("stopping the recorder child to capture the source the user chose")
				closeStdin()
			}

		case <-watch:
			watch = nil
			switch {
			case stopCh == nil || out.rebind != nil || out.mode == ModeSystem: // stopping, or no app to follow any more
			case !out.started:
				watch = r.after(r.cfg.RebindInterval)
			default:
				probing = true
				proc, gone, find := c.bind.proc, out.gone, c.bind.rebinds < r.cfg.MaxRebinds
				go func() { probes <- r.probeApp(c.ctx, proc, gone, find) }()
			}

		case pr := <-probes:
			probing = false
			applyProbe(pr)
			if stopCh != nil && out.rebind == nil {
				watch = r.after(r.cfg.RebindInterval)
			}

		case <-grace:
			grace = nil
			if final == "" {
				out.stopErr = fmt.Errorf("the recorder child did not stop within %v and was killed", r.cfg.StopGrace)
			}
			kill("no exit after the stop")

		case <-abortCh:
			abortCh = nil
			kill("the capture was abandoned")

		case <-hang.C:
			hung = true
			kill(fmt.Sprintf("no event for %v", r.cfg.HangTimeout))

		case code := <-exited:
			if probing { // bounded by the timeout of the app checks; ended at once by a stop
				stopCh = nil // the child is gone: nothing to stop
				applyProbe(<-probes)
			}
			r.classify(&out, final, hung, code)
			c.update(func(d *Diagnostics) { d.Running = false })
			log.Info("recorder child exited", slog.Int("code", code), slog.String("end", out.reason))
			return out
		}
	}
}

// classify decides how a reaped child ended.
func (r *Recorder) classify(out *runOutcome, final string, hung bool, code int) {
	switch {
	case final == evStopped:
		out.kind = endStopped
	case final == evFailed:
		out.kind = endFailed
	case hung:
		out.kind, out.reason = endCrashed, fmt.Sprintf("the recorder child sent nothing for %v", r.cfg.HangTimeout)
	case out.reason == "":
		out.kind, out.reason = endCrashed, fmt.Sprintf("the recorder child exited unexpectedly (code %d)", code)
	}
}

// apply records an event in the outcome and the diagnostics.
func (r *Recorder) apply(c *capture, ev event, out *runOutcome, log *slog.Logger) {
	switch ev.Type {
	case evStarted:
		out.mode, out.runStart = Mode(ev.Mode), r.now()
		win, format := ev.Windows.version(), ev.Format.format().Stream()
		log.Info("capture running", slog.String("mode", ev.Mode), slog.String("format", format.String()),
			slog.String("windows", win.String()), slog.String("device", ev.Device))
		c.update(func(d *Diagnostics) {
			d.Running, d.Mode, d.Format, d.Windows, d.Device = true, Mode(ev.Mode), format, win, ev.Device
		})
	case evAttempt:
		a := Attempt{Mode: Mode(ev.Mode), OK: ev.OK, HResult: ev.HResult, Error: ev.Error, Device: ev.Device}
		attrs := []any{slog.String("mode", ev.Mode), slog.Bool("ok", ev.OK), slog.String("hresult", ev.HResult),
			slog.String("error", ev.Error), slog.String("device", ev.Device)}
		if ev.OK {
			log.Info("capture attempt", attrs...)
		} else {
			log.Warn("capture attempt failed", attrs...)
		}
		c.update(func(d *Diagnostics) {
			d.Attempts = appendBounded(d.Attempts, a)
			if a.OK && a.Device != "" { // an open that succeeded runs now: a reopen on another device included
				d.Device = a.Device
			}
		})
	case evSwitch:
		out.mode = Mode(ev.To)
		sw := Switch{From: Mode(ev.From), To: Mode(ev.To), Cause: values.SwitchCause(ev.Cause), Reason: ev.Reason}
		log.Warn("capture mode switched", slog.String("from", ev.From), slog.String("to", ev.To),
			slog.String("cause", ev.Cause), slog.String("reason", ev.Reason))
		c.update(func(d *Diagnostics) {
			d.Mode = sw.To
			d.Switches = appendBounded(d.Switches, sw)
		})
	case evChunk:
		out.closedAudio += durationOf(ev.Frames, ev.Rate)
		c.update(func(d *Diagnostics) { d.Chunks++ })
	case evStats:
		c.update(func(d *Diagnostics) {
			d.Stats = Stats{Packets: ev.Packets, SilentPackets: ev.SilentPackets, AudiblePackets: ev.AudiblePackets,
				Frames: ev.Frames, Reopens: ev.Reopens}
		})
	case evFailed:
		out.reason, out.unavailable = ev.Reason, ev.Unavailable
		log.Warn("capture failed", slog.String("reason", ev.Reason))
	case evStopped:
		log.Info("capture stopped", slog.Int64("frames", ev.Frames))
	case evLevel: // the decoder checked the level
		c.hear(values.ClampSoundLevel(*ev.Level), r.now())
	}
}

// readEvents parses the child's stdout until EOF. Bad lines are counted, logged briefly and ignored.
func (r *Recorder) readEvents(c *capture, stdout io.Reader, events chan<- event, log *slog.Logger) {
	scanEvents(stdout, events, log, func() { c.update(func(d *Diagnostics) { d.InvalidLines++ }) })
}

// scanEvents parses a child's stdout until EOF and sends the events. Bad lines are logged briefly, reported to bad
// (when not nil) and ignored.
func scanEvents(stdout io.Reader, events chan<- event, log *slog.Logger, bad func()) {
	lines := newLineReader(stdout, maxLineBytes)
	for {
		line, oversized, err := lines.next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Warn("reading the recorder child failed", slog.String("error", shortText(err.Error())))
			}
			return
		}
		var ev event
		if !oversized {
			ev, err = decodeEvent(line)
		}
		if oversized || err != nil {
			if bad != nil {
				bad()
			}
			why := "oversized line"
			if err != nil {
				why = err.Error()
			}
			log.Warn("ignoring a bad line of the recorder child", slog.String("error", shortText(why)))
			continue
		}
		events <- ev
	}
}

// logStderr logs the child's stderr line by line, bounded, until EOF.
func logStderr(stderr io.Reader, log *slog.Logger) {
	lines := newLineReader(stderr, maxStderrLineLen)
	logged, dropped := 0, 0
	for {
		line, oversized, err := lines.next()
		if err != nil {
			break
		}
		if oversized || logged >= maxStderrLines {
			dropped++
			continue
		}
		logged++
		log.Info("recorder child", slog.String("stderr", shortText(string(line))))
	}
	if dropped > 0 {
		log.Info("recorder child stderr lines not logged", slog.Int("count", dropped))
	}
}
