package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// A soft-deleted assignment (ADR-0029) stores deleted_at, reads back the same by id and by source (the tombstone of
// reprocessing) and is skipped by every list; «Вернуть» clears the column again.
func TestDeletedAssignmentRoundtripAndReaders(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	kept, gone := newExtracted(t, "a1", "t1", 0, time.Time{}), newExtracted(t, "a2", "t1", 1, time.Time{})
	mine := newByStudent(t, "s-a3")
	for _, a := range []*assignment.Assignment{kept, gone, mine} {
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	deletedAt := asgT0.Add(time.Hour)
	for _, a := range []*assignment.Assignment{gone, mine} {
		if err := a.Delete(deletedAt); err != nil {
			t.Fatal(err)
		}
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		if a.Version() != 2 {
			t.Fatalf("version after the delete = %d, want 2", a.Version())
		}
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE deleted_at = ?", jobMs(deletedAt)); n != 2 {
		t.Fatalf("rows with deleted_at = %d, want 2", n)
	}

	got, err := e.assignments.Get(ctx, "a2")
	if err != nil {
		t.Fatal(err)
	}
	if got.State() != gone.State() || !got.Deleted() {
		t.Fatalf("Get = %+v, want %+v", got.State(), gone.State())
	}
	if src, err := e.assignments.GetBySource(ctx, "t1", 1); err != nil || !src.Deleted() {
		t.Fatalf("GetBySource of the tombstone = %v, %v", src, err)
	}

	byTranscript, err := e.assignments.ListByTranscript(ctx, "t1")
	if err != nil || len(byTranscript) != 1 || byTranscript[0].ID() != "a1" {
		t.Fatalf("ListByTranscript = %v, %v; want a1 only", byTranscript, err)
	}
	recent, err := e.assignments.ListRecent(ctx, 10)
	if err != nil || len(recent) != 1 || recent[0].ID() != "a1" {
		t.Fatalf("ListRecent = %v, %v; want a1 only", recent, err)
	}

	if err := got.Restore(asgT0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE id = 'a2' AND deleted_at IS NULL AND version = 3"); n != 1 {
		t.Fatal("the restore did not clear deleted_at")
	}
	if list, err := e.assignments.ListByTranscript(ctx, "t1"); err != nil || len(list) != 2 {
		t.Fatalf("ListByTranscript after the restore = %v, %v", list, err)
	}
}

// A soft-deleted assignment found in a material is read back by its source (the tombstone of a new homework search)
// and skipped by ListByMaterial.
func TestDeletedMaterialAssignment(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewMaterialRepository(db).Save(ctx, newLibMaterial(t, "m1", "текст")); err != nil {
		t.Fatal(err)
	}
	repo := NewAssignmentRepository(db)
	var stored []*assignment.Assignment
	for i, id := range []shared.AssignmentID{"am0", "am1"} {
		d, err := assignment.NewDetails("Задача "+string(id), "", time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		a, err := assignment.NewExtracted(assignment.Spec{
			ID: id, Origin: assignment.Origin{Material: "m1", Index: i}, PromptVersion: 1, Details: d,
		}, libT0)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, a)
	}
	gone := stored[0]
	if err := gone.Delete(libT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, gone); err != nil {
		t.Fatal(err)
	}

	if src, err := repo.GetByMaterialSource(ctx, "m1", 0); err != nil || !src.Deleted() || src.State() != gone.State() {
		t.Fatalf("GetByMaterialSource of the tombstone = %v, %v", src, err)
	}
	list, err := repo.ListByMaterial(ctx, "m1")
	if err != nil || len(list) != 1 || list[0].ID() != "am1" {
		t.Fatalf("ListByMaterial = %v, %v; want am1 only", list, err)
	}
}

func TestAssignmentBusy(t *testing.T) {
	ctx := context.Background()
	delivery := func(status string) string {
		return `INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id, idempotency_key,
			status, confirmed_at, sent_at, created_at, updated_at)
			VALUES ('d1', 'a1', 'student@example.invalid', 's', 'exports/x', 'msg-1', 'dk-1', '` + status + `', 0, 1, 0, 0)`
	}
	work := func(status string) string {
		return `INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
			VALUES ('w1', 'a1', '` + status + `', 1, 1, 1, 0, 0)`
	}
	job := func(typ, status, assignmentID string) string {
		finished := "NULL"
		if status == "FAILED" {
			finished = "1"
		}
		return `INSERT INTO jobs (id, type, status, idempotency_key, payload, max_attempts, next_run_at, created_at,
			finished_at, version, updated_at) VALUES ('j1', '` + typ + `', '` + status + `', 'k1',
			'{"assignment_id":"` + assignmentID + `"}', 3, 0, 0, ` + finished + `, 1, 0)`
	}
	// Busy (the delete is refused) is only an email that may still go out or may have gone out unseen; a work or a job
	// is what the delete stops (Working, ActiveJobs; TASK-338).
	cases := []struct {
		name    string
		insert  string // "" : nothing references the assignment
		busy    bool
		working bool
		jobs    []shared.JobID
	}{
		{name: "nothing"},
		{name: "a pending delivery", insert: delivery("PENDING"), busy: true},
		{name: "a sending delivery", insert: delivery("SENDING"), busy: true},
		{name: "a delivery with an unknown outcome", insert: delivery("UNKNOWN"), busy: true},
		{name: "a sent delivery", insert: delivery("SENT")},
		{name: "a failed delivery", insert: delivery("FAILED")},
		{name: "a work in triage", insert: work("triage"), working: true},
		{name: "a work being written", insert: work("writing"), working: true},
		{name: "a work waiting for answers", insert: work("questions")},
		{name: "a done work", insert: work("done")},
		{name: "a queued draft generation", insert: job("homework_draft", "QUEUED", "a1"), working: true, jobs: []shared.JobID{"j1"}},
		{name: "a draft generation waiting for the network", insert: job("homework_draft", "WAITING_FOR_NETWORK", "a1"),
			working: true, jobs: []shared.JobID{"j1"}},
		{name: "a running writing job", insert: job("work_writing", "RUNNING", "a1"), working: true, jobs: []shared.JobID{"j1"}},
		{name: "a failed draft generation", insert: job("homework_draft", "FAILED", "a1")},
		{name: "a draft generation of another assignment", insert: job("homework_draft", "RUNNING", "a2")},
		{name: "another job type naming it", insert: job("summarize", "RUNNING", "a1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCourseworkEnv(t)
			if err := e.assignments.Save(ctx, newByStudent(t, "a1")); err != nil {
				t.Fatal(err)
			}
			if tc.insert != "" {
				mustExec(t, e.db, tc.insert)
			}
			busy, err := e.assignments.Busy(ctx, "a1")
			if err != nil || busy != tc.busy {
				t.Fatalf("Busy = %v, %v; want %v", busy, err, tc.busy)
			}
			working, err := e.assignments.Working(ctx, "a1")
			if err != nil || working != tc.working {
				t.Fatalf("Working = %v, %v; want %v", working, err, tc.working)
			}
			jobs, err := e.assignments.ActiveJobs(ctx, "a1")
			if err != nil || !slices.Equal(jobs, tc.jobs) {
				t.Fatalf("ActiveJobs = %v, %v; want %v", jobs, err, tc.jobs)
			}
		})
	}
}

// ActiveJobs lists every unfinished draft and writing job of the assignment, oldest first, and no finished one.
func TestAssignmentActiveJobsOrder(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if err := e.assignments.Save(ctx, newByStudent(t, "a1")); err != nil {
		t.Fatal(err)
	}
	for _, j := range []struct {
		id, typ, status, finished string
		created                   int
	}{
		{"j-late", "work_writing", "RETRY_WAIT", "NULL", 30},
		{"j-early", "homework_draft", "RUNNING", "NULL", 10},
		{"j-done", "work_writing", "CANCELLED", "20", 20},
	} {
		mustExec(t, e.db, `INSERT INTO jobs (id, type, status, idempotency_key, payload, max_attempts, next_run_at,
			created_at, finished_at, version, updated_at) VALUES (?, ?, ?, ?, '{"assignment_id":"a1"}', 3, 0, ?, `+
			j.finished+`, 1, 0)`, j.id, j.typ, j.status, "k-"+j.id, j.created)
	}
	jobs, err := e.assignments.ActiveJobs(ctx, "a1")
	if err != nil || !slices.Equal(jobs, []shared.JobID{"j-early", "j-late"}) {
		t.Fatalf("ActiveJobs = %v, %v; want j-early, j-late", jobs, err)
	}
}
