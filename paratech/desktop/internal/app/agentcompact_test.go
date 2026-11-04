package app

import (
	"context"
	"log/slog"
	"testing"

	"paratech/desktop/internal/cases/agentcompact"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
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
	started := func() { t.Error("an empty chat called the Hub") }
	if got, err := a.core.compact.Compact(ctx, "", started); err != nil || got != (agentcompact.Result{}) {
		t.Fatalf("Compact without a chat = %+v, %v", got, err)
	}

	created := wails.NewAgentService(ui.Agent, slog.New(slog.DiscardHandler)).NewChat(ctx)
	if created.Problem != "" {
		t.Fatalf("NewChat = %+v", created)
	}
	for _, id := range []string{created.Chat.ThreadID, ""} {
		got, err := a.core.compact.Compact(ctx, shared.ThreadID(id), started)
		want := agentcompact.Result{Thread: shared.ThreadID(created.Chat.ThreadID), Fill: agentcompact.Fill{Level: thread.FillNormal}}
		if err != nil || got != want {
			t.Fatalf("Compact of the empty chat %q = %+v, %v", id, got, err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// The compaction updates of the Driver reach the panel with their marks and fill (TASK-269).
func TestPanelUpdateMapsCompaction(t *testing.T) {
	fill := agentcompact.Fill{Percent: 92, Level: thread.FillFull, Overflow: true}
	got := panelUpdate(agentloop.Update{Thread: "th1", Compaction: true, Fill: &fill})
	if got.Thread != "th1" || !got.Compaction || got.Compacting || got.Fill == nil ||
		*got.Fill != (wails.AgentFillView{Percent: 92, Level: "full", Overflow: true}) {
		t.Fatalf("end = %+v", got)
	}
	if got := panelUpdate(agentloop.Update{Thread: "th1", Compaction: true, Compacting: true}); !got.Compaction ||
		!got.Compacting || got.Fill != nil {
		t.Fatalf("start = %+v", got)
	}
	if got := panelUpdate(agentloop.Update{Thread: "th1", Status: thread.StatusDone}); got.Compaction || got.Fill != nil {
		t.Fatalf("request = %+v", got)
	}
}
