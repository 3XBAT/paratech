package agent

import (
	"context"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// memChanges is the homework workflow of the wave-10 assignment tools over the assignments fake: the page's edit,
// soft delete and restore, «Выполнено» and the Google event projection. Every save bumps the version.
type memChanges struct {
	a         *memAssignments
	events    map[shared.AssignmentID]bool
	eventErr  error
	inUse     map[shared.AssignmentID]bool
	working   map[shared.AssignmentID]bool // a work or a job under way (DeletionImpact)
	impactErr error
	marks     int // MarkDone calls
	restores  int // RestoreDone calls
	calendar  int // calls that would touch Google Calendar (none may)
}

func newMemChanges(a *memAssignments) *memChanges {
	if a.deleted == nil {
		a.deleted = map[shared.AssignmentID]bool{}
	}
	return &memChanges{
		a: a, events: map[shared.AssignmentID]bool{}, inUse: map[shared.AssignmentID]bool{}, working: map[shared.AssignmentID]bool{},
	}
}

func (m *memChanges) DeletionImpact(_ context.Context, id shared.AssignmentID) (homeworkflow.DeleteImpact, error) {
	if m.impactErr != nil {
		return homeworkflow.DeleteImpact{}, m.impactErr
	}
	return homeworkflow.DeleteImpact{Working: m.working[id]}, nil
}

func (m *memChanges) live(id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	return m.a.Assignment(context.Background(), id)
}

func (m *memChanges) save(v homeworkflow.AssignmentView) homeworkflow.AssignmentView {
	v.Version++
	v.UpdatedAt = now
	m.a.views[v.ID] = v
	return v
}

func (m *memChanges) AssignmentState(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	v, ok := m.a.views[id]
	if !ok {
		return homeworkflow.AssignmentView{}, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	v.Deleted = m.a.deleted[id]
	return v, nil
}

func (m *memChanges) EditAssignment(_ context.Context, e homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error) {
	v, err := m.live(e.ID)
	if err != nil {
		return v, err
	}
	if v.Version != e.ExpectedVersion {
		return homeworkflow.AssignmentView{}, shared.ErrVersionConflict
	}
	due, err := assignment.StudentDue(e.Date, e.Time, moscow)
	if err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	var name string
	switch e.Subject {
	case "":
	case "s1":
		name = "Физика"
	default:
		return homeworkflow.AssignmentView{}, homeworkflow.ErrSubjectNotFound
	}
	v.Title, v.Description, v.Due, v.CorrectedSubject, v.UserEdited = e.Title, e.Note, due, name, true
	return m.save(v), nil
}

func (m *memChanges) CorrectLesson(_ context.Context, c homeworkflow.LessonCorrection) (homeworkflow.AssignmentView, error) {
	v, err := m.live(c.ID)
	if err != nil {
		return v, err
	}
	if v.CorrectedSubject == c.Subject && v.CorrectedTeacher == c.Teacher {
		return v, nil
	}
	v.CorrectedSubject, v.CorrectedTeacher = c.Subject, c.Teacher
	return m.save(v), nil
}

func (m *memChanges) TrashAssignment(_ context.Context, id shared.AssignmentID, expected int64) (homeworkflow.Deletion, error) {
	v, err := m.live(id)
	switch {
	case err != nil:
		return homeworkflow.Deletion{}, err
	case v.Version != expected:
		return homeworkflow.Deletion{}, shared.ErrVersionConflict
	case m.inUse[id]:
		return homeworkflow.Deletion{}, assignment.ErrInUse
	}
	v = m.save(v)
	m.a.deleted[id] = true
	v.Deleted = true
	return homeworkflow.Deletion{Assignment: v}, nil
}

func (m *memChanges) RestoreAssignment(_ context.Context, id shared.AssignmentID, expected int64) (homeworkflow.AssignmentView, error) {
	v, ok := m.a.views[id]
	switch {
	case !ok:
		return homeworkflow.AssignmentView{}, shared.ErrNotFound
	case v.Version != expected:
		return homeworkflow.AssignmentView{}, shared.ErrVersionConflict
	case !m.a.deleted[id]:
		return homeworkflow.AssignmentView{}, assignment.ErrNotDeleted
	}
	delete(m.a.deleted, id)
	return m.save(v), nil
}

func (m *memChanges) setDone(id shared.AssignmentID, done bool, expected int64) homeworkflow.DoneOutcome {
	o := homeworkflow.DoneOutcome{ID: id}
	v, ok := m.a.views[id]
	switch {
	case !ok:
		o.Code, o.Err = homeworkflow.DoneNotFound, shared.ErrNotFound
	case m.a.deleted[id]:
		o.Code, o.Err = homeworkflow.DoneDeleted, assignment.ErrDeleted
	case expected > 0 && v.Version != expected:
		o.Code, o.Err = homeworkflow.DoneConflict, shared.ErrVersionConflict
	case v.Done == done:
		o.Code, o.Done, o.Version = homeworkflow.DoneUnchanged, v.Done, v.Version
	default:
		v.Done = done
		v = m.save(v)
		o.Code, o.Done, o.Version = homeworkflow.DoneChanged, v.Done, v.Version
	}
	return o
}

func (m *memChanges) MarkDone(_ context.Context, ids []shared.AssignmentID, done bool) (homeworkflow.DoneMarks, error) {
	m.marks++
	out := homeworkflow.DoneMarks{}
	seen := map[shared.AssignmentID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		o := m.setDone(id, done, 0)
		out.Outcomes = append(out.Outcomes, o)
		if o.Code == homeworkflow.DoneChanged {
			out.Prior = append(out.Prior, homeworkflow.DonePrior{ID: id, Done: !done, Version: o.Version})
		}
	}
	return out, nil
}

func (m *memChanges) RestoreDone(_ context.Context, prior []homeworkflow.DonePrior) (homeworkflow.DoneMarks, error) {
	m.restores++
	out := homeworkflow.DoneMarks{}
	for _, p := range prior {
		o := m.setDone(p.ID, p.Done, p.Version)
		out.Outcomes = append(out.Outcomes, o)
		if o.Code == homeworkflow.DoneChanged {
			out.Prior = append(out.Prior, homeworkflow.DonePrior{ID: p.ID, Done: !p.Done, Version: o.Version})
		}
	}
	return out, nil
}

func (m *memChanges) HasDeadlineEvent(_ context.Context, id shared.AssignmentID) (bool, error) {
	return m.events[id], m.eventErr
}

// newWave10World is newWorld with the wave-10 assignment use cases wired into Actions and the journal.
func newWave10World(t *testing.T) (*world, *memChanges) {
	t.Helper()
	w := newWorld(t)
	ch := newMemChanges(w.assignments)
	var err error
	if w.actions, err = NewActions(ActionDeps{
		Library: w.lib, Lessons: w.lessons, Assignments: w.assignments, NewAssignments: w.added, Rerunner: w.jobs,
		Jobs: w.activity, Canceller: w.jobs, Tx: w.tx, Clock: fixedClock{}, Changes: ch, Marks: ch, Events: ch,
	}); err != nil {
		t.Fatal(err)
	}
	if w.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: w.store, Compensator: w.actions, Resolver: w.actions, Clock: fixedClock{}, IDs: &seqActionIDs{},
	}); err != nil {
		t.Fatal(err)
	}
	w.actions.chat = w.journal
	return w, ch
}

// call10 is an admitted call of catalog v10 of the given class (wave 10 is declared, not served: TASK-326).
func call10(t *testing.T, id string, tool values.Tool, args string, class actionpolicy.Class) agentturn.Call {
	t.Helper()
	c, err := values.ParseCallAt(id, string(tool), args, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	ac := agentturn.Call{Call: c, Class: class, PlanStep: -1}
	if class == actionpolicy.ClassC {
		ac.Card = values.CardDeleteAssignment
	}
	return ac
}

// run10 runs an admitted call through tools.run (the deleted-assignment guard first) as request st and returns what
// it gave and its journal entry (zero when none).
func run10(t *testing.T, w *world, st agentturn.Step, c agentturn.Call) (ran, agentjournal.Entry) {
	t.Helper()
	r := w.tools(t).run(t.Context(), st, c)
	e, err := w.journal.Find(t.Context(), st.Thread, st.Turn, c.Call.ID())
	if err != nil {
		e = agentjournal.Entry{}
	}
	return r, e
}
