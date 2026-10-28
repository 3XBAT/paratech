package agent

import (
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// TASK-274 (ADR-0029 addendum 2): the purge after the restore window removes the assignment row, the journal line
// stays with the ID only. An Undo naming the purged ID applies nothing and brings nothing back: the target is gone
// (the journal's rule for a vanished target, the same as "after it was removed" of create_assignment), so the line
// ends UNDONE without a write.
func TestUndoOfAnActionOnAPurgedAssignment(t *testing.T) {
	for name, call := range map[string]struct {
		tool values.Tool
		args string
		id   func() shared.AssignmentID
	}{
		"edit_due":          {values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`, func() shared.AssignmentID { return "a1" }},
		"create_assignment": {values.ToolCreateAssignment, `{"title":"Эссе","due":"2026-10-30"}`, addedID},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			_, e := act(t, w, writeCall(t, "c1", call.tool, call.args))
			id := call.id()
			if _, ok := w.assignments.views[id]; !ok {
				t.Fatalf("fixture: %s missing after the action", id)
			}

			// Deleted by the student, then purged after the window: the row is gone.
			delete(w.assignments.views, id)
			w.assignments.deleted = nil

			u, err := w.journal.Undo(t.Context(), e.ID)
			if err != nil || u.Status != action.StatusUndone {
				t.Fatalf("Undo = %+v, %v; want UNDONE with nothing applied", u, err)
			}
			if _, back := w.assignments.views[id]; back {
				t.Fatal("Undo brought a purged assignment back")
			}
		})
	}
}
