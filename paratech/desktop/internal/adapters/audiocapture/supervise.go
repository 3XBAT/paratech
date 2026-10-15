package audiocapture

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/shared"
)

const (
	maxDiagEntries   = 32   // attempts and switches kept per capture
	maxStderrLines   = 200  // child stderr lines logged per child
	maxStderrLineLen = 1024 // longer stderr lines are skipped
)

// Diagnostics is a snapshot of one capture for the diagnostics screen (TASK-058). It holds no audio content.
type Diagnostics struct {
	Running      bool
	Target       Target
	Mode         Mode   // the mode the capture runs in now (after a fallback: system)
	Format       Format // the format of the running stream
	Build        string // Windows build reported by the child
	Attempts     []Attempt
	Switches     []Switch
	Chunks       int // closed chunks
	Stats        Stats
	Restarts     int
	Failure      string // why the capture or the last child ended unexpectedly
	InvalidLines int    // malformed or oversized event lines of the children
}

// Stats are the counters a child reports.
type Stats struct {
	Packets       uint64
	SilentPackets uint64
	Frames        int64
	Reopens       uint64
}

// capture is one recording's capture: its supervisor goroutine and the children it runs one after another.
type capture struct {
	id     shared.RecordingID
	chunks shared.ArtifactRef
	target Target

	started chan error    // the outcome of the first child's start; buffered
	done    chan struct{} // closed when no goroutine or process of the capture is left

	stopOnce, abortOnce sync.Once
	stopCh              chan struct{} // Stop: close the child's stdin
	abortCh             chan struct{} // kill the child at once (abandoned start, Stop past its context)

	mu      sync.Mutex
	ended   bool
	err     error // why the capture ended by itself; nil after a stop
	stopErr error // the stop was not clean
	diag    Diagnostics
}

func newCapture(id shared.RecordingID, chunks shared.ArtifactRef, target Target) *capture {
	return &capture{
		id: id, chunks: chunks, target: target,
		started: make(chan error, 1), done: make(chan struct{}),
		stopCh: make(chan struct{}), abortCh: make(chan struct{}),
		diag: Diagnostics{Target: target},
	}
}

func (c *capture) requestStop() { c.stopOnce.Do(func() { close(c.stopCh) }) }
func (c *capture) abort()       { c.abortOnce.Do(func() { close(c.abortCh) }) }

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

func (c *capture) update(f func(d *Diagnostics)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f(&c.diag)
}

// wait waits for ch and reports false if a stop came first.
func (c *capture) wait(ch <-chan time.Time) bool {
	select {
	case <-ch:
		return true
	case <-c.stopCh:
		return false
	case <-c.abortCh:
		return false
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
}

// supervise runs the children of a capture until it is stopped, fails or runs out of restarts.
func (r *Recorder) supervise(c *capture) {
	defer close(c.done)
	var (
		lead     time.Duration
		floor    Mode
		restarts int
	)
	for first := true; ; first = false {
		req := Request{Recording: c.id, Chunks: c.chunks, Target: c.target, ChunkSeconds: r.cfg.ChunkSeconds,
			LeadSilence: lead, Floor: floor}
		out := r.runChild(c, req, first)

		if first && !out.started {
			c.started <- startFailure(out)
			c.end(nil, nil)
			return
		}

		switch {
		case out.kind == endStopped || c.stopping(): // a failure during the stop still counts as the stop
			c.end(nil, out.stopErr)
			r.retire(c)
			return
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
		if !c.wait(r.after(delay)) {
			c.end(nil, nil)
			r.retire(c)
			return
		}

		// The new child starts with the time the chunks miss as silence, in the mode the last one ended in.
		lead = req.LeadSilence + r.now().Sub(out.runStart) - out.closedAudio
		lead = min(max(lead, 0), maxLeadSilence).Truncate(time.Millisecond)
		if out.mode == ModeSystem && c.target.Mode != ModeSystem {
			floor = ModeSystem // the fallback is one-way within a recording
		}
		c.update(func(d *Diagnostics) { d.Restarts = restarts; d.Failure = shortText(out.reason) })
	}
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

// runChild runs one child until it has exited and is reaped.
func (r *Recorder) runChild(c *capture, req Request, first bool) runOutcome {
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
	)
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
					c.started <- nil
				}
				out.started = true
			case evFailed, evStopped:
				final = ev.Type
				if grace == nil {
					grace = r.after(r.cfg.StopGrace) // it should exit now; a lingering child is killed
				}
			}

		case <-stopCh:
			stopCh = nil
			_ = p.Stdin().Close()
			if grace == nil {
				grace = r.after(r.cfg.StopGrace)
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
		log.Info("capture running", slog.String("mode", ev.Mode), slog.String("format", ev.Format.format().String()),
			slog.String("windows", ev.Build))
		c.update(func(d *Diagnostics) {
			d.Running, d.Mode, d.Format, d.Build = true, Mode(ev.Mode), ev.Format.format(), ev.Build
		})
	case evAttempt:
		a := Attempt{Mode: Mode(ev.Mode), OK: ev.OK, HResult: ev.HResult, Error: ev.Error}
		log.Info("capture attempt", slog.String("mode", ev.Mode), slog.Bool("ok", ev.OK), slog.String("hresult", ev.HResult),
			slog.String("error", ev.Error))
		c.update(func(d *Diagnostics) { d.Attempts = appendBounded(d.Attempts, a) })
	case evSwitch:
		out.mode = Mode(ev.To)
		log.Warn("capture mode switched", slog.String("from", ev.From), slog.String("to", ev.To), slog.String("reason", ev.Reason))
		c.update(func(d *Diagnostics) {
			d.Mode = Mode(ev.To)
			d.Switches = appendBounded(d.Switches, Switch{From: Mode(ev.From), To: Mode(ev.To), Reason: ev.Reason})
		})
	case evChunk:
		out.closedAudio += durationOf(ev.Frames, ev.Rate)
		c.update(func(d *Diagnostics) { d.Chunks++ })
	case evStats:
		c.update(func(d *Diagnostics) {
			d.Stats = Stats{Packets: ev.Packets, SilentPackets: ev.SilentPackets, Frames: ev.Frames, Reopens: ev.Reopens}
		})
	case evFailed:
		out.reason, out.unavailable = ev.Reason, ev.Unavailable
		log.Warn("capture failed", slog.String("reason", ev.Reason))
	case evStopped:
		log.Info("capture stopped", slog.Int64("frames", ev.Frames))
	}
}

// readEvents parses the child's stdout until EOF. Bad lines are counted, logged briefly and ignored.
func (r *Recorder) readEvents(c *capture, stdout io.Reader, events chan<- event, log *slog.Logger) {
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
			c.update(func(d *Diagnostics) { d.InvalidLines++ })
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
