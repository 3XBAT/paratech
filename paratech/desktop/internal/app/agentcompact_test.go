package app

import (
	"context"
	"log/slog"
	"testing"

	"paratech/desktop/internal/cases/agentcompact"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// Context compaction is wired to the stored chats (TASK-268): without a current chat it does nothing, and an empty chat
// needs no summary, so the Hub is never asked.
func TestAgentCompactWiring(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	ui := startCore(t, a)
	if a.core.compact == nil {
		t.Fatal("compaction not wired")
	}
	if got, err := a.core.compact.Compact(ctx, ""); err != nil || got != (agentcompact.Result{}) {
		t.Fatalf("Compact without a chat = %+v, %v", got, err)
	}

	created := wails.NewAgentService(ui.Agent, slog.New(slog.DiscardHandler)).NewChat(ctx)
	if created.Problem != "" {
		t.Fatalf("NewChat = %+v", created)
	}
	for _, id := range []string{created.Chat.ThreadID, ""} {
		got, err := a.core.compact.Compact(ctx, shared.ThreadID(id))
		if err != nil || got != (agentcompact.Result{Fill: agentcompact.Fill{Level: thread.FillNormal}}) {
			t.Fatalf("Compact of the empty chat %q = %+v, %v", id, got, err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
