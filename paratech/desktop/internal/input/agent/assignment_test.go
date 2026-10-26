package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/shared"
)

// memAdded is homeworkflow's CreateAssignment and RemoveCreatedAssignment over the assignments fake: a repeat of an
// ID returns the stored one, the remove compares the version and refuses an assignment in use.
type memAdded struct {
	views   *memAssignments
	inUse   map[shared.AssignmentID]bool
	creates []homeworkflow.NewAssignment
	removes int
	err     error
}

func (m *memAdded) CreateAssignment(_ context.Context, in homeworkflow.NewAssignment) (homeworkflow.AssignmentView, error) {
	m.creates = append(m.creates, in)
	if m.err != nil {
		return homeworkflow.AssignmentView{}, m.err
	}
	if v, ok := m.views.views[in.ID]; ok {
		return v, nil
	}
	var subjectName string
	switch in.Subject {
	case "":
	case "s1":
		subjectName = "Физика"
	default:
		return homeworkflow.AssignmentView{}, homeworkflow.ErrSubjectNotFound
	}
	if in.Lesson != "" && in.Lesson != "l1" {
		return homeworkflow.AssignmentView{}, fmt.Errorf("lesson: %w", shared.ErrNotFound)
	}
	due, err := assignment.StudentDue(in.Date, in.Time, moscow)
	if err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	v := homeworkflow.AssignmentView{
		ID: in.ID, LessonID: in.Lesson, Title: in.Title, Description: in.Note, Due: due, Subject: subjectName,
		UserEdited: true, Version: 1,
	}
	m.views.views[in.ID] = v
	return v, nil
}

func (m *memAdded) RemoveCreatedAssignment(_ context.Context, id shared.AssignmentID, expected int64) error {
	m.removes++
	v, ok := m.views.views[id]
	switch {
	case !ok:
		return fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	case v.Version != expected:
		return shared.ErrVersionConflict
	case m.inUse[id]:
		return assignment.ErrInUse
	}
	delete(m.views.views, id)
	return nil
}

// addedID is the ID the call c1 of req adds.
func addedID() shared.AssignmentID { return addedAssignmentID(originOf(req), "c1") }

// create_assignment adds the assignment under an ID derived from the call's key, journals it with its Undo and shows
// the planner the ID only; the same call again adds nothing (TASK-217, ADR-0029).
func TestCreateAssignmentAndItsReplay(t *testing.T) {
	w := newWorld(t)
	c := writeCall(t, "c1", values.ToolCreateAssignment,
		`{"title":"Скинуть проверочную Иванову","due":"2026-10-23","time":"09:00","subject":"s1","note":"с ответами"}`)
	first, e := act(t, w, c)
	id := addedID()
	ref := mustRef(t, values.KindAssignment, string(id))
	if first.out.Result.IsError || first.out.Result.ResultJSON != `{"changed":true,"done":true,"id":"`+string(id)+`"}` ||
		first.out.Created != ref {
		t.Fatalf("result = %+v", first.out)
	}
	if !strings.HasPrefix(string(id), addedIDPrefix) || id.Validate() != nil {
		t.Fatalf("derived id %q", id)
	}
	want := homeworkflow.NewAssignment{
		ID: id, Title: "Скинуть проверочную Иванову", Note: "с ответами", Subject: "s1", Date: "2026-10-23", Time: "09:00",
	}
	if len(w.added.creates) != 1 || w.added.creates[0] != want {
		t.Fatalf("creates = %+v", w.added.creates)
	}
	if e.Status != action.StatusDone || e.Compensation != (action.RemoveAssignment{Assignment: ref}) || !e.CanUndo {
		t.Fatalf("entry = %+v", e)
	}
	if len(first.cards) != 1 || first.cards[0] != (values.ActionLine{Action: e.ID, Assignment: values.AssignmentLine{Assignment: ref}}) {
		t.Fatalf("cards = %+v", first.cards)
	}
	// The planner's result, the journal intent and the entry never carry the student's words.
	for _, s := range []string{first.out.Result.ResultJSON, fmt.Sprint(e), fmt.Sprint(w.store.states)} {
		if strings.Contains(s, "Иванов") || strings.Contains(s, "ответами") {
			t.Fatalf("the title or note leaked: %s", s)
		}
	}

	second, _ := act(t, w, c)
	if second.out.Result != first.out.Result || second.out.Created != ref || len(w.added.creates) != 1 || len(w.store.order) != 1 {
		t.Fatalf("replay = %+v, creates %d, entries %d", second.out, len(w.added.creates), len(w.store.order))
	}

	// Another call or another request adds another assignment: the key differs.
	other := addedAssignmentID(origin{thread: req.Thread, turn: 2}, "c1")
	if other == id || addedAssignmentID(originOf(req), "c2") == id {
		t.Fatal("the derived id does not depend on the request and the call")
	}
}

// Undo removes the added assignment while it is as the action left it; a student edit, a draft, a work or a calendar
// proposal since (the use case's ErrInUse or another version) refuses it as «Изменено после».
func TestUndoOfAnAddedAssignment(t *testing.T) {
	for name, tt := range map[string]struct {
		after func(w *world)
		want  action.Status
		kept  bool
	}{
		"at once": {func(*world) {}, action.StatusUndone, false},
		"after a student edit": {func(w *world) {
			v := w.assignments.views[addedID()]
			v.Version++
			w.assignments.views[addedID()] = v
		}, action.StatusUndoRefused, true},
		"after a draft":        {func(w *world) { w.added.inUse[addedID()] = true }, action.StatusUndoRefused, true},
		"after it was removed": {func(w *world) { delete(w.assignments.views, addedID()) }, action.StatusUndone, false},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			_, e := act(t, w, writeCall(t, "c1", values.ToolCreateAssignment, `{"title":"Эссе","due":"2026-10-30"}`))
			tt.after(w)
			u, err := w.journal.Undo(t.Context(), e.ID)
			if err != nil || u.Status != tt.want {
				t.Fatalf("Undo = %+v, %v", u, err)
			}
			if tt.want == action.StatusUndoRefused && u.NoUndo != action.NoUndoChanged {
				t.Fatalf("refusal = %q", u.NoUndo)
			}
			if _, kept := w.assignments.views[addedID()]; kept != tt.kept {
				t.Fatalf("assignment kept = %v", kept)
			}
		})
	}
}

// «Отменить всё» of a plan that added an assignment and moved its deadline: the deadline is undone first, the
// creation follows the assignment's version and removes it.
func TestUndoRequestOfAnAddedAssignmentAndItsDeadline(t *testing.T) {
	w := newWorld(t)
	_, e := act(t, w, writeCall(t, "c1", values.ToolCreateAssignment, `{"title":"Эссе","due":"2026-10-30"}`))
	edit := writeCall(t, "c2", values.ToolEditDue, `{"assignment":"`+string(addedID())+`","due":"2026-11-02"}`)
	edit.PlanStep = 1
	if r, _ := act(t, w, edit); r.out.Result.IsError {
		t.Fatalf("edit_due of the added assignment = %+v", r.out.Result)
	}
	undone, err := w.journal.UndoRequest(t.Context(), req.Thread, req.Turn)
	if err != nil || len(undone) != 2 || undone[0].Status != action.StatusUndone || undone[1].Status != action.StatusUndone ||
		undone[1].ID != e.ID {
		t.Fatalf("UndoRequest = %+v, %v", undone, err)
	}
	if _, ok := w.assignments.views[addedID()]; ok {
		t.Fatal("the added assignment is still there")
	}
}

// A crash between the journal's PENDING and its outcome is settled by the ID the intent named: the assignment is there
// -> DONE with Undo, absent -> FAILED.
func TestRecoverOfAnAddedAssignment(t *testing.T) {
	for _, ran := range []bool{true, false} {
		w := newWorld(t)
		ctx := t.Context()
		c := writeCall(t, "c1", values.ToolCreateAssignment, `{"title":"Эссе"}`)
		p, ok, err := w.actions.prepare(ctx, originOf(req), c.Call)
		if !ok || err != nil || p.intent != (action.Intent{Target: mustRef(t, values.KindAssignment, string(addedID()))}) {
			t.Fatalf("prepare = %+v, %v, %v", p.intent, ok, err)
		}
		e, err := w.journal.Begin(ctx, agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: "c1", Tool: c.Call.Tool(), Intent: p.intent})
		if err != nil {
			t.Fatal(err)
		}
		if ran {
			if _, err := p.run(ctx); err != nil {
				t.Fatal(err)
			}
		}
		rep, err := w.journal.Recover(ctx)
		want := agentjournal.RecoverReport{Failed: 1}
		if ran {
			want = agentjournal.RecoverReport{Done: 1}
		}
		if err != nil || rep != want {
			t.Fatalf("ran %v: Recover = %+v, %v", ran, rep, err)
		}
		got, err := w.journal.Find(ctx, req.Thread, req.Turn, "c1")
		if err != nil || got.ID != e.ID {
			t.Fatal(err)
		}
		if ran && (got.Status != action.StatusDone || !got.CanUndo) || !ran && got.Status != action.StatusFailed {
			t.Fatalf("ran %v: entry = %+v", ran, got)
		}
		if ran {
			if u, err := w.journal.Undo(ctx, e.ID); err != nil || u.Status != action.StatusUndone || len(w.assignments.views) != 2 {
				t.Fatalf("Undo of the settled entry = %+v, %v", u, err)
			}
		}
	}
}

// Errors of the use case are enums: invalid details are invalid_arguments, an unknown subject or lesson unknown_id; the
// entry is FAILED and no text reaches the planner.
func TestCreateAssignmentRefusals(t *testing.T) {
	for name, tt := range map[string]struct {
		args string
		err  error
		want values.Refusal
	}{
		"invalid details": {`{"title":"Эссе","due":"2026-10-30"}`, fmt.Errorf("x: %w", assignment.ErrInvalidDue), values.RefuseInvalidArgs},
		"unknown subject": {`{"title":"Эссе","subject":"s9"}`, nil, values.RefuseUnknownID},
		"unknown lesson":  {`{"title":"Эссе","lesson":"l9"}`, nil, values.RefuseUnknownID},
		"failed":          {`{"title":"Эссе"}`, errors.New("disk full"), values.RefuseFailed},
		"taken id":        {`{"title":"Эссе"}`, fmt.Errorf("x: %w", shared.ErrDuplicate), values.RefuseFailed},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.added.err = tt.err
			r, e := act(t, w, writeCall(t, "c1", values.ToolCreateAssignment, tt.args))
			if !r.out.Result.IsError || r.out.Result.ResultJSON != `{"error":"`+string(tt.want)+`"}` || !r.out.Created.IsZero() {
				t.Fatalf("result = %+v", r.out)
			}
			if e.Status != action.StatusFailed {
				t.Fatalf("entry = %+v", e)
			}
		})
	}

	// Without the use case wired the tool is unavailable and nothing is journaled.
	w := newWorld(t)
	w.actions.added = nil
	r, e := act(t, w, writeCall(t, "c1", values.ToolCreateAssignment, `{"title":"Эссе"}`))
	if r.out.Result.ResultJSON != `{"error":"unavailable"}` || e.ID != "" {
		t.Fatalf("unwired = %+v, %+v", r.out.Result, e)
	}
}

// The Driver shows the action line of an added assignment with its title, library subject and deadline from the local
// data; the planner got only the ID, and neither its result nor the log carries the title.
func TestActionLineOfAnAddedAssignment(t *testing.T) {
	w := newWorld(t)
	w.reads.library.resolve = map[string]subjectmatch.Match{"Физика": {ID: "s1", Name: "Физика"}}
	turns := (&scriptedTurns{}).
		then(agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusExecuting, Calls: []agentturn.Call{
			writeCall(t, "c1", values.ToolCreateAssignment, `{"title":"Скинуть проверочную Иванову","due":"2026-10-23","time":"09:00","subject":"s1"}`),
		}}, nil).
		then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Задание добавлено, срок 2026-10-23."}), nil)
	var buf bytes.Buffer
	sink := &chanSink{}
	deps := w.deps(turns, sink)
	deps.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "завтра в 9 скинуть проверочную Иванову"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	var lines []values.ActionLine
	for _, u := range sink.all() {
		for _, c := range u.Cards {
			if l, ok := c.(values.ActionLine); ok {
				lines = append(lines, l)
			}
		}
	}
	due, _ := values.NewDate(2026, time.October, 23)
	nine, _ := values.NewTimeOfDay(9, 0)
	want := values.AssignmentLine{
		Assignment: mustRef(t, values.KindAssignment, string(addedID())), Title: "Скинуть проверочную Иванову",
		Subject: mustRef(t, values.KindSubject, "s1"), Due: due, DueTime: nine,
	}
	if len(lines) != 1 || lines[0].Assignment != want || lines[0].Validate() != nil {
		t.Fatalf("action lines = %+v", lines)
	}
	sub := turns.report().submitted
	if len(sub) != 1 || strings.Contains(sub[0].Outcomes[0].Result.ResultJSON, "Иванов") {
		t.Fatalf("submitted = %+v", sub)
	}
	if strings.Contains(buf.String(), "Иванов") || strings.Contains(buf.String(), "проверочн") {
		t.Fatalf("the log has the title:\n%s", buf.String())
	}

	// An assignment gone before the line is shown: the line stays, without it.
	line := w.tools(t).actionLine(t.Context(), values.ActionLine{Action: "act9", Assignment: values.AssignmentLine{
		Assignment: mustRef(t, values.KindAssignment, "a9")}})
	if line != (values.ActionLine{Action: "act9"}) || line.Validate() != nil {
		t.Fatalf("line of a gone assignment = %+v", line)
	}
}
