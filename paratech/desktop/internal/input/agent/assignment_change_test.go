package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

const (
	editA1    = `{"assignment":"a1","action":"edit","title":"Эссе про Канта","due":"2026-11-05","time":"18:30","subject":"s1"}`
	deleteA1  = `{"assignment":"a1","action":"delete"}`
	restoreA1 = `{"assignment":"a1","action":"restore"}`
)

func undo(t *testing.T, w *world, e agentjournal.Entry) agentjournal.Entry {
	t.Helper()
	got, err := w.journal.Undo(t.Context(), e.ID)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	return got
}

// edit stores the named fields through EditAssignment, keeps the others, journals the prior fields read before it
// (RevertAssignment, pinned to the intent) and Undo writes them back exactly.
func TestChangeAssignmentEditAndUndo(t *testing.T) {
	w, _ := newWave10World(t)
	a1 := w.assignments.views["a1"]
	a1.Description, a1.CorrectedSubject, a1.CorrectedTeacher = "старое условие", "Матан (своё)", "Иванов"
	w.assignments.views["a1"] = a1
	before := a1

	r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, editA1, actionpolicy.ClassU))
	if r.out.Result.IsError || r.out.Result.ResultJSON != `{"changed":true,"done":true}` {
		t.Fatalf("result = %+v", r.out.Result)
	}
	got := w.assignments.views["a1"]
	wantDue := time.Date(2026, 11, 5, 18, 30, 0, 0, moscow)
	if got.Title != "Эссе про Канта" || !got.Due.Equal(wantDue) || got.CorrectedSubject != "Физика" ||
		got.Description != "старое условие" || got.CorrectedTeacher != "Иванов" {
		t.Fatalf("edited = %+v", got)
	}
	prior := action.AssignmentFields{Title: before.Title, Note: before.Description, Due: before.Due.UTC(), SubjectName: before.CorrectedSubject}
	ref := mustRef(t, values.KindAssignment, "a1")
	if e.Status != action.StatusDone || e.Compensation.Kind() != action.CompRevertAssignment ||
		e.Compensation != (action.RevertAssignment{Assignment: ref, Prior: prior}) || !e.CanUndo {
		t.Fatalf("entry = %+v", e)
	}
	for _, s := range []string{r.out.Result.ResultJSON, fmt.Sprint(r.cards)} {
		if strings.Contains(s, "Канта") || strings.Contains(s, "условие") {
			t.Fatalf("the student's words reached the planner or a card: %s", s)
		}
	}

	if u := undo(t, w, e); u.Status != action.StatusUndone {
		t.Fatalf("undo = %+v", u)
	}
	back := w.assignments.views["a1"]
	if back.Title != before.Title || !back.Due.Equal(before.Due) || back.Description != before.Description ||
		back.CorrectedSubject != before.CorrectedSubject || back.CorrectedTeacher != before.CorrectedTeacher {
		t.Fatalf("after undo = %+v, want %+v", back, before)
	}
}

func TestChangeAssignmentEditKeepsWhatItDoesNotName(t *testing.T) {
	w, _ := newWave10World(t)
	a1 := w.assignments.views["a1"]
	a1.Due = time.Date(2026, 10, 24, 15, 30, 17, 0, time.UTC) // an extracted deadline with seconds stays exact
	a1.CorrectedSubject = "Матан (своё)"
	w.assignments.views["a1"] = a1

	r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, `{"assignment":"a1","action":"edit","note":"10 страниц"}`, actionpolicy.ClassU))
	got := w.assignments.views["a1"]
	if r.out.Result.IsError || got.Description != "10 страниц" || got.Title != a1.Title || !got.Due.Equal(a1.Due) ||
		got.CorrectedSubject != "Матан (своё)" {
		t.Fatalf("result %+v, edited = %+v", r.out.Result, got)
	}
	if e.Compensation.Kind() != action.CompRevertAssignment {
		t.Fatalf("kind = %s", e.Compensation.Kind())
	}
}

func TestChangeAssignmentEditRefusals(t *testing.T) {
	tests := []struct {
		name string
		args string
		want values.Refusal
	}{
		{"unknown subject", `{"assignment":"a1","action":"edit","subject":"s9"}`, values.RefuseNotFound},
		{"missing assignment", `{"assignment":"zz","action":"edit","title":"x"}`, values.RefuseNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := newWave10World(t)
			before := w.assignments.views["a1"]
			r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, tt.args, actionpolicy.ClassU))
			if !r.out.Result.IsError || !strings.Contains(r.out.Result.ResultJSON, string(tt.want)) || e.ID != "" {
				t.Fatalf("result = %+v, entry %+v; want %s, no entry", r.out.Result, e, tt.want)
			}
			if w.assignments.views["a1"] != before {
				t.Fatal("a refused edit changed the assignment")
			}
		})
	}

	t.Run("nothing changes: no entry, no «Отменить»", func(t *testing.T) {
		w, _ := newWave10World(t)
		r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, `{"assignment":"a1","action":"edit","title":"Задачи 1-5"}`, actionpolicy.ClassU))
		if r.out.Result.ResultJSON != `{"changed":false,"done":true}` || e.ID != "" || len(r.cards) != 0 {
			t.Fatalf("result = %+v, entry %+v, cards %v", r.out.Result, e, r.cards)
		}
	})
}

// Undo of each change_assignment action whose target changed since is «Изменено после»; nothing is written back.
func TestChangeAssignmentUndoOfAChangedTarget(t *testing.T) {
	tests := []struct {
		name   string
		action string
		change func(w *world, ch *memChanges)
		kind   action.CompensationKind
	}{
		{"edit, then the student edits", editA1, func(w *world, _ *memChanges) {
			v := w.assignments.views["a1"]
			v.Title, v.Version = "Своё", v.Version+1
			w.assignments.views["a1"] = v
		}, action.CompRevertAssignment},
		{"edit, then the student deletes", editA1, func(w *world, _ *memChanges) {
			w.assignments.deleted["a1"] = true
		}, action.CompRevertAssignment},
		{"delete, then the student restores", deleteA1, func(w *world, ch *memChanges) {
			v, _ := ch.AssignmentState(t.Context(), "a1")
			if _, err := ch.RestoreAssignment(t.Context(), "a1", v.Version); err != nil {
				t.Fatal(err)
			}
		}, action.CompRestoreAssignment},
		{"delete, then the student restores and deletes again", deleteA1, func(w *world, ch *memChanges) {
			v, _ := ch.AssignmentState(t.Context(), "a1")
			r, _ := ch.RestoreAssignment(t.Context(), "a1", v.Version)
			if _, err := ch.TrashAssignment(t.Context(), "a1", r.Version); err != nil {
				t.Fatal(err)
			}
		}, action.CompRestoreAssignment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, ch := newWave10World(t)
			_, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, tt.action, actionpolicy.ClassU))
			if e.Status != action.StatusDone || e.Compensation.Kind() != tt.kind {
				t.Fatalf("entry = %+v, want DONE with %s", e, tt.kind)
			}
			tt.change(w, ch)
			snapshot := w.assignments.views["a1"]
			deleted := w.assignments.deleted["a1"]
			u := undo(t, w, e)
			if u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
				t.Fatalf("undo = %+v, want «Изменено после»", u)
			}
			if w.assignments.views["a1"] != snapshot || w.assignments.deleted["a1"] != deleted {
				t.Fatal("a refused undo changed the assignment")
			}
		})
	}
}

// delete without a Google event is U: the page's soft delete, Undo is «Вернуть». The fact is read again before the
// delete: an event that appeared since Admit, or a fact that cannot be read, refuses and deletes nothing.
func TestChangeAssignmentDelete(t *testing.T) {
	w, ch := newWave10World(t)
	r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
	ref := mustRef(t, values.KindAssignment, "a1")
	if r.out.Result.ResultJSON != `{"changed":true,"done":true}` || !w.assignments.deleted["a1"] {
		t.Fatalf("result = %+v, deleted %v", r.out.Result, w.assignments.deleted["a1"])
	}
	if e.Status != action.StatusDone || e.Compensation != (action.RestoreAssignment{Assignment: ref}) ||
		e.Compensation.Kind() != action.CompRestoreAssignment {
		t.Fatalf("entry = %+v", e)
	}
	if u := undo(t, w, e); u.Status != action.StatusUndone || w.assignments.deleted["a1"] {
		t.Fatalf("undo = %+v, deleted %v", u, w.assignments.deleted["a1"])
	}

	for name, set := range map[string]func(){
		"an event appeared since Admit": func() { ch.events["a2"] = true },
		"the fact cannot be read":       func() { ch.eventErr = errors.New("disk I/O error") },
	} {
		t.Run(name, func(t *testing.T) {
			w, ch = newWave10World(t)
			set()
			r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, `{"assignment":"a2","action":"delete"}`, actionpolicy.ClassU))
			if !r.out.Result.IsError || e.ID != "" || w.assignments.deleted["a2"] {
				t.Fatalf("result = %+v, entry %+v, deleted %v", r.out.Result, e, w.assignments.deleted["a2"])
			}
		})
	}

	t.Run("without the event reader nothing is deleted", func(t *testing.T) {
		w, _ := newWave10World(t)
		w.actions.events = nil
		r, _ := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
		if !strings.Contains(r.out.Result.ResultJSON, string(values.RefuseUnavailable)) || w.assignments.deleted["a1"] {
			t.Fatalf("result = %+v", r.out.Result)
		}
	})

	t.Run("in use (an email may go out)", func(t *testing.T) {
		w, ch := newWave10World(t)
		ch.inUse["a1"] = true
		r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
		if !strings.Contains(r.out.Result.ResultJSON, string(values.RefuseNotNow)) || e.Status != action.StatusFailed {
			t.Fatalf("result = %+v, entry %+v", r.out.Result, e)
		}
	})
}

// delete of an assignment with a Google event is C: the agent only makes the delete_assignment card; nothing is
// deleted, the journal holds the proposal.
func TestChangeAssignmentDeleteWithAnEventMakesTheCard(t *testing.T) {
	w, ch := newWave10World(t)
	ch.events["a1"] = true
	r, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassC))
	ref := mustRef(t, values.KindAssignment, "a1")
	if r.out.Result.IsError || r.out.Result.ResultJSON != `{"preview":"a1","proposed":true,"waits_for":"student"}` {
		t.Fatalf("result = %+v", r.out.Result)
	}
	if len(r.cards) != 1 || r.cards[0] != (values.DeleteAssignmentCard{Assignment: ref}) {
		t.Fatalf("cards = %+v", r.cards)
	}
	if w.assignments.deleted["a1"] || e.Status != action.StatusProposed || e.Compensation != nil {
		t.Fatalf("deleted %v, entry %+v", w.assignments.deleted["a1"], e)
	}
	// The same call again answers from the journal, without a second card.
	again, _ := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassC))
	if again.out.Result != r.out.Result || len(again.cards) != 0 {
		t.Fatalf("replay = %+v, cards %v", again.out.Result, again.cards)
	}
	// Edit and restore never come as C.
	bad, _ := run10(t, w, req, call10(t, "c2", values.ToolChangeAssignment, `{"assignment":"a1","action":"edit","title":"x"}`, actionpolicy.ClassC))
	if !strings.Contains(bad.out.Result.ResultJSON, string(values.RefuseUnavailable)) {
		t.Fatalf("edit as C = %+v", bad.out.Result)
	}
}

// restore brings back only what this chat deleted (ADR-0029 C-2), as the controlled exception of the deleted-ID guard;
// its Undo deletes it again, unless it got a Google event since.
func TestChangeAssignmentRestore(t *testing.T) {
	w, _ := newWave10World(t)
	if _, e := run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU)); e.Status != action.StatusDone {
		t.Fatalf("delete = %+v", e)
	}
	// Another tool naming the deleted assignment still gets unknown_id.
	other, _ := run10(t, w, req, call10(t, "c2", values.ToolChangeAssignment, `{"assignment":"a1","action":"edit","title":"x"}`, actionpolicy.ClassU))
	if !strings.Contains(other.out.Result.ResultJSON, string(values.RefuseUnknownID)) {
		t.Fatalf("edit of a deleted one = %+v", other.out.Result)
	}

	r, e := run10(t, w, req, call10(t, "c3", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU))
	ref := mustRef(t, values.KindAssignment, "a1")
	if r.out.Result.ResultJSON != `{"changed":true,"done":true,"id":"a1"}` || w.assignments.deleted["a1"] {
		t.Fatalf("restore = %+v, deleted %v", r.out.Result, w.assignments.deleted["a1"])
	}
	if e.Status != action.StatusDone || e.Compensation != (action.DeleteAssignment{Assignment: ref}) ||
		e.Compensation.Kind() != action.CompDeleteAssignment {
		t.Fatalf("entry = %+v", e)
	}
	// Restored: a second restore has nothing of this chat to bring back.
	twice, _ := run10(t, w, req, call10(t, "c4", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU))
	if !strings.Contains(twice.out.Result.ResultJSON, string(values.RefuseUnknownID)) {
		t.Fatalf("second restore = %+v", twice.out.Result)
	}

	if u := undo(t, w, e); u.Status != action.StatusUndone || !w.assignments.deleted["a1"] {
		t.Fatalf("undo of the restore = %+v, deleted %v", u, w.assignments.deleted["a1"])
	}
	// Deleted again by this chat's Undo: it may be restored again.
	if r, _ := run10(t, w, req, call10(t, "c5", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU)); r.out.Result.IsError {
		t.Fatalf("restore after the undo = %+v", r.out.Result)
	}

	t.Run("undo of a restore whose assignment got a Google event is refused", func(t *testing.T) {
		w, ch := newWave10World(t)
		run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
		_, e := run10(t, w, req, call10(t, "c2", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU))
		ch.events["a1"] = true
		if u := undo(t, w, e); u.Status != action.StatusUndoRefused || w.assignments.deleted["a1"] {
			t.Fatalf("undo = %+v, deleted %v", u, w.assignments.deleted["a1"])
		}
	})
}

func TestChangeAssignmentRestoreRefusesWhatThisChatDidNotDelete(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, w *world, ch *memChanges)
	}{
		{"deleted by the student", func(_ *testing.T, w *world, _ *memChanges) { w.assignments.deleted["a1"] = true }},
		{"deleted by another chat", func(t *testing.T, w *world, _ *memChanges) {
			run10(t, w, agentturn.Step{Thread: "th-other", Turn: 1}, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
		}},
		{"deleted by this chat, then restored and deleted again on the page", func(t *testing.T, w *world, ch *memChanges) {
			run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
			v, _ := ch.AssignmentState(t.Context(), "a1")
			r, _ := ch.RestoreAssignment(t.Context(), "a1", v.Version)
			d, _ := ch.TrashAssignment(t.Context(), "a1", r.Version)
			later := d.Assignment
			later.Deleted, later.UpdatedAt = false, now.Add(time.Hour)
			w.assignments.views["a1"] = later
		}},
		{"not deleted at all", func(*testing.T, *world, *memChanges) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, ch := newWave10World(t)
			tt.prepare(t, w, ch)
			deleted := w.assignments.deleted["a1"]
			r, e := run10(t, w, req, call10(t, "c9", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU))
			if !strings.Contains(r.out.Result.ResultJSON, string(values.RefuseUnknownID)) || e.ID != "" {
				t.Fatalf("restore = %+v, entry %+v; want unknown_id", r.out.Result, e)
			}
			if w.assignments.deleted["a1"] != deleted {
				t.Fatal("a refused restore changed the assignment")
			}
		})
	}
}

// A replayed call answers from its entry: nothing runs twice; the result is rebuilt from the compensation.
func TestChangeAssignmentReplay(t *testing.T) {
	for _, args := range []string{editA1, deleteA1} {
		w, _ := newWave10World(t)
		c := call10(t, "c1", values.ToolChangeAssignment, args, actionpolicy.ClassU)
		first, _ := run10(t, w, req, c)
		version := w.assignments.views["a1"].Version
		second, _ := act(t, w, c)
		if second.out.Result != first.out.Result || w.assignments.views["a1"].Version != version || len(w.store.order) != 1 {
			t.Fatalf("%s: replay = %+v, first %+v", args, second.out.Result, first.out.Result)
		}
	}
}

// Recover settles a PENDING entry of each action by its intent after a crash between the use case and DONE.
func TestChangeAssignmentRecover(t *testing.T) {
	tests := []struct {
		name string
		args string
		kind action.CompensationKind
	}{
		{"edit", editA1, action.CompRevertAssignment},
		{"delete", deleteA1, action.CompRestoreAssignment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := newWave10World(t)
			crash(t, w, call10(t, "c1", values.ToolChangeAssignment, tt.args, actionpolicy.ClassU))
			e := recovered(t, w, "c1")
			if e.Status != action.StatusDone || e.Compensation.Kind() != tt.kind {
				t.Fatalf("recovered = %+v, want DONE with %s", e, tt.kind)
			}
		})
	}

	t.Run("restore", func(t *testing.T) {
		w, _ := newWave10World(t)
		run10(t, w, req, call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU))
		crash(t, w, call10(t, "c2", values.ToolChangeAssignment, restoreA1, actionpolicy.ClassU))
		if e := recovered(t, w, "c2"); e.Status != action.StatusDone || e.Compensation.Kind() != action.CompDeleteAssignment {
			t.Fatalf("recovered = %+v", e)
		}
	})

	t.Run("the use case did not run: FAILED", func(t *testing.T) {
		w, _ := newWave10World(t)
		p, ok, err := w.actions.prepare(t.Context(), originOf(req), call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU).Call)
		if !ok || err != nil {
			t.Fatal(ok, err)
		}
		if _, err := w.journal.Begin(t.Context(), agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: "c1",
			Tool: values.ToolChangeAssignment, Intent: p.intent}); err != nil {
			t.Fatal(err)
		}
		if e := recovered(t, w, "c1"); e.Status != action.StatusFailed {
			t.Fatalf("recovered = %+v", e)
		}
	})
}

// crash journals c PENDING and runs its use case without recording the outcome, as a crash in between would.
func crash(t *testing.T, w *world, c agentturn.Call) {
	t.Helper()
	p, ok, err := w.actions.prepare(t.Context(), originOf(req), c.Call)
	if !ok || err != nil {
		t.Fatalf("prepare: %v, %v", ok, err)
	}
	if _, err := w.journal.Begin(t.Context(), agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: c.Call.ID(),
		Tool: c.Call.Tool(), Intent: p.intent}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.run(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func recovered(t *testing.T, w *world, callID string) agentjournal.Entry {
	t.Helper()
	if _, err := w.journal.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, callID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// The facts of change_assignment: only a delete reads the Google event, from the local projection; an error or no
// reader fails closed.
func TestFactsCalendarEvent(t *testing.T) {
	ch := newMemChanges(&memAssignments{})
	ch.events["a1"] = true
	f, err := NewFacts(&memLessons{})
	if err != nil {
		t.Fatal(err)
	}
	withEvents := f.WithCalendarEvents(ch)
	del := call10(t, "c1", values.ToolChangeAssignment, deleteA1, actionpolicy.ClassU).Call
	if got, err := withEvents.Facts(t.Context(), del); err != nil || !got.HasCalendarEvent {
		t.Fatalf("delete with an event = %+v, %v", got, err)
	}
	other := call10(t, "c2", values.ToolChangeAssignment, `{"assignment":"a2","action":"delete"}`, actionpolicy.ClassU).Call
	if got, err := withEvents.Facts(t.Context(), other); err != nil || got.HasCalendarEvent {
		t.Fatalf("delete without an event = %+v, %v", got, err)
	}
	edit := call10(t, "c3", values.ToolChangeAssignment, editA1, actionpolicy.ClassU).Call
	ch.eventErr = errors.New("disk I/O error")
	if got, err := withEvents.Facts(t.Context(), edit); err != nil || got.HasCalendarEvent {
		t.Fatalf("edit reads no event: %+v, %v", got, err)
	}
	if _, err := withEvents.Facts(t.Context(), del); err == nil {
		t.Fatal("a read error is no fact")
	}
	if _, err := f.Facts(t.Context(), del); err == nil {
		t.Fatal("without the reader a delete has no fact")
	}
}
