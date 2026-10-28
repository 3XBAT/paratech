package tests

// «Удалить» of an assignment whose work is being written (TASK-338) on a SQLite file: the delete goes through (only an
// unresolved email refuses it), the activity port finds the writing job, and once that job is cancelled the purge after
// the restore window still removes the assignment with its work; the finished job row stays (TASK-274 follow-up).

import (
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/shared"
)

func TestDeleteOfAWorkingAssignmentThenPurgeOnSQLite(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	files, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	assignments := sqlite.NewAssignmentRepository(e.db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.db.DBTX(ctx).ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}

	v, err := e.homework.CreateAssignment(ctx, homeworkflow.NewAssignment{Title: "Реферат"})
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
		VALUES ('w-1', ?1, 'writing', 1, 1, 1, 1, 1)`, string(v.ID))
	exec(`INSERT INTO jobs (id, type, status, idempotency_key, payload, max_attempts, next_run_at, created_at, version, updated_at)
		VALUES ('j-write', 'work_writing', 'QUEUED', 'work_writing:w-1:g1', json_object('work_id', 'w-1', 'assignment_id', ?1),
		3, 0, 1, 1, 1)`, string(v.ID))

	if working, err := assignments.Working(ctx, v.ID); err != nil || !working {
		t.Fatalf("Working = %v, %v; want true", working, err)
	}
	jobs, err := assignments.ActiveJobs(ctx, v.ID)
	if err != nil || !slices.Equal(jobs, []shared.JobID{"j-write"}) {
		t.Fatalf("ActiveJobs = %v, %v; want j-write", jobs, err)
	}

	deleted, err := e.homework.DeleteAssignment(ctx, v.ID, v.Version)
	if err != nil {
		t.Fatalf("DeleteAssignment of a working assignment: %v", err)
	}
	// What the runner's Cancel stores for a queued job.
	exec(`UPDATE jobs SET status = 'CANCELLED', finished_at = 2, version = version + 1 WHERE id = 'j-write'`)
	if jobs, err := assignments.ActiveJobs(ctx, v.ID); err != nil || len(jobs) != 0 {
		t.Fatalf("ActiveJobs after the cancel = %v, %v; want none", jobs, err)
	}
	if !deleted.Deleted {
		t.Fatalf("view = %+v, want deleted", deleted)
	}

	e.clock.Advance(3*24*time.Hour + time.Minute)
	purger, err := homeworkflow.NewPurger(homeworkflow.PurgeDeps{
		Assignments: assignments, Proposals: sqlite.NewProposalRepository(e.db),
		Deliveries: sqlite.NewDeliveryRepository(e.db), Drafts: sqlite.NewDraftRepository(e.db),
		Works: sqlite.NewWorkRepository(e.db), Files: files, Clock: e.clock, Tx: e.db,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := purger.PurgeDeleted(ctx); err != nil || n != 1 {
		t.Fatalf("PurgeDeleted = %d, %v; want 1, nil", n, err)
	}
	left := mentions(t, e.db, v.ID)
	if left["assignments"] != 0 || left["works"] != 0 || left["jobs"] != 1 {
		t.Fatalf("rows naming the purged assignment = %v, want the cancelled job only", left)
	}
}
