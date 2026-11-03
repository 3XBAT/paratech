package agent

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/recording/prompt"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// recording{start|stop} (catalog v10, ADR-0033 §2, §10, О-7 а): «Начать запись» and «Остановить» of the recordings
// page, at once and without «Да». The model names no source: start records what the page's picker preselects. Undo
// of a start stops it and keeps the audio; a stop has no Undo and its line offers «Начать запись».

// RecordingStarts is «Начать запись» of the recordings page (cases/recordingprompt): the source the picker preselects
// (Sources) and the start without the popup (StartManual).
type RecordingStarts interface {
	Sources(ctx context.Context) (recordingprompt.Sources, error)
	StartManual(ctx context.Context, source recvalues.Source) (recordingprompt.Manual, error)
}

// RecordingSession is the running recording (cases/recordingsession): the one the page shows as running and
// «Остановить» of it, which waits until the recording is finished and keeps its audio.
type RecordingSession interface {
	Active() (recordingsession.Info, bool)
	Stop(ctx context.Context, id shared.RecordingID) (recordingsession.Info, error)
}

// recordingVersion is the version of a running recording as a target: it is never edited, so a running one is the
// one the action started; whether it still runs is Target.Compensated.
const recordingVersion = 1

var (
	// errRecordingNotNow: another recording starts or runs, or the interrupted ones are being recovered.
	errRecordingNotNow = errors.New("agent: a recording cannot start now")
	// errSourceGone: the app the picker preselected closed before the start.
	errSourceGone = errors.New("agent: the recording source is not running")
	// errCaptureUnavailable: this build cannot capture audio.
	errCaptureUnavailable = errors.New("agent: audio capture is unavailable")
)

// recording prepares one action of recording.
func (a *Actions) recording(call values.Call) (prepared, error) {
	if a.controls.Starts == nil || a.controls.Session == nil {
		return prepared{}, errUnavailable
	}
	act, _ := call.Args().Enum(values.ArgAction)
	switch act {
	case values.RecordingStart:
		return a.startRecording(), nil
	case values.RecordingStop:
		return a.stopRecording(), nil
	}
	return prepared{}, errUnavailable
}

// startRecording is «Начать запись» with the source the page preselects. The look at the running recording is made
// after the entry is journaled, and a start that finds one changes nothing but is still journaled (DONE without a
// compensation): a replay of its call ID then answers from the entry and never starts a capture the student did not
// ask for since. The Undo of a real start stops exactly the recording it made.
func (a *Actions) startRecording() prepared {
	return prepared{run: func(ctx context.Context) (effect, error) {
		if info, ok := a.controls.Session.Active(); ok {
			r, err := recordingRefOf(info.ID)
			if err != nil {
				return effect{}, err
			}
			return effect{noop: true, version: recordingVersion, out: recordingOut(values.RecordingRunning, r)}, nil
		}
		m, err := a.controls.Starts.StartManual(ctx, a.preselected(ctx))
		if err != nil {
			return effect{}, startError(m.Problem, err)
		}
		r, err := recordingRefOf(m.Recording)
		if err != nil {
			return effect{}, err
		}
		return effect{
			comp: action.StopRecording{Recording: r}, version: recordingVersion, out: recordingOut(values.RecordingStarted, r),
			line: values.LineResult{Kind: values.LineRecordingStarted},
		}, nil
	}}
}

// preselected is the source the picker of «Начать запись» takes on a fresh opening (frontend sourcePicker nextSelection
// from initialSelection, which is the core's preselection values.Preselect): the remembered app while it is open, the
// one app that sounds instead, else the whole computer. When the apps cannot be listed the page offers the whole
// computer only, and so does this.
func (a *Actions) preselected(ctx context.Context) recvalues.Source {
	src, err := a.controls.Starts.Sources(ctx)
	if err != nil {
		a.log.InfoContext(ctx, opAction, "tool", values.ToolRecording.String(), "err", err)
		return recvalues.WholeComputer()
	}
	return src.Selected
}

// startError maps a refused start to the error of its problem.
func startError(p prompt.Problem, err error) error {
	switch p {
	case prompt.ProblemInProgress, prompt.ProblemRecovering:
		return fmt.Errorf("%w: %w", errRecordingNotNow, err)
	case prompt.ProblemSourceGone:
		return fmt.Errorf("%w: %w", errSourceGone, err)
	case prompt.ProblemUnavailable:
		return fmt.Errorf("%w: %w", errCaptureUnavailable, err)
	}
	return err
}

// stopRecording is «Остановить» of the running recording: done without a compensation (О-7 а), the line offers
// «Начать запись». Without a running recording it changes nothing (not_recording) and its entry is FAILED, so a
// replay of its call ID answers not_recording again and never stops a recording the student started since.
func (a *Actions) stopRecording() prepared {
	notRecording := effect{failed: true, out: recordingOut(values.RecordingNotActive, values.Ref{})}
	return prepared{run: func(ctx context.Context) (effect, error) {
		info, ok := a.controls.Session.Active()
		if !ok {
			return notRecording, nil
		}
		r, err := recordingRefOf(info.ID)
		if err != nil {
			return effect{}, err
		}
		// Stop of a recording that already ended returns it without an error (and it counts as stopped here);
		// ErrNotRunning is an unfinished recording no session of this process captures: nothing was stopped.
		_, err = a.controls.Session.Stop(ctx, info.ID)
		switch {
		case errors.Is(err, recordingsession.ErrNotRunning):
			return notRecording, nil
		case err != nil:
			return effect{}, err
		}
		return effect{
			version: recordingVersion, offer: values.OfferStartRecording, out: recordingOut(values.RecordingStopped, r),
			line: values.LineResult{Kind: values.LineRecordingStopped},
		}, nil
	}}
}

// checkRecording reads the target of a StopRecording: compensated once the recording the start made no longer runs
// (the student stopped it, or another one runs in its place).
func (a *Actions) checkRecording(c action.StopRecording) (action.Target, error) {
	if a.controls.Session == nil {
		return action.Target{}, errUnavailable
	}
	info, ok := a.controls.Session.Active()
	if !ok || string(info.ID) != c.Recording.ID() {
		return action.Target{Compensated: true}, nil
	}
	return action.Target{Version: recordingVersion}, nil
}

// stopStarted applies a StopRecording: «Остановить» of the recording the start made while it still runs; the audio is
// kept. A recording stopped meanwhile needs nothing.
func (a *Actions) stopStarted(ctx context.Context, c action.StopRecording, expectedVersion int64) error {
	t, err := a.checkRecording(c)
	switch {
	case err != nil:
		return err
	case t.Compensated:
		return nil
	case t.Version != expectedVersion:
		return action.ErrTargetChanged
	}
	_, err = a.controls.Session.Stop(ctx, shared.RecordingID(c.Recording.ID()))
	if err == nil || errors.Is(err, recordingsession.ErrNotRunning) {
		return nil
	}
	if t, cerr := a.checkRecording(c); cerr == nil && t.Compensated {
		return nil // it stopped, with a problem the recordings page shows
	}
	return err
}

// recordingRefOf is the ref of a recording a use case gave.
func recordingRefOf(id shared.RecordingID) (values.Ref, error) {
	return values.NewRef(values.KindRecording, string(id))
}

// recordingOut is the result of recording in state st for recording r.
func recordingOut(st values.RecordingState, r values.Ref) resultOf {
	return func(t tools, c agentturn.Call, cards []values.Card) ran {
		res, seen, err := values.RecordingResult(c.Call.ID(), values.RecordingRow{State: st, Recording: r})
		if err != nil {
			t.log.Warn(opAction, "tool", c.Call.Tool().String(), "err", err)
			return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseFailed)}, write: true}
		}
		return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true, cards: cards}
	}
}

// recordingRefusal maps a refused start.
func recordingRefusal(err error) (values.Refusal, bool) {
	switch {
	case errors.Is(err, errRecordingNotNow):
		return values.RefuseNotNow, true
	case errors.Is(err, errSourceGone):
		return values.RefuseNotFound, true
	case errors.Is(err, errCaptureUnavailable):
		return values.RefuseUnavailable, true
	}
	return "", false
}
