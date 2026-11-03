package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// change_assignment (catalog v10, ADR-0033 §10): edit, delete and restore through the use cases of the assignment page.
// The model gives only the ID, the action and the new fields; the prior fields, the versions, the Google event fact
// and whether this chat deleted the assignment come from local data (hard rule 3). The title and note are the
// student's words: stored, never logged and never sent back to the planner.

// AssignmentChanges is the homework workflow (cases/homeworkflow) as change_assignment and its Undo use it: the same
// use cases as the assignment page (HomeworkService) and the read of a deleted assignment for the versions.
// TrashAssignment is homeworkflow.Service.DeleteAndStop, the soft delete of the page with «Вернуть» (app wires it
// under this name: the package has no method named after an irreversible action, TestAgentHasNoPathToIrreversibleActions).
// DeletionImpact tells whether that delete would stop a work or a job.
type AssignmentChanges interface {
	AssignmentState(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	EditAssignment(ctx context.Context, e homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error)
	CorrectLesson(ctx context.Context, c homeworkflow.LessonCorrection) (homeworkflow.AssignmentView, error)
	DeletionImpact(ctx context.Context, id shared.AssignmentID) (homeworkflow.DeleteImpact, error)
	TrashAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (homeworkflow.Deletion, error)
	RestoreAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (homeworkflow.AssignmentView, error)
}

// ChatActions lists the action journal of a chat (cases/agentjournal): change_assignment{restore} brings back only an
// assignment this chat deleted (ADR-0029 C-2).
type ChatActions interface {
	ListThread(ctx context.Context, thread shared.ThreadID, limit int) ([]agentjournal.Entry, error)
}

var (
	// errNotThisChat: restore names an assignment this chat did not delete (or one restored since).
	errNotThisChat = errors.New("agent: the assignment was not deleted by this chat")
	// errHasEvent: the assignment got a Google event after the call was admitted as U; only the «Да» card deletes it.
	errHasEvent = errors.New("agent: the assignment has a Google Calendar event")
	// errWorking: a delete would stop a work or a job of the assignment, and «Вернуть» restarts nothing; the student
	// deletes it on the page, where the card warns first.
	errWorking = errors.New("agent: a delete would stop the work on the assignment")
	// errTimeWithoutDue: edit names a time of day without a date (as create_assignment).
	errTimeWithoutDue = errors.New("agent: a time without a date")
)

// changeAssignment prepares one action of change_assignment.
func (a *Actions) changeAssignment(ctx context.Context, at origin, call values.Call) (prepared, error) {
	if a.changes == nil {
		return prepared{}, errUnavailable
	}
	act, _ := call.Args().Enum(values.ArgAction)
	switch act {
	case values.AssignmentEdit:
		return a.editAssignment(ctx, call)
	case values.AssignmentDelete:
		return a.deleteAssignment(ctx, call)
	case values.AssignmentRestore:
		return a.restoreAssignment(ctx, at, call)
	}
	return prepared{}, errUnavailable
}

// editAssignment is «Сохранить» of the assignment page with the fields the call names; the others stay as stored. A
// deadline the agent changes updates its Google event through the existing sync without asking (C-4, О-2 а). Undo
// writes back the fields read here (RevertAssignment, the intent's Prior).
func (a *Actions) editAssignment(ctx context.Context, call values.Call) (prepared, error) {
	args := call.Args()
	ref, _ := args.ID("assignment")
	id := shared.AssignmentID(ref.ID())
	v, err := a.assignments.Assignment(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	prior := fieldsOf(v)
	next := prior
	// Trimmed as the domain stores them (assignment.NewDetails), so Recover finds exactly these fields.
	if l, ok := args.Label("title"); ok {
		next.Title = strings.TrimSpace(string(l))
	}
	if q, ok := args.Query("note"); ok {
		next.Note = strings.TrimSpace(string(q))
	}
	var date, tod string
	day, hasDay := args.Date("due")
	t, hasTime := args.Time("time")
	switch {
	case hasTime && !hasDay:
		return prepared{}, errTimeWithoutDue
	case hasDay:
		date = day.String()
		if hasTime {
			tod = t.String()
		}
		due, err := assignment.StudentDue(date, tod, a.clock.Now().Location())
		if err != nil {
			return prepared{}, err
		}
		next.Due = due.UTC()
	}
	var sub shared.SubjectID
	if r, ok := args.ID("subject"); ok {
		s, err := a.subject(ctx, shared.SubjectID(r.ID()))
		if err != nil {
			return prepared{}, err
		}
		sub, next.SubjectName = s.ID(), s.Name().String()
	}
	if next == prior {
		return prepared{noop: &values.WriteRow{}}, nil
	}

	undo := action.RevertAssignment{Assignment: ref, Prior: prior}
	before := v.Version
	return prepared{
		intent: action.Intent{Target: ref, Before: before, Prior: undo, Next: action.RevertAssignment{Assignment: ref, Prior: next}},
		run: func(ctx context.Context) (effect, error) {
			after, err := a.writeAssignment(ctx, id, before, next, sub, date, tod)
			if err != nil {
				return effect{}, err
			}
			return effect{comp: undo, version: after.Version, row: values.WriteRow{Changed: true}}, nil
		},
	}, nil
}

// writeAssignment stores f on assignment id at version expected in one transaction: EditAssignment with the title and
// note (and the deadline when date is given, else none), then the exact deadline f.Due and the subject correction
// f.SubjectName when sub is empty. So an edit keeps what it does not name, and Undo restores exactly what was.
func (a *Actions) writeAssignment(ctx context.Context, id shared.AssignmentID, expected int64, f action.AssignmentFields,
	sub shared.SubjectID, date, tod string) (homeworkflow.AssignmentView, error) {
	var v homeworkflow.AssignmentView
	err := a.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		v, err = a.changes.EditAssignment(ctx, homeworkflow.AssignmentEdit{
			ID: id, ExpectedVersion: expected, Title: f.Title, Note: f.Note, Subject: sub, Date: date, Time: tod,
		})
		if err != nil {
			return movedOn(err)
		}
		if date == "" && !f.Due.IsZero() {
			if v, err = a.assignments.SetAssignmentDue(ctx, id, v.Version, f.Due); err != nil {
				return movedOn(err)
			}
		}
		if sub == "" && f.SubjectName != "" && v.CorrectedSubject != f.SubjectName {
			v, err = a.changes.CorrectLesson(ctx, homeworkflow.LessonCorrection{ID: id, Subject: f.SubjectName, Teacher: v.CorrectedTeacher})
		}
		return err
	})
	return v, err
}

// movedOn reads a version conflict of a use case as a target changed since the look before the action.
func movedOn(err error) error {
	if errors.Is(err, shared.ErrVersionConflict) {
		return fmt.Errorf("%w: %w", errMovedOn, err)
	}
	return err
}

// fieldsOf is what change_assignment{edit} may change of a stored assignment, the deadline in UTC as the journal
// stores it (action.RevertAssignment is compared with ==).
func fieldsOf(v homeworkflow.AssignmentView) action.AssignmentFields {
	f := action.AssignmentFields{Title: v.Title, Note: v.Description, SubjectName: v.CorrectedSubject}
	if !v.Due.IsZero() {
		f.Due = v.Due.UTC()
	}
	return f
}

// deleteAssignment is «Удалить» of the assignment page (DeleteAndStop) for one without a Google event: class U, Undo
// is «Вернуть» (RestoreAssignment at the version the delete left). One with an event is class C (the card); the fact
// is read again here, so a delete never runs as U for an assignment that has one (a read error refuses). Nor for one
// with a work or a job under way: DeleteAndStop would stop it and Undo would not restart it.
func (a *Actions) deleteAssignment(ctx context.Context, call values.Call) (prepared, error) {
	ref, _ := call.Args().ID("assignment")
	id := shared.AssignmentID(ref.ID())
	v, err := a.assignments.Assignment(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if err := a.mayTrash(ctx, id); err != nil {
		return prepared{}, err
	}
	undo := action.RestoreAssignment{Assignment: ref}
	before := v.Version
	return prepared{
		intent: action.Intent{Target: ref, Before: before, Prior: undo},
		run: func(ctx context.Context) (effect, error) {
			if err := a.mayTrash(ctx, id); err != nil {
				return effect{}, err
			}
			d, err := a.changes.TrashAssignment(ctx, id, before)
			if err != nil {
				return effect{}, movedOn(err)
			}
			return effect{comp: undo, version: d.Assignment.Version, row: values.WriteRow{Changed: true}}, nil
		},
	}, nil
}

// mayTrash refuses a delete without «Да» of an assignment with a Google event or with a work or a job under way, and
// of one where either cannot be told (fail closed).
func (a *Actions) mayTrash(ctx context.Context, id shared.AssignmentID) error {
	if err := a.noEvent(ctx, id); err != nil {
		return err
	}
	impact, err := a.changes.DeletionImpact(ctx, id)
	switch {
	case err != nil:
		return err
	case impact.Working:
		return errWorking
	}
	return nil
}

// noEvent refuses an assignment with a Google event, and one whose event cannot be told (fail closed).
func (a *Actions) noEvent(ctx context.Context, id shared.AssignmentID) error {
	if a.events == nil {
		return errUnavailable
	}
	has, err := a.events.HasDeadlineEvent(ctx, id)
	switch {
	case err != nil:
		return err
	case has:
		return errHasEvent
	}
	return nil
}

// restoreAssignment is «Вернуть» of an assignment this chat deleted (ADR-0029 C-2): the newest DONE change_assignment
// entry of the chat naming it is its delete, and it was not changed after that delete. Undo deletes it again.
func (a *Actions) restoreAssignment(ctx context.Context, at origin, call values.Call) (prepared, error) {
	if a.chat == nil || at.thread == "" {
		return prepared{}, errUnavailable
	}
	ref, _ := call.Args().ID("assignment")
	id := shared.AssignmentID(ref.ID())
	v, err := a.changes.AssignmentState(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if !v.Deleted {
		return prepared{}, errNotThisChat
	}
	if err := a.deletedByChat(ctx, at.thread, ref, v); err != nil {
		return prepared{}, err
	}
	undo := action.DeleteAssignment{Assignment: ref}
	before := v.Version
	return prepared{
		intent: action.Intent{Target: ref, Before: before, Prior: undo},
		run: func(ctx context.Context) (effect, error) {
			r, err := a.changes.RestoreAssignment(ctx, id, before)
			if err != nil {
				return effect{}, movedOn(err)
			}
			return effect{comp: undo, version: r.Version, row: values.WriteRow{ID: ref, Changed: true}}, nil
		},
	}, nil
}

// deletedByChat checks against the journal of the chat that it deleted the assignment v (ref): its newest DONE
// change_assignment entry naming it is a delete (Undo RestoreAssignment), not a restore, and the assignment has not
// changed since that delete was recorded (a restore and a new delete on the page would be later).
func (a *Actions) deletedByChat(ctx context.Context, th shared.ThreadID, ref values.Ref, v homeworkflow.AssignmentView) error {
	entries, err := a.chat.ListThread(ctx, th, agentjournal.MaxList)
	if err != nil {
		return err
	}
	for _, e := range entries { // newest first
		if e.Tool != values.ToolChangeAssignment || e.Status != action.StatusDone {
			continue
		}
		switch c := e.Compensation.(type) {
		case action.RestoreAssignment:
			if c.Assignment == ref {
				if v.UpdatedAt.After(e.UpdatedAt) {
					return errNotThisChat
				}
				return nil
			}
		case action.DeleteAssignment:
			if c.Assignment == ref {
				return errNotThisChat
			}
		}
	}
	return errNotThisChat
}

// assignmentRefusal maps the errors of change_assignment and mark_assignments_done the generic map does not know.
func assignmentRefusal(tool values.Tool, err error) (values.Refusal, bool) {
	if tool != values.ToolChangeAssignment && tool != values.ToolMarkAssignmentsDone {
		return "", false
	}
	switch {
	case errors.Is(err, errNotThisChat), errors.Is(err, assignment.ErrRestoreExpired),
		errors.Is(err, homeworkflow.ErrSubjectNotFound):
		return values.RefuseUnknownID, true
	case errors.Is(err, errHasEvent), errors.Is(err, errWorking), errors.Is(err, assignment.ErrInUse):
		return values.RefuseNotNow, true
	case errors.Is(err, errTimeWithoutDue), errors.Is(err, homeworkflow.ErrInvalidRequest):
		return values.RefuseInvalidArgs, true
	}
	return "", false
}

// --- Undo and Recover ----------------------------------------------------------------------------------------

// checkAssignment reads the target of a change_assignment compensation. An assignment deleted, restored or purged
// since the action is a changed target, never an undone one.
func (a *Actions) checkAssignment(ctx context.Context, c action.Compensation) (action.Target, error) {
	switch v := c.(type) {
	case action.RevertAssignment:
		av, err := a.assignments.Assignment(ctx, shared.AssignmentID(v.Assignment.ID()))
		if errors.Is(err, shared.ErrNotFound) {
			return deletedTarget, nil
		}
		if err != nil {
			return action.Target{}, err
		}
		return action.Target{Version: av.Version, Compensated: fieldsOf(av) == v.Prior}, nil
	case action.RestoreAssignment:
		if a.changes == nil {
			return action.Target{}, errUnavailable
		}
		av, err := a.changes.AssignmentState(ctx, shared.AssignmentID(v.Assignment.ID()))
		if errors.Is(err, shared.ErrNotFound) {
			return deletedTarget, nil
		}
		if err != nil {
			return action.Target{}, err
		}
		return action.Target{Version: av.Version}, nil
	case action.DeleteAssignment:
		av, err := a.assignments.Assignment(ctx, shared.AssignmentID(v.Assignment.ID()))
		if errors.Is(err, shared.ErrNotFound) {
			return deletedTarget, nil
		}
		if err != nil {
			return action.Target{}, err
		}
		return action.Target{Version: av.Version}, nil
	}
	return action.Target{}, fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
}

// compensateAssignment applies a change_assignment compensation to a target Apply found at its expected version.
func (a *Actions) compensateAssignment(ctx context.Context, c action.Compensation) error {
	if a.changes == nil {
		return errUnavailable
	}
	switch v := c.(type) {
	case action.RevertAssignment:
		id := shared.AssignmentID(v.Assignment.ID())
		av, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return err
		}
		_, err = a.writeAssignment(ctx, id, av.Version, v.Prior, "", "", "")
		return err
	case action.RestoreAssignment:
		id := shared.AssignmentID(v.Assignment.ID())
		av, err := a.changes.AssignmentState(ctx, id)
		if err != nil {
			return err
		}
		_, err = a.changes.RestoreAssignment(ctx, id, av.Version)
		if errors.Is(err, assignment.ErrNotDeleted) || errors.Is(err, assignment.ErrRestoreExpired) {
			return fmt.Errorf("%w: %w", errMovedOn, err)
		}
		return movedOn(err)
	case action.DeleteAssignment:
		// Deleting again is a delete: one that got a Google event since the restore needs «Да» (О-6 б).
		id := shared.AssignmentID(v.Assignment.ID())
		if err := a.noEvent(ctx, id); err != nil {
			return fmt.Errorf("%w: %w", errMovedOn, err)
		}
		av, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return err
		}
		_, err = a.changes.TrashAssignment(ctx, id, av.Version)
		if errors.Is(err, assignment.ErrInUse) || errors.Is(err, assignment.ErrDeleted) {
			return fmt.Errorf("%w: %w", errMovedOn, err)
		}
		return movedOn(err)
	}
	return fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
}

// observeAssignment settles a PENDING change_assignment entry after a crash by its intent: edit — the fields are the
// Next ones; delete — the assignment is deleted; restore — it is not.
func (a *Actions) observeAssignment(ctx context.Context, in action.Intent) (action.Observation, error) {
	id := shared.AssignmentID(in.Target.ID())
	switch in.Prior.(type) {
	case action.RevertAssignment:
		v, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return absent(err)
		}
		next, ok := in.Next.(action.RevertAssignment)
		return observed(v.Version, in, ok && fieldsOf(v) == next.Prior), nil
	case action.RestoreAssignment, action.DeleteAssignment:
		if a.changes == nil {
			return action.Observation{}, nil
		}
		v, err := a.changes.AssignmentState(ctx, id)
		if err != nil {
			return absent(err)
		}
		_, deletes := in.Prior.(action.RestoreAssignment)
		return observed(v.Version, in, v.Deleted == deletes), nil
	}
	return action.Observation{}, nil
}

// replayAssignment rebuilds the result of a change_assignment or mark_assignments_done call from its compensation.
func (t tools) replayAssignment(c agentturn.Call, comp action.Compensation) (ran, bool) {
	switch v := comp.(type) {
	case action.RevertAssignment, action.RestoreAssignment:
		return t.result(c, values.WriteRow{Changed: true}, values.Ref{}, nil), true
	case action.DeleteAssignment:
		return t.result(c, values.WriteRow{ID: v.Assignment, Changed: true}, values.Ref{}, nil), true
	case action.RestoreDone:
		rows := make([]values.MarkRow, 0, len(v.Marks()))
		for _, m := range v.Marks() {
			rows = append(rows, values.MarkRow{ID: m.Assignment, Changed: true})
		}
		return t.marksResult(c, rows, nil), true
	}
	return ran{}, false
}
