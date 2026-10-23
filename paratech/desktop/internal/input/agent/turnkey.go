package agent

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// opTriage names the log line of a triage whose reader call did not finish (the work stays, its call due).
const opTriage = "agent: triage reader call not finished"

// Turnkey is the turnkey work (cases/turnkey) as triage_assignment and its Undo use it. It has no method that writes
// the work or confirms anything for the student: answers, edits and «Подтвердить» come only from input/wails.
type Turnkey interface {
	Triage(ctx context.Context, src work.Source) (turnkey.Started, error)
	Work(ctx context.Context, id shared.WorkID) (turnkey.View, error)
	Remove(ctx context.Context, id shared.WorkID, expected int64) error
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
