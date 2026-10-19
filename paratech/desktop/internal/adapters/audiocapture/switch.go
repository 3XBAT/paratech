package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// The user's switch of a running recording to another source (TASK-082): the same capture, the same chunk directory
// and numbering, a new target for the next recorder child. It reuses the re-bind path of the supervisor: the child
// is stopped (its last chunk is closed) and the next one captures the new target, with the gap as lead silence.

const opSwitchCapture = "switch capture"

var (
	// ErrSwitchRefused means the capture cannot switch now: it is starting, stopping or ended, or another switch is
	// under way. The capture goes on as it was.
	ErrSwitchRefused = errors.New("audiocapture: the capture cannot switch now")

	// ErrSwitchFailed means the recorder child of the new source did not start; the capture goes back to the source it
	// had. It wraps recording.ErrSourceDidNotStart.
	ErrSwitchFailed = fmt.Errorf("audiocapture: %w", recording.ErrSourceDidNotStart)
)

// switchReq is one switch handed to the supervisor.
type switchReq struct {
	target Target
	bind   *binding       // nil for the whole computer
	app    shared.AppName // empty for the whole computer
	reply  chan error     // buffered; answered exactly once
}

// switchPrev is what a switch replaced, to go back to it when the new source does not start.
type switchPrev struct {
	target Target
	bind   *binding
	app    shared.AppName
	floor  Mode
}

// Switch makes the running capture of id record source from now on (TASK-082). The new target is resolved first: an
// app that does not run fails with an error wrapping recording.ErrSourceNotRunning, and on any failure the capture goes
// on unchanged. Then the current child is stopped (its last chunk closed) and the next one captures the new target
// into the same chunk directory, the numbering continuing; there is one child at any time. Switch returns once the
// new child runs: when it does not start, the capture goes back to its old source and the error wraps
// ErrSwitchFailed. A capture that is starting, stopping or ended refuses with ErrSwitchRefused. Switching to the
// source the capture records already does nothing. When ctx ends first, Switch returns its error and the switch may
// still happen (Diagnostics tells).
func (r *Recorder) Switch(ctx context.Context, id shared.RecordingID, source values.Source) error {
	c := r.lookup(id)
	if c == nil || c.isEnded() {
		return fmt.Errorf("audiocapture: %s %s: %w", opSwitchCapture, id, ErrNotCapturing)
	}
	if c.stopping() {
		return fmt.Errorf("audiocapture: %s %s: %w: the capture is stopping", opSwitchCapture, id, ErrSwitchRefused)
	}
	if d := c.snapshot(); d.App == source.App() && !d.WaitingForApp &&
		(source.IsWholeComputer() == (d.TargetMode == ModeExclude)) {
		return nil
	}

	req, err := r.resolveSwitch(ctx, source)
	if err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opSwitchCapture, id, err)
	}
	if err := c.offerSwitch(req); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opSwitchCapture, id, err)
	}
	select {
	case err := <-req.reply:
		if err != nil {
			return fmt.Errorf("audiocapture: %s %s: %w", opSwitchCapture, id, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("audiocapture: %s %s: %w", opSwitchCapture, id, ctx.Err())
	}
}

// resolveSwitch finds the target of source, as Start does.
func (r *Recorder) resolveSwitch(ctx context.Context, source values.Source) (*switchReq, error) {
	req := &switchReq{reply: make(chan error, 1)}
	if source.IsWholeComputer() {
		req.target = Target{Mode: ModeExclude, PID: r.cfg.OwnPID}
		return req, nil
	}
	if r.deps.Apps == nil {
		return nil, fmt.Errorf("%w: this build cannot find %s", ErrInvalidTarget, source.App())
	}
	proc, err := r.deps.Apps.Find(ctx, source.App())
	if err != nil {
		return nil, err
	}
	req.target = Target{Mode: ModeInclude, PID: proc.PID}
	if err := req.target.Validate(); err != nil {
		return nil, err
	}
	req.bind, req.app = &binding{proc: proc}, proc.App
	return req, nil
}

// offerSwitch hands req to the supervisor: only while the capture runs and no other switch is under way.
func (c *capture) offerSwitch(req *switchReq) error {
	c.mu.Lock()
	switch {
	case c.ended || c.stopping():
		c.mu.Unlock()
		return fmt.Errorf("%w: the capture is stopping", ErrSwitchRefused)
	case !c.begun:
		c.mu.Unlock()
		return fmt.Errorf("%w: the capture is starting", ErrSwitchRefused)
	case c.pending != nil || c.answering != nil:
		c.mu.Unlock()
		return fmt.Errorf("%w: another switch is under way", ErrSwitchRefused)
	}
	c.pending = req
	c.mu.Unlock()

	select {
	case c.switchKick <- struct{}{}:
	default: // a kick is waiting already
	}
	return nil
}

// switchPending reports a switch the supervisor has not taken yet.
func (c *capture) switchPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending != nil
}

// takeSwitch takes the pending switch; the supervisor answers it once the next child runs or fails.
func (c *capture) takeSwitch() *switchReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := c.pending
	c.pending, c.answering = nil, req
	return req
}

// answer replies to req unless it was answered already.
func (c *capture) answer(req *switchReq, err error) {
	c.mu.Lock()
	if c.answering != req {
		c.mu.Unlock()
		return
	}
	c.answering = nil
	c.mu.Unlock()
	req.reply <- err
}

// refuseSwitches answers the switches still open when the capture ends; c.mu is held.
func (c *capture) refuseSwitches() {
	for _, req := range []*switchReq{c.pending, c.answering} {
		if req != nil {
			req.reply <- fmt.Errorf("%w: the capture ended", ErrSwitchRefused)
		}
	}
	c.pending, c.answering = nil, nil
}

// applySwitch makes req the target of the capture: the next child captures it. It returns what it replaced.
func (r *Recorder) applySwitch(c *capture, req *switchReq, floor Mode) switchPrev {
	prev := switchPrev{target: c.target, bind: c.bind, app: c.app, floor: floor}
	if req.bind != nil && prev.bind != nil {
		req.bind.rebinds = prev.bind.rebinds // the cap of re-binds is per recording
	}
	c.target, c.bind, c.app = req.target, req.bind, req.app
	r.log.Info("capture switched by the user", slog.String("recording", c.id.String()),
		slog.String("from_app", prev.app.String()), slog.String("from_target", string(prev.target.Mode)),
		slog.String("app", c.app.String()), slog.String("target", string(c.target.Mode)),
		slog.Uint64("target_pid", uint64(c.target.PID)))
	target, app := c.target, c.app
	c.update(func(d *Diagnostics) {
		d.TargetMode, d.TargetPID, d.App, d.WaitingForApp = target.Mode, target.PID, app, false
		d.SourceSwitches++
	})
	return prev
}

// revertSwitch goes back to the source before a switch whose child did not start.
func (r *Recorder) revertSwitch(c *capture, prev switchPrev, reason string) {
	r.log.Warn("the source the user chose did not start; the capture goes back", slog.String("recording", c.id.String()),
		slog.String("app", c.app.String()), slog.String("back_to", prev.app.String()), slog.String("reason", shortText(reason)))
	c.target, c.bind, c.app = prev.target, prev.bind, prev.app
	waiting := prev.bind != nil && prev.bind.waiting
	c.update(func(d *Diagnostics) {
		d.TargetMode, d.TargetPID, d.App, d.WaitingForApp = prev.target.Mode, prev.target.PID, prev.app, waiting
		d.SourceSwitches--
	})
}
