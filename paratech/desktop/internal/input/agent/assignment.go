package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// opActionLine is the log name of an action line shown without its assignment.
const opActionLine = "agent: action line assignment not read"

// addedIDPrefix marks the assignments the agent adds; extracted and hand-made ones use other IDs.
const addedIDPrefix = "ag-"

// createAssignment adds an assignment or a reminder the student named (create_assignment, ADR-0029). Its ID is derived
// here from the call's key ag:<thread>:<turn>:<call_id>, never taken from the model (the catalog has no id argument),
// so a replay of the call adds nothing new and a call of another request never meets it. The intent names that ID
// before the use case runs: after a crash its presence tells the outcome. The title and note are the student's own
// words, display text only: they are stored as typed and never go back to the planner, the journal or the log.
func (a *Actions) createAssignment(at origin, call values.Call) (prepared, error) {
	if a.added == nil || at.thread == "" || at.turn < 1 {
		return prepared{}, errUnavailable
	}
	ref, err := values.NewRef(values.KindAssignment, string(addedAssignmentID(at, call.ID())))
	if err != nil {
		return prepared{}, err
	}
	args := call.Args()
	title, _ := args.Label("title")
	note, _ := args.Query("note")
	sub, _ := args.ID("subject")
	lesson, _ := args.ID("lesson")
	due, _ := args.Date("due")
	tod, _ := args.Time("time")
	in := homeworkflow.NewAssignment{
		ID: shared.AssignmentID(ref.ID()), Title: string(title), Note: string(note), Subject: shared.SubjectID(sub.ID()),
		Lesson: shared.LessonID(lesson.ID()), Date: due.String(), Time: tod.String(),
	}
	return prepared{
		intent: action.Intent{Target: ref},
		run: func(ctx context.Context) (effect, error) {
			v, err := a.added.CreateAssignment(ctx, in)
			if err != nil {
				return effect{}, err
			}
			return effect{
				comp: action.RemoveAssignment{Assignment: ref}, version: v.Version,
				row: values.WriteRow{ID: ref, Changed: true}, created: ref,
			}, nil
		},
	}, nil
}

// addedAssignmentID is the ID of the assignment a create_assignment call adds: a digest of the call's key, a valid
// AssignmentID of fixed length whatever the thread and call IDs are.
func addedAssignmentID(at origin, callID string) shared.AssignmentID {
	sum := sha256.Sum256(fmt.Appendf(nil, "ag:%s:%d:%s", at.thread, at.turn, callID))
	return shared.AssignmentID(addedIDPrefix + hex.EncodeToString(sum[:16]))
}

// checkAdded reads the version of the assignment a RemoveAssignment compensation removes; a gone one is undone.
func (a *Actions) checkAdded(ctx context.Context, c action.RemoveAssignment) (action.Target, error) {
	v, err := a.assignments.Assignment(ctx, shared.AssignmentID(c.Assignment.ID()))
	if errors.Is(err, assignment.ErrDeleted) {
		return deletedTarget, nil // the student deleted it: changed, not undone
	}
	if errors.Is(err, shared.ErrNotFound) {
		return action.Target{Compensated: true}, nil
	}
	if err != nil {
		return action.Target{}, err
	}
	return action.Target{Version: v.Version}, nil
}

// removeAdded applies a RemoveAssignment compensation at expectedVersion. The use case removes it in one transaction
// with its checks; a student edit since (another version), a draft, a delivery, a work or a live calendar proposal on
// it is action.ErrTargetChanged, so Undo is refused («Изменено после»). A gone one is already undone.
func (a *Actions) removeAdded(ctx context.Context, c action.RemoveAssignment, expectedVersion int64) error {
	if a.added == nil {
		return errUnavailable
	}
	err := a.added.RemoveCreatedAssignment(ctx, shared.AssignmentID(c.Assignment.ID()), expectedVersion)
	switch {
	case err == nil, errors.Is(err, shared.ErrNotFound):
		return nil
	case errors.Is(err, assignment.ErrInUse), errors.Is(err, shared.ErrVersionConflict),
		errors.Is(err, homeworkflow.ErrNotByStudent):
		return fmt.Errorf("%w: %w", action.ErrTargetChanged, err)
	}
	return err
}

// createdVersion is the version CreateAssignment stores a new assignment at (the first save, ADR-0004).
const createdVersion int64 = 1

// observeAdded settles a PENDING create_assignment after a crash by the ID its intent named: there -> DONE with its
// Undo, absent -> FAILED (the version before was 0). The version reported is the one the create wrote, not the current
// one: an edit by the student between the crash and Recover then makes Undo refuse («Изменено после») instead of
// removing the edit.
func (a *Actions) observeAdded(ctx context.Context, in action.Intent) (action.Observation, error) {
	if in.Target.Kind() != values.KindAssignment {
		return action.Observation{}, nil
	}
	if _, err := a.assignments.Assignment(ctx, shared.AssignmentID(in.Target.ID())); err != nil {
		return absent(err)
	}
	return action.Observation{
		Known: true, Version: createdVersion, Effect: true, Compensation: action.RemoveAssignment{Assignment: in.Target},
	}, nil
}

// namesDeleted reports whether an ID argument of call (plan steps aside) names an assignment the student deleted:
// every tool then answers unknown_id before its use case runs, as if the assignment never was (ADR-0029). Any other
// read error is left to the tool.
func (t tools) namesDeleted(ctx context.Context, call values.Call) bool {
	if t.actions == nil {
		return false
	}
	args := call.Args()
	for _, name := range args.Names() {
		refs := args.IDs(name)
		if r, ok := args.ID(name); ok {
			refs = append(refs, r)
		}
		for _, r := range refs {
			if r.Kind() != values.KindAssignment {
				continue
			}
			if _, err := t.actions.assignments.Assignment(ctx, shared.AssignmentID(r.ID())); errors.Is(err, assignment.ErrDeleted) {
				return true
			}
		}
	}
	return false
}

// actionLine adds to the action line of create_assignment the assignment as the local data has it: its title, library
// subject and deadline (ADR-0029, as the lines of a reply). Nothing read here goes to the planner or the log; when the
// assignment cannot be read (or is gone) the line is shown without it.
func (t tools) actionLine(ctx context.Context, line values.ActionLine) values.ActionLine {
	ref := line.Assignment.Assignment
	line.Assignment = values.AssignmentLine{}
	if t.actions == nil || ref.Kind() != values.KindAssignment {
		return line
	}
	v, err := t.actions.assignments.Assignment(ctx, shared.AssignmentID(ref.ID()))
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			t.log.WarnContext(ctx, opActionLine, "err", err)
		}
		return line
	}
	out := values.AssignmentLine{Assignment: ref, Title: v.Title}
	if !v.Due.IsZero() {
		loc := t.clock.Now().Location()
		out.Due = dateOf(v.Due, loc)
		local := v.Due.In(loc)
		if !out.Due.IsZero() {
			out.DueTime, _ = values.NewTimeOfDay(local.Hour(), local.Minute())
		}
	}
	if v.Subject != "" {
		subjects, err := t.library.ResolveSubjects(ctx, []string{v.Subject})
		if err != nil {
			t.log.WarnContext(ctx, opActionLine, "what", "subjects", "err", err)
		} else if m, ok := subjects[v.Subject]; ok {
			out.Subject, _ = refOfID(values.KindSubject, string(m.ID))
		}
	}
	line.Assignment = out
	if line.Validate() != nil {
		line.Assignment = values.AssignmentLine{}
	}
	return line
}
