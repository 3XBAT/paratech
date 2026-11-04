package app

// The purge of the details of agent requests that ended 90 days ago (TASK-250, ADR-0028 «Retention») on the wired
// core: a startup cleanup after the recovery of the agent requests, then a loop once a day.

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestStartupPurgesOldAgentRequestDetails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	steps := make([]string, len(a.core.cleanups))
	for i, c := range a.core.cleanups {
		steps[i] = c.name
	}
	at, recover := slices.Index(steps, agentRetentionStep), slices.Index(steps, "agent requests left running")
	if at < 0 || recover < 0 || at < recover {
		t.Fatalf("cleanups %q: the retention step must follow the recovery of the agent requests", steps)
	}
	if !slices.ContainsFunc(a.core.loops(), func(l loop) bool { return l.name == "agent history retention" && l.run != nil }) {
		t.Fatal("no daily agent history retention loop")
	}

	now := time.Now()
	old, recent := now.Add(-91*24*time.Hour).UnixMilli(), now.Add(-89*24*time.Hour).UnixMilli()
	execDB(t, dir, `INSERT INTO agent_threads (id, title, turns, history_from, seen, last_active_at, version, created_at, updated_at)
		VALUES ('th1', 'Чат', 3, 0, '[]', 1, 1, 1, 1)`)
	execDB(t, dir, `INSERT INTO agent_turns (thread_id, number, status, scope, started_at, updated_at) VALUES
		('th1', 1, 'DONE', '{}', 1, ?), ('th1', 2, 'DONE', '{}', 1, ?), ('th1', 3, 'DONE', '{}', 1, ?)`, old, recent, old)
	for turn := 1; turn <= 3; turn++ {
		execDB(t, dir, `INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at) VALUES
			('th1', ?, ?, 'student', '{"text":"просьба"}', 1),
			('th1', ?, ?, 'calls', '{"calls":[{"call_id":"r1","tool":"reply","arguments_json":"{\"text\":\"ответ\"}"}],"seal":"AQ=="}', 1),
			('th1', ?, ?, 'results', '{"results":[{"call_id":"r1","result_json":"{\"shown\":true}"}]}', 1)`,
			3*turn-2, turn, 3*turn-1, turn, 3*turn, turn)
	}

	life, ok := a.claimStart(false)
	if !ok {
		t.Fatal("no lifecycle to start")
	}
	if err := life.start(ctx); err != nil {
		t.Fatal(err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := a.core.db.DBTX(ctx).QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Request 1 ended 91 days ago: its details go, its text stays and its reply line is frozen. Request 2 (89 days)
	// and the current request 3 keep everything.
	for q, want := range map[string]int{
		"SELECT count(*) FROM agent_steps WHERE turn = 1":                                   1,
		"SELECT count(*) FROM agent_steps WHERE turn = 1 AND entry = 'student'":             1,
		"SELECT count(*) FROM agent_steps WHERE turn = 2":                                   3,
		"SELECT count(*) FROM agent_steps WHERE turn = 3":                                   3,
		"SELECT count(*) FROM agent_turns WHERE number = 1 AND reply_line = 'reply: ответ'": 1,
		"SELECT count(*) FROM agent_turns WHERE number IN (2, 3) AND reply_line IS NULL":    2,
	} {
		if n := count(q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
}
