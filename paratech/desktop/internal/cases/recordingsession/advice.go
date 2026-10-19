package recordingsession

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// The silent-source advice and the switch of the source of a running recording (TASK-082). The decision is the
// values.SourceWatch of the session; this file feeds it and carries out the answer of the user.

// advise looks at once and then every advice interval whether the capture heard sound and, while it hears nothing,
// which apps play, until ctx ends or a stop is requested. It writes nothing: the advice lives in the session
// (Info.Advice) and goes to the AdviceListener. When it returns, an advice shown is withdrawn: the stats of a capture
// that is ending freeze and would read as silence.
func (s *Service) advise(ctx context.Context, sess *session) {
	defer func() {
		if advice, changed := sess.updateWatch(func(w values.SourceWatch) values.SourceWatch {
			return values.NewSourceWatch(w.Source())
		}); changed {
			s.adviceChanged(sess.id, advice)
		}
	}()

	var (
		prev   values.CaptureStats
		failed bool // the last listing failed: logged once
	)
	// The first look comes at once: the silence counts from the start of the capture.
	for first := true; ; first = false {
		if !first && s.sleeper.Sleep(ctx, s.adviceEvery) != nil {
			return
		}
		if ctx.Err() != nil || sess.stopRequested() {
			return
		}

		// The look belongs to the source watched now; one that a switch overtakes is dropped (session.observe).
		source, gen := sess.watching()
		d, ok := s.recorder.Diagnostics(ctx, sess.id)
		obs := values.SourceObservation{At: s.clock.Now(), Heard: ok && d.Stats.HeardSince(prev)}
		if ok {
			prev = d.Stats
		}
		if !obs.Heard && !source.IsWholeComputer() {
			lctx, cancel := context.WithTimeout(ctx, adviceListTimeout)
			apps, err := s.apps.List(lctx)
			cancel()
			switch {
			case err == nil:
				obs.Listed, obs.Apps, failed = true, apps, false
			case ctx.Err() != nil:
				return
			case !failed:
				failed = true
				s.log.Warn("the apps could not be listed for the source advice", slog.String("recording", sess.id.String()),
					slog.Any("error", err))
			}
		}
		if ctx.Err() != nil {
			return
		}
		if advice, changed := sess.observe(gen, obs); changed {
			s.adviceChanged(sess.id, advice)
		}
	}
}

// adviceChanged logs a new or withdrawn advice and tells the listener.
func (s *Service) adviceChanged(id shared.RecordingID, advice values.SourceAdvice) {
	if advice.IsZero() {
		s.log.Info("source advice withdrawn", slog.String("recording", id.String()))
	} else {
		// Executable names only, no window titles.
		s.log.Info("the chosen app is silent while another one plays; advising a switch",
			slog.String("recording", id.String()), slog.String("silent", advice.Silent.String()),
			slog.String("suggest", advice.Suggest.String()))
	}
	s.tellAdvice(id, advice)
}

// tellAdvice hands the advice to the listener; it runs detached and bounded, and its error is only logged.
func (s *Service) tellAdvice(id shared.RecordingID, advice values.SourceAdvice) {
	if s.advice == nil {
		return
	}
	dctx, release := s.detached(s.life)
	defer release()
	if err := s.advice.SourceAdviceChanged(dctx, id, advice); err != nil {
		s.log.Warn("the source advice could not be shown", slog.String("recording", id.String()), slog.Any("error", err))
	}
}

// SwitchSource makes the running recording id capture source from now on (TASK-082): the same recording, the same
// chunks. The advice is cleared and watches the new source. A recording that this process does not capture yields
// ErrNotRunning (shared.ErrNotFound when there is none); one that is stopping or finished, or a capture that refuses,
// yields ErrSwitchRefused; an app that does not run wraps recording.ErrSourceNotRunning, a new source whose capture
// did not start ErrSwitchFailed. On any error the recording
// goes on with the source it had. Switching to the current source changes nothing in the capture.
func (s *Service) SwitchSource(ctx context.Context, id shared.RecordingID, source values.Source) error {
	sess, err := s.running(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSwitch, id, err)
	}
	sess.switching.Lock()
	defer sess.switching.Unlock()

	info, _ := sess.snapshot()
	if sess.stopRequested() || !info.Status.CanSwitchSource() {
		return fmt.Errorf("%s %s: %w: %s", opSwitch, id, ErrSwitchRefused, info.Status)
	}
	// No short cut on the session's source: the recorder does nothing for its current source, and the session's
	// source may lag behind a switch that was given up but done.
	if err := s.recorder.Switch(ctx, id, source); err != nil {
		cancelled := ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		switch {
		case errors.Is(err, recording.ErrSourceDidNotStart):
			err = fmt.Errorf("%w: %w", ErrSwitchFailed, err)
		case !errors.Is(err, recording.ErrSourceNotRunning) && !cancelled:
			err = fmt.Errorf("%w: %w", ErrSwitchRefused, err)
		}
		s.log.Warn("switching the source failed; the recording goes on as it was", slog.String("recording", id.String()),
			slog.String("source", source.String()), slog.Any("error", err))
		if cancelled {
			s.resyncSource(ctx, sess)
		}
		return fmt.Errorf("%s %s: %w", opSwitch, id, err)
	}

	s.log.Info("recording source switched", slog.String("recording", id.String()), slog.String("from", info.Source.String()),
		slog.String("to", source.String()))
	if advice, changed := sess.rewatch(source); changed {
		s.adviceChanged(id, advice)
	}
	return nil
}

// resyncSource makes the session watch the source the recorder captures, after a switch whose caller gave up: the
// recorder may have switched anyway.
func (s *Service) resyncSource(ctx context.Context, sess *session) {
	d, ok := s.recorder.Diagnostics(context.WithoutCancel(ctx), sess.id)
	if !ok {
		return
	}
	captured := values.WholeComputer()
	switch {
	case !d.App.IsZero():
		src, err := values.SourceOf(d.App)
		if err != nil {
			return
		}
		captured = src
	case d.TargetMode == values.CaptureInclude:
		return // an app without its name: unknown
	}
	if captured == sess.source() {
		return
	}

	s.log.Info("the recording source follows the recorder after a cancelled switch", slog.String("recording", sess.id.String()),
		slog.String("source", captured.String()))
	if advice, changed := sess.rewatch(captured); changed {
		s.adviceChanged(sess.id, advice)
	}
}

// DismissAdvice hides the current advice of the running recording id («Оставить») until it changes (TASK-082). A
// recording that this process does not capture yields ErrNotRunning (shared.ErrNotFound when there is none).
func (s *Service) DismissAdvice(ctx context.Context, id shared.RecordingID) error {
	sess, err := s.running(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDismiss, id, err)
	}
	if advice, changed := sess.updateWatch(values.SourceWatch.Dismiss); changed {
		s.adviceChanged(id, advice)
	}
	return nil
}

// running returns the session of id; ErrNotRunning when this process does not capture it.
func (s *Service) running(ctx context.Context, id shared.RecordingID) (*session, error) {
	s.mu.Lock()
	sess := s.active
	s.mu.Unlock()
	if sess != nil && sess.id == id {
		return sess, nil
	}
	if _, err := s.recordings.Get(ctx, id); err != nil {
		return nil, err
	}
	return nil, ErrNotRunning
}
