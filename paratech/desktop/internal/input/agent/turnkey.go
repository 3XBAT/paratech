package agent

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// opTriage names the log line of a triage whose reader call did not finish (the work stays, its call due).
const opTriage = "agent: triage reader call not finished"

// Turnkey is the turnkey work (cases/turnkey) as triage_assignment, write_work and their Undo use it. It has no method
// that confirms anything for the student: answers, edits, «Подтвердить», «Утвердить план» and regenerations come only
// from input/wails; the agent writes only a work whose requirements the student confirmed, and its job stops at the
// outline until the student approves it.
type Turnkey interface {
	Triage(ctx context.Context, src work.Source) (turnkey.Started, error)
	Work(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	Remove(ctx context.Context, id shared.WorkID, expected int64) error
	StartWriting(ctx context.Context, id shared.WorkID) (turnkey.WritingStarted, error)
	ActiveWriting(ctx context.Context, id shared.WorkID) (shared.JobID, bool, error)
	WritingGeneration(ctx context.Context, job shared.JobID) (int, error)
}

// triage starts the work of an assignment or an uploaded file and asks the reader (TASK-129). The planner gets the
// work's ID, where it stands, the feasibility enum and the number of waiting questions, never a text (ADR-0022);
// the panel gets a work card that the turnkey binding fills. A work this call created is undone by removing it while
// the student has not touched it; an existing work is not this action's (no Undo). A reader call that did not finish
// leaves the work with its call due: outcome «pending», not a failure of the action.
func (a *Actions) triage(call values.Call) (prepared, error) {
	if a.turnkey == nil {
		return prepared{}, errUnavailable
	}
	var src work.Source
	if r, ok := call.Args().ID("assignment"); ok {
		src.Assignment = shared.AssignmentID(r.ID())
	} else if r, ok := call.Args().ID("upload"); ok {
		src.Material = shared.MaterialID(r.ID())
	}
	return prepared{run: func(ctx context.Context) (effect, error) {
		st, err := a.turnkey.Triage(ctx, src)
		if st.View.ID == "" {
			if err == nil {
				err = fmt.Errorf("%w: no work", turnkey.ErrUnavailable)
			}
			return effect{}, err
		}
		if err != nil {
			a.log.InfoContext(ctx, opTriage, "work", string(st.View.ID), "err", err)
		}
		row, err := triageRowOf(st.View, st.Created)
		if err != nil {
			return effect{}, err
		}
		return effect{
			comp: action.RemoveWork{Work: row.Work}, version: st.View.Version, triage: &row, failed: !st.Created,
			cards: []values.Card{values.WorkCard{Work: row.Work, Outcome: row.Outcome}},
		}, nil
	}}, nil
}

// triageRowOf maps a work to what the planner sees of it.
func triageRowOf(v turnkey.View, changed bool) (values.TriageRow, error) {
	ref, err := values.NewRef(values.KindWork, string(v.ID))
	if err != nil {
		return values.TriageRow{}, err
	}
	row := values.TriageRow{Work: ref, Outcome: outcomeOf(v.Status), Round: v.Round, Changed: changed}
	if v.HasAnswer {
		row.Feasibility = string(v.Feasibility)
	}
	if row.Outcome == values.TriageQuestions {
		row.Questions = len(v.Questions)
	}
	return row, nil
}

func outcomeOf(s work.Status) values.TriageOutcome {
	switch s {
	case work.StatusQuestions:
		return values.TriageQuestions
	case work.StatusReview:
		return values.TriageReview
	case work.StatusHelper:
		return values.TriageHelper
	case work.StatusTriage:
		return values.TriagePending
	case work.StatusFailed:
		return values.TriageFailed
	}
	return values.TriageConfirmed
}

// triageReplay answers a triage call run again (the step was replayed) from the work as it is now; nothing is asked.
func (a *Actions) triageReplay(ctx context.Context, w values.Ref) (values.TriageRow, error) {
	if a.turnkey == nil {
		return values.TriageRow{}, errUnavailable
	}
	v, err := a.turnkey.Work(ctx, shared.WorkID(w.ID()))
	if err != nil {
		return values.TriageRow{}, err
	}
	return triageRowOf(v, true)
}

// checkWork reads the version of the work a RemoveWork compensation removes; a gone one is shared.ErrNotFound.
func (a *Actions) checkWork(ctx context.Context, c action.RemoveWork) (action.Target, error) {
	if a.turnkey == nil {
		return action.Target{}, errUnavailable
	}
	v, err := a.turnkey.Work(ctx, shared.WorkID(c.Work.ID()))
	if err != nil {
		return action.Target{}, gone(err)
	}
	return action.Target{Version: v.Version}, nil
}

// removeWork applies a RemoveWork compensation at expectedVersion: a work the student answered, edited or confirmed
// since (another version) is action.ErrTargetChanged; a gone one is already undone.
func (a *Actions) removeWork(ctx context.Context, c action.RemoveWork, expectedVersion int64) error {
	if a.turnkey == nil {
		return errUnavailable
	}
	err := a.turnkey.Remove(ctx, shared.WorkID(c.Work.ID()), expectedVersion)
	if errors.Is(err, turnkey.ErrChanged) {
		return fmt.Errorf("%w: %w", action.ErrTargetChanged, err)
	}
	return err
}

// --- write_work (TASK-131) ------------------------------------------------------------------------------

// writeWork starts the writing of a confirmed work: a paid job that writes the outline and ends there; once the
// student approved the outline (input/wails), the next job writes one section per step and files the .docx into the
// subject's «ДЗ». It always runs on an approved plan (actionpolicy). The planner gets the
// job only (ADR-0022); the progress card follows it («Раздел N из M»), the result card is the work's file
// (turnkey.View.Writing.Result). A work done already, or one whose writing job runs, is no action of this call.
// Undo (StopWork): the job is cancelled while it runs; once the paper is filed it goes back to «Входящие».
func (a *Actions) writeWork(ctx context.Context, call values.Call) (prepared, error) {
	if a.turnkey == nil {
		return prepared{}, errUnavailable
	}
	ref, _ := call.Args().ID("work")
	id := shared.WorkID(ref.ID())
	v, err := a.turnkey.Work(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if v.Status == work.StatusDone {
		return prepared{noop: &values.WriteRow{}}, nil
	}
	if running, ok, err := a.turnkey.ActiveWriting(ctx, id); err != nil {
		return prepared{}, err
	} else if ok {
		j, err := values.NewRef(values.KindJob, string(running))
		if err != nil {
			return prepared{}, err
		}
		return prepared{noop: &values.WriteRow{Job: j}}, nil
	}
	return prepared{run: func(ctx context.Context) (effect, error) {
		st, err := a.turnkey.StartWriting(ctx, id)
		if err != nil {
			return effect{}, err
		}
		e, err := jobEffect(st.Job, st.Created)
		if err != nil {
			return effect{}, err
		}
		e.comp = action.StopWork{Work: ref, Job: e.row.Job}
		return e, nil
	}}, nil
}

// checkWriting reads the target of a StopWork compensation: the job while it has not completed; then the paper of
// its generation, compensated once back in «Входящие», changed when the student moved it elsewhere or the work was
// written again since (another generation: that paper is not this action's).
func (a *Actions) checkWriting(ctx context.Context, c action.StopWork) (action.Target, error) {
	st, err := a.jobState(ctx, shared.JobID(c.Job.ID()))
	if err != nil {
		return action.Target{}, err
	}
	if st != job.StatusCompleted {
		return action.Target{Version: jobVersion, Compensated: st == job.StatusCancelled || st == job.StatusFailed}, nil
	}
	if a.turnkey == nil {
		return action.Target{}, errUnavailable
	}
	v, err := a.turnkey.Work(ctx, shared.WorkID(c.Work.ID()))
	if err != nil {
		return action.Target{}, gone(err)
	}
	if v.Writing == nil || v.Writing.Material == "" {
		return action.Target{Version: jobVersion + 1}, nil
	}
	gen, err := a.turnkey.WritingGeneration(ctx, shared.JobID(c.Job.ID()))
	if err != nil {
		return action.Target{}, gone(err)
	}
	if gen != v.Writing.Generation {
		return action.Target{Version: jobVersion + 1}, nil
	}
	m, err := a.library.Material(ctx, v.Writing.Material)
	if err != nil {
		return action.Target{}, gone(err)
	}
	switch {
	case m.InInbox():
		return action.Target{Version: jobVersion, Compensated: true}, nil
	case m.Folder() != subject.SystemFolderID(m.Subject(), subject.SystemHomework):
		return action.Target{Version: jobVersion + 1}, nil
	}
	return action.Target{Version: jobVersion}, nil
}

// stopWork applies a StopWork compensation: a running job is cancelled; a filed paper still in «ДЗ» goes back to
// «Входящие» in one transaction with the check.
func (a *Actions) stopWork(ctx context.Context, c action.StopWork, expectedVersion int64) (int64, error) {
	t, err := a.checkWriting(ctx, c)
	switch {
	case err != nil:
		return 0, err
	case t.Compensated:
		return 0, nil
	case t.Version != expectedVersion:
		return 0, action.ErrTargetChanged
	}
	if st, err := a.jobState(ctx, shared.JobID(c.Job.ID())); err != nil {
		return 0, err
	} else if st != job.StatusCompleted {
		err := a.cancelJob(ctx, shared.JobID(c.Job.ID()))
		if !errors.Is(err, action.ErrResultReady) {
			return jobVersion, err
		}
		// It completed meanwhile: its paper goes back to «Входящие» instead.
	}
	err = a.tx.Do(ctx, func(ctx context.Context) error {
		t, err := a.checkWriting(ctx, c)
		switch {
		case err != nil:
			return err
		case t.Compensated:
			return nil
		case t.Version != expectedVersion:
			return action.ErrTargetChanged
		}
		v, err := a.turnkey.Work(ctx, shared.WorkID(c.Work.ID()))
		if err != nil {
			return err
		}
		_, err = a.library.MoveToInbox(ctx, v.Writing.Material)
		return changedIfConflict(err)
	})
	if err != nil {
		return 0, err
	}
	return jobVersion, nil
}
