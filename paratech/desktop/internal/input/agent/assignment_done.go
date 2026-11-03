package agent

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// AssignmentMarks is «Выполнено» / «Вернуть в работу» (cases/homeworkflow, ADR-0033 §9) as mark_assignments_done and
// its Undo use it. Google Calendar is not touched (О-13 is open).
type AssignmentMarks interface {
	MarkDone(ctx context.Context, ids []shared.AssignmentID, done bool) (homeworkflow.DoneMarks, error)
	RestoreDone(ctx context.Context, prior []homeworkflow.DonePrior) (homeworkflow.DoneMarks, error)
}

// markDone is mark_assignments_done: ONE call of MarkDone for the whole list, one journal entry and one compensation
// (RestoreDone) of the assignments the use case changed, built from its result, never from the model. A list that
// changes nothing (all already so, or unknown) is answered without a journal entry and without «Отменить». The intent
// records the state and version of each one that would change, for Recover.
func (a *Actions) markDone(ctx context.Context, call values.Call) (prepared, error) {
	if a.marks == nil {
		return prepared{}, errUnavailable
	}
	done, _ := call.Args().Bool("done")
	var (
		ids  []shared.AssignmentID
		rows []values.MarkRow
		next []action.DoneMark
		seen = values.RefSet{}
	)
	for _, r := range call.Args().IDs("assignments") {
		if seen.Has(r) {
			continue
		}
		seen.Add(r)
		id := shared.AssignmentID(r.ID())
		ids = append(ids, id)
		v, err := a.assignments.Assignment(ctx, id)
		switch {
		case err == nil && v.Done == done:
			rows = append(rows, values.MarkRow{ID: r})
		case err == nil:
			next = append(next, action.DoneMark{Assignment: r, WasDone: v.Done, Version: v.Version})
		case errors.Is(err, shared.ErrNotFound):
			rows = append(rows, values.MarkRow{ID: r, Refusal: markRefusal(homeworkflow.DoneOutcome{Err: err, Code: homeworkflow.DoneNotFound})})
		default:
			return prepared{}, err
		}
	}
	if len(next) == 0 {
		return prepared{noopMarks: rows}, nil
	}
	wanted, err := action.NewRestoreDone(next)
	if err != nil {
		return prepared{}, err
	}
	return prepared{
		intent: action.Intent{Next: wanted},
		run: func(ctx context.Context) (effect, error) {
			res, err := a.marks.MarkDone(ctx, ids, done)
			if err != nil {
				return effect{}, err
			}
			rows := make([]values.MarkRow, 0, len(res.Outcomes))
			changed := 0
			for _, o := range res.Outcomes {
				r, err := values.NewRef(values.KindAssignment, string(o.ID))
				if err != nil {
					return effect{}, err
				}
				if o.Code == homeworkflow.DoneChanged {
					changed++
				}
				rows = append(rows, values.MarkRow{ID: r, Changed: o.Code == homeworkflow.DoneChanged, Refusal: markRefusal(o)})
			}
			if len(res.Prior) == 0 {
				return effect{marks: rows, failed: true}, nil // changed nothing after all: no «Отменить»
			}
			comp, err := restoreDoneOf(res.Prior)
			if err != nil {
				return effect{}, err
			}
			return effect{comp: comp, version: comp.ExpectedVersion(), marks: rows, line: markedLine(done, changed)}, nil
		},
	}, nil
}

// markRefusal is the refusal of one row: a deleted assignment is unknown as everywhere (ADR-0029).
func markRefusal(o homeworkflow.DoneOutcome) values.Refusal {
	switch o.Code {
	case homeworkflow.DoneChanged, homeworkflow.DoneUnchanged:
		return ""
	case homeworkflow.DoneDeleted:
		return values.RefuseUnknownID
	case homeworkflow.DoneNotFound:
		if errors.Is(o.Err, assignment.ErrDeleted) {
			return values.RefuseUnknownID
		}
		return values.RefuseNotFound
	}
	return values.RefuseFailed
}

// restoreDoneOf is the compensation of the prior states MarkDone or RestoreDone returned.
func restoreDoneOf(prior []homeworkflow.DonePrior) (action.RestoreDone, error) {
	marks := make([]action.DoneMark, 0, len(prior))
	for _, p := range prior {
		r, err := values.NewRef(values.KindAssignment, string(p.ID))
		if err != nil {
			return action.RestoreDone{}, err
		}
		marks = append(marks, action.DoneMark{Assignment: r, WasDone: p.Done, Version: p.Version})
	}
	return action.NewRestoreDone(marks)
}

// checkMarks reads the marks of a RestoreDone: compensated when each is back in its prior state; at the expected
// version only when each is still at the version the call saved, so a change of any of them (also of one below the
// highest version) refuses Undo. A deleted or missing one is a changed target.
func (a *Actions) checkMarks(ctx context.Context, c action.RestoreDone) (action.Target, error) {
	same, compensated := true, true
	for _, m := range c.Marks() {
		v, err := a.assignments.Assignment(ctx, shared.AssignmentID(m.Assignment.ID()))
		if errors.Is(err, shared.ErrNotFound) {
			return deletedTarget, nil
		}
		if err != nil {
			return action.Target{}, err
		}
		same = same && v.Version == m.Version
		compensated = compensated && v.Done == m.WasDone
	}
	t := action.Target{Compensated: compensated}
	if same {
		t.Version = c.ExpectedVersion()
	}
	return t, nil
}

// restoreMarks applies a RestoreDone inside Apply's transaction: every mark must still be at the version the call saved
// and every one must change back, else nothing stays (errMovedOn rolls the transaction back): Undo is all or nothing.
func (a *Actions) restoreMarks(ctx context.Context, c action.RestoreDone) error {
	if a.marks == nil {
		return errUnavailable
	}
	prior := make([]homeworkflow.DonePrior, 0, len(c.Marks()))
	for _, m := range c.Marks() {
		id := shared.AssignmentID(m.Assignment.ID())
		v, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return err
		}
		if v.Version != m.Version {
			return errMovedOn
		}
		prior = append(prior, homeworkflow.DonePrior{ID: id, Done: m.WasDone, Version: m.Version})
	}
	res, err := a.marks.RestoreDone(ctx, prior)
	if err != nil {
		return err
	}
	for _, o := range res.Outcomes {
		if o.Code != homeworkflow.DoneChanged {
			return fmt.Errorf("%w: %s %s", errMovedOn, o.ID, o.Code)
		}
	}
	return nil
}

// observeMarks settles a PENDING mark_assignments_done after a crash: the assignments of the intent that moved from
// their state to the other one with exactly one save are the effect (MarkDone saves each once); none moved is no
// effect (FAILED); any other change leaves the outcome unknown.
func (a *Actions) observeMarks(ctx context.Context, in action.Intent) (action.Observation, error) {
	wanted, ok := in.Next.(action.RestoreDone)
	if !ok {
		return action.Observation{}, nil
	}
	var changed []action.DoneMark
	for _, m := range wanted.Marks() {
		v, err := a.assignments.Assignment(ctx, shared.AssignmentID(m.Assignment.ID()))
		switch {
		case errors.Is(err, shared.ErrNotFound):
			return action.Observation{}, nil
		case err != nil:
			return action.Observation{}, err
		case v.Done == m.WasDone && v.Version == m.Version:
		case v.Done != m.WasDone && v.Version == m.Version+1:
			changed = append(changed, action.DoneMark{Assignment: m.Assignment, WasDone: m.WasDone, Version: v.Version})
		default:
			return action.Observation{}, nil
		}
	}
	if len(changed) == 0 {
		return action.Observation{Known: true}, nil
	}
	comp, err := action.NewRestoreDone(changed)
	if err != nil {
		return action.Observation{}, err
	}
	return action.Observation{Known: true, Version: comp.ExpectedVersion(), Effect: true, Compensation: comp}, nil
}

// marksResult is the result of mark_assignments_done: the outcome of each assignment by its ID.
func (t tools) marksResult(c agentturn.Call, rows []values.MarkRow, cards []values.Card) ran {
	res, seen, err := values.MarksResult(c.Call.ID(), rows)
	if err != nil {
		t.log.Warn(opAction, "tool", c.Call.Tool().String(), "err", err)
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true, cards: cards}
}
