package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// The agent core is wired and its startup recovery runs: a request left running stops as «нет связи», a journal
// entry left PENDING becomes UNKNOWN.
func TestAgentRecoveryAtStart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// The core runs on the real clock; the seeded rows are older than the start.
	at := time.Now().Add(-time.Hour).UTC()

	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dir, dbFile), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th1", thread.NoSubject, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartRequest(values.Scope{}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(at); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewAgentThreadRepository(db).Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	pending, err := action.New(action.Spec{ID: "a1", Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewAgentActionRepository(db).Save(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestAppWith(t, dir)
	startCore(t, a)
	if a.core.agent == nil || a.core.journal == nil {
		t.Fatal("agent core not wired")
	}
	got, err := sqlite.NewAgentThreadRepository(a.core.db).Get(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := got.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline || !turn.StepOpen() {
		t.Fatalf("request after start = %s %s open %v", turn.Status(), turn.StopReason(), turn.StepOpen())
	}
	entry, err := sqlite.NewAgentActionRepository(a.core.db).Get(ctx, "a1")
	if err != nil || entry.Status() != action.StatusUnknown {
		t.Fatalf("journal entry after start = %v, %v", entry, err)
	}
}
