package sqlite

import (
	"context"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// TASK-342: a chat stored while the free-work card existed (a topic call answered {"status":"awaiting_student"}) still
// reads: the history entries come back as they were, and the thread loads. Nothing of the card was ever stored but
// that plain result, so nothing waits on it.
func TestAgentHistoryWithAnOldFreeWorkCall(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(thread.Request{Text: "сделай презентацию про Петра"}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	h := NewAgentHistory(db, fixedNow{agentT0})
	entries := []values.HistoryEntry{
		{Turn: 1, Entry: values.HistoryTurn{StudentText: "сделай презентацию про Петра"}},
		{Turn: 1, Entry: values.HistoryTurn{Calls: &values.SealedCalls{
			Calls: []values.RawCall{{CallID: "c1", Tool: "triage_assignment", ArgumentsJSON: `{"topic":"Реформы Петра I"}`}},
			Seal:  []byte{1},
		}}},
		{Turn: 1, Entry: values.HistoryTurn{Results: []values.ToolResult{{CallID: "c1", ResultJSON: `{"status":"awaiting_student"}`}}}},
	}
	for _, e := range entries {
		if err := h.Append(ctx, "th1", e.Turn, e.Entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := h.Since(ctx, "th1", 0, 100)
	if err != nil || !reflect.DeepEqual(got, entries) {
		t.Fatalf("Since = %+v, %v", got, err)
	}
	if _, err := repo.Get(ctx, "th1"); err != nil {
		t.Fatalf("Get = %v", err)
	}
}
