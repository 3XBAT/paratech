package tests

// edit_due of the agent after EditAssignment grew (TASK-230): on the real homework use case and SQLite it still
// changes only the deadline, keeps the time of day, and its Undo restores the old deadline exactly.

import (
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/testenv"
)

func TestAgentEditDueIsUnchanged(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment",
		Args: `{"title":"Эссе","note":"Стр. 10","due":"2026-10-30","time":"09:15"}`}).Then(endCall("e1"))
	e.ask(t, "запиши эссе к 30 октября к 9:15, стр. 10")
	id := addedAssignmentID("th1", 1, "c1")
	created, err := e.homework.Assignment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	e.hub.Then(testenv.AgentCall{ID: "d1", Tool: "edit_due", Args: `{"assignment":"` + string(id) + `","due":"2026-11-02"}`}).
		Then(endCall("e2"))
	e.ask(t, "перенеси эссе на 2 ноября")
	if got := e.plannerResults()["d1"]; got != `{"changed":true,"done":true}` {
		t.Fatalf("edit_due = %s", got)
	}
	moved, err := e.homework.Assignment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if want := created.Due.AddDate(0, 0, 3); !moved.Due.Equal(want) || moved.Title != created.Title ||
		moved.Description != created.Description || moved.Subject != created.Subject || moved.Version != created.Version+1 {
		t.Fatalf("after edit_due = %+v, want the due %v and the rest of %+v", moved, want, created)
	}

	entry, err := e.journal.Find(ctx, "th1", 2, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo of edit_due = %+v, %v", u, err)
	}
	back, err := e.homework.Assignment(ctx, id)
	if err != nil || !back.Due.Equal(created.Due) || back.Title != created.Title {
		t.Fatalf("after the Undo = %+v, %v; want the due %v", back, err, created.Due)
	}
}
