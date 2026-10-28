package app

// The purge of deleted assignments past the restore window (TASK-274, ADR-0029 addendum 2) on the wired core: a
// startup cleanup, then a loop once a day on the clock of the use cases.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// countingPurge counts its runs and fails while err is set.
type countingPurge struct {
	mu  sync.Mutex
	n   int
	err error
}

func (p *countingPurge) run(context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	return 1, p.err
}

func (p *countingPurge) runs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func TestPurgeLoopRunsOnceADay(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC))
	purge := &countingPurge{err: errors.New("disk busy")}
	var logs bytes.Buffer
	run := purgeLoop(clock, purge.run, slog.New(slog.NewTextHandler(&syncWriter{w: &logs}, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()

	await := func() {
		t.Helper()
		if err := clock.AwaitSleepers(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	await()
	if purge.runs() != 0 {
		t.Fatal("the loop ran before the first day passed (the start runs the cleanup)")
	}
	clock.Advance(purgeInterval - time.Minute)
	await()
	if purge.runs() != 0 {
		t.Fatal("ran before a day")
	}

	clock.Advance(time.Minute)
	testenv.Eventually(t, "the daily run", func() bool { return purge.runs() == 1 }, nil)
	await() // a failed run does not end the loop

	purge.mu.Lock()
	purge.err = nil
	purge.mu.Unlock()
	clock.Advance(purgeInterval)
	testenv.Eventually(t, "the second daily run", func() bool { return purge.runs() == 2 }, nil)
	await()

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("loop ended with %v", err)
	}
	if s := logs.String(); !strings.Contains(s, "daily cleanup failed") || !strings.Contains(s, "disk busy") {
		t.Fatalf("the failure is not logged: %q", s)
	}
}

// syncWriter guards a buffer written by the loop and read by the test.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// execDB runs statements on the database of dataDir through its own connection.
func execDB(t *testing.T, dataDir string, q string, args ...any) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dataDir, dbFile))+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// The start purges an assignment deleted longer than the window, with its proposal, its sent email and its export
// file; one deleted an hour ago and a live one stay. The daily loop is wired.
func TestStartupPurgesDeletedAssignments(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(a.core.cleanups, func(c cleanup) bool { return c.name == purgeStep }) {
		t.Fatal("no startup cleanup of the purge")
	}
	if !slices.ContainsFunc(a.core.loops(), func(l loop) bool { return l.name == "assignment purge" && l.run != nil }) {
		t.Fatal("no daily purge loop")
	}

	now := time.Now()
	old := now.Add(-assignment.RestoreWindow - time.Hour).UnixMilli()
	recent := now.Add(-time.Hour).UnixMilli()
	for _, id := range []shared.AssignmentID{"a-old", "a-recent", "a-live"} {
		insertAssignment(t, dir, id, "Задание "+string(id))
		execDB(t, dir, `INSERT INTO proposals (id, kind, status, assignment_id, created_at, updated_at)
			VALUES (?, 'export_homework', 'PROPOSED', ?, 1, 1)`, "p-"+string(id), string(id))
		execDB(t, dir, `INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id,
			idempotency_key, status, attempts, confirmed_at, sent_at, version, created_at, updated_at, attachment_sha256,
			attachment_size)
			VALUES (?, ?, 'teacher@example.test', 'ДЗ', ?, ?, ?, 'SENT', 1, 1, 1, 1, 1, 1, ?, 4)`,
			"d-"+string(id), string(id), "exports/"+string(id)+"/homework.md", "<d-"+string(id)+"@paratech.invalid>",
			"k-"+string(id), strings.Repeat("a", 64))
		ref, err := proposal.ExportRef(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.core.store.Write(ctx, ref, strings.NewReader("ДЗ")); err != nil {
			t.Fatal(err)
		}
	}
	execDB(t, dir, `UPDATE assignments SET deleted_at = ? WHERE id = 'a-old'`, old)
	execDB(t, dir, `UPDATE assignments SET deleted_at = ? WHERE id = 'a-recent'`, recent)

	life, ok := a.claimStart(false)
	if !ok {
		t.Fatal("no lifecycle to start")
	}
	if err := life.start(ctx); err != nil {
		t.Fatal(err)
	}

	count := func(q, id string) int {
		t.Helper()
		var n int
		if err := a.core.db.DBTX(ctx).QueryRowContext(ctx, q, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, tc := range []struct {
		id   string
		want int
	}{{"a-old", 0}, {"a-recent", 1}, {"a-live", 1}} {
		for _, q := range []string{
			"SELECT count(*) FROM assignments WHERE id = ?",
			"SELECT count(*) FROM proposals WHERE assignment_id = ?",
			"SELECT count(*) FROM deliveries WHERE assignment_id = ?",
		} {
			if n := count(q, tc.id); n != tc.want {
				t.Errorf("%s: %q = %d, want %d", tc.id, q, n, tc.want)
			}
		}
		ref, err := proposal.ExportRef(shared.AssignmentID(tc.id))
		if err != nil {
			t.Fatal(err)
		}
		rc, err := a.core.store.Open(ctx, ref)
		if err == nil {
			_ = rc.Close()
		}
		if gone := errors.Is(err, shared.ErrNotFound); gone != (tc.want == 0) {
			t.Errorf("%s: export file gone = %v (err %v)", tc.id, gone, err)
		}
	}
}
