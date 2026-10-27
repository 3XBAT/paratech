package sqlite

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
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
	cases := []struct {
		name   string
		insert string // "" : nothing references the assignment
		want   bool
	}{
		{"nothing", "", false},
		{"a pending delivery", delivery("PENDING"), true},
		{"a sending delivery", delivery("SENDING"), true},
		{"a delivery with an unknown outcome", delivery("UNKNOWN"), true},
		{"a sent delivery", delivery("SENT"), false},
		{"a failed delivery", delivery("FAILED"), false},
		{"a work in triage", work("triage"), true},
		{"a work being written", work("writing"), true},
		{"a work waiting for answers", work("questions"), false},
		{"a done work", work("done"), false},
		{"a queued draft generation", job("homework_draft", "QUEUED", "a1"), true},
		{"a draft generation waiting for the network", job("homework_draft", "WAITING_FOR_NETWORK", "a1"), true},
		{"a running writing job", job("work_writing", "RUNNING", "a1"), true},
		{"a failed draft generation", job("homework_draft", "FAILED", "a1"), false},
		{"a draft generation of another assignment", job("homework_draft", "RUNNING", "a2"), false},
		{"another job type naming it", job("summarize", "RUNNING", "a1"), false},
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
			if err != nil || busy != tc.want {
				t.Fatalf("Busy = %v, %v; want %v", busy, err, tc.want)
			}
		})
	}
}
