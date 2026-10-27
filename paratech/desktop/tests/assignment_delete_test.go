package tests

// «Удалить» of an assignment (TASK-228, ADR-0029 addendum 2026-10-27) end to end on SQLite: the agent adds an
// assignment, the student deletes it; the homework list and the agent no longer see it (list_assignments skips it, a
// call naming it is unknown_id), the Undo of its creation is refused as changed, and «Вернуть» brings it back.

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func TestDeletedAssignmentIsHiddenFromTheAgent(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment", Args: `{"title":"Эссе","due":"2026-10-30"}`}).Then(endCall("e1"))
	e.ask(t, "запиши эссе к 30 октября")
	id := addedAssignmentID("th1", 1, "c1")
	v, err := e.homework.Assignment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := e.homework.DeleteAssignment(ctx, id, v.Version)
	if err != nil || !deleted.Deleted || deleted.Version != v.Version+1 {
		t.Fatalf("DeleteAssignment = %+v, %v", deleted, err)
	}
	if list, err := e.homework.ListAssignments(ctx); err != nil || len(list) != 0 {
		t.Fatalf("assignments after the delete = %+v, %v", list, err)
	}
	if _, err := e.homework.Assignment(ctx, id); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Assignment of a deleted one: %v", err)
	}

	// The thread saw the ID in the create's result; the list skips it now and a call naming it is unknown_id.
	e.hub.Then(testenv.AgentCall{ID: "l1", Tool: "list_assignments", Args: `{}`}).
		Then(testenv.AgentCall{ID: "d1", Tool: "edit_due", Args: `{"assignment":"` + string(id) + `","due":"2026-11-02"}`}).
		Then(endCall("e2"))
	e.ask(t, "перенеси эссе на 2 ноября")
	results := e.plannerResults()
	if strings.Contains(results["l1"], string(id)) {
		t.Fatalf("list_assignments shows the deleted assignment: %s", results["l1"])
	}
	if results["d1"] != `{"error":"unknown_id"}` {
		t.Fatalf("edit_due of a deleted assignment: %s, want unknown_id", results["d1"])
	}

	entry, err := e.journal.Find(ctx, "th1", 1, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
		t.Fatalf("Undo of the creation after the delete = %+v, %v", u, err)
	}

	restored, err := e.homework.RestoreAssignment(ctx, id, deleted.Version)
	if err != nil || restored.Deleted || !restored.Due.Equal(v.Due) {
		t.Fatalf("RestoreAssignment = %+v, %v", restored, err)
	}
	if list, err := e.homework.ListAssignments(ctx); err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("assignments after the restore = %+v, %v", list, err)
	}
	if _, err := e.homework.RestoreAssignment(ctx, id, restored.Version); !errors.Is(err, assignment.ErrNotDeleted) {
		t.Fatalf("second restore: %v", err)
	}
}
