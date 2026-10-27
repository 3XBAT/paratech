package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// deletionEnv is a COMPLETED recording with a transcript "t1" and what was made from it: two live parts, a summary, a
// lecture text, three found assignments ("a-plain", "a-draft" with a draft, "a-sent" with an email) and jobs (a
// cancelled stt job, a finished summary job, a running draft job of "a-draft"), next to an unrelated assignment.
type deletionEnv struct {
	db   *DB
	recs *RecordingRepository
	dels *RecordingDeletions
	jobs *JobRepository
	rec  *recording.Recording
}

const insertDeletionJob = `INSERT INTO jobs (id, type, status, idempotency_key, payload, max_attempts, next_run_at,
	created_at, finished_at, version, updated_at) VALUES (?, ?, ?, ?, ?, 3, 0, ?, ?, 1, 0)`

func newDeletionEnv(t *testing.T) *deletionEnv {
	t.Helper()
	db := openTest(t, nil)
	e := &deletionEnv{db: db, recs: NewRecordingRepository(db), dels: NewRecordingDeletions(db), jobs: NewJobRepository(db)}
	e.rec = recordingInStatus(t, recording.StatusCompleted)
	saveRecording(t, e.recs, e.rec)
	id := string(e.rec.ID())

	mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_name, model_version, text_ref, version, created_at)
		VALUES ('t1', ?, 'base', '1', 'transcripts/t1.json', 1, 0)`, id)
	mustExec(t, db, `INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text)
		VALUES (?, 0, 0, 10, 'a'), (?, 1, 10, 20, 'b')`, id, id)
	mustExec(t, db, `INSERT INTO summaries (id, transcript_id, body, prompt_version, version, created_at, updated_at)
		VALUES ('t1', 't1', 'summary', 1, 1, 0, 0)`)
	mustExec(t, db, `INSERT INTO lecture_texts (id, transcript_id, version, created_at, updated_at)
		VALUES ('t1', 't1', 1, 0, 0)`)
	for i, a := range []string{"a-plain", "a-draft", "a-sent"} {
		mustExec(t, db, `INSERT INTO assignments (id, transcript_id, title, source_index, version, created_at, updated_at)
			VALUES (?, 't1', ?, ?, 1, 0, 0)`, a, "hw "+a, i)
	}
	mustExec(t, db, `INSERT INTO assignments (id, title, version, created_at, updated_at) VALUES ('a-other', 'other', 1, 0, 0)`)
	mustExec(t, db, "INSERT INTO homework_drafts (assignment_id, version) VALUES ('a-draft', 1)")
	mustExec(t, db, `INSERT INTO homework_draft_revisions (assignment_id, number, origin, title, body, created_at)
		VALUES ('a-draft', 1, 'edited', 'title', 'body', 0)`)
	mustExec(t, db, `INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id,
		idempotency_key, status, confirmed_at, sent_at, created_at, updated_at)
		VALUES ('d1', 'a-sent', 'a@b.c', 's', 'exports/x', 'msg-1', 'dk-1', 'SENT', 0, 0, 0, 0)`)

	mustExec(t, db, insertDeletionJob, "j-stt", "stt", "CANCELLED", "k-stt", `{"recording_id":"`+id+`"}`, 1, 5)
	mustExec(t, db, insertDeletionJob, "j-sum", "summarize", "COMPLETED", "k-sum", `{"transcript_id":"t1"}`, 2, 5)
	mustExec(t, db, insertDeletionJob, "j-draft", "homework_draft", "RUNNING", "k-draft",
		`{"assignment_id":"a-draft","base":0,"instruction":""}`, 3, nil)
	mustExec(t, db, "UPDATE jobs SET attempts = 1, started_at = 3 WHERE id = 'j-draft'") // a RUNNING job has started
	mustExec(t, db, insertDeletionJob, "j-other", "homework_draft", "COMPLETED", "k-other",
		`{"assignment_id":"a-other","base":0,"instruction":""}`, 4, 5)
	return e
}

func TestRecordingDeletionMarkRoundTripAndLists(t *testing.T) {
	e, ctx := newDeletionEnv(t), context.Background()
	saveRecording(t, e.recs, newTestRecording(t, "r-live", ""))

	if err := e.rec.MarkDeleting(recT0.Add(time.Hour), []shared.AssignmentID{"a-plain", "a-draft"}); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, e.recs, e.rec)
	got, err := e.recs.Get(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, e.rec) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, e.rec)
	}

	deleting, err := e.recs.ListDeleting(ctx)
	if err != nil || !reflect.DeepEqual(deleting, []shared.RecordingID{e.rec.ID()}) {
		t.Fatalf("ListDeleting = %v, %v", deleting, err)
	}
	listed, _, err := e.recs.ListActivity(ctx, 10)
	if err != nil || len(listed) != 1 || listed[0].ID() != "r-live" {
		t.Fatalf("ListActivity = %v, %v; want only the live recording", listed, err)
	}
	mustExec(t, e.db, "DELETE FROM transcripts")
	if ids, err := e.recs.ListUntranscribed(ctx, "", 10); err != nil || len(ids) != 0 {
		t.Fatalf("ListUntranscribed = %v, %v; want the marked recording left out", ids, err)
	}
}

func TestRecordingDeletionContents(t *testing.T) {
	e, ctx := newDeletionEnv(t), context.Background()
	got, err := e.dels.Contents(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	want := deletion.Contents{Transcripts: 1, Parts: 2, Summaries: 1, LectureTexts: 1, Assignments: []deletion.Assignment{
		{ID: "a-plain", Title: "hw a-plain"}, {ID: "a-draft", Title: "hw a-draft", HasDraft: true},
		{ID: "a-sent", Title: "hw a-sent", Deliveries: 1},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Contents =\n %+v\nwant\n %+v", got, want)
	}

	files, err := e.dels.Files(ctx, e.rec.ID())
	if err != nil || len(files) != 1 || files[0].String() != "transcripts/t1.json" {
		t.Fatalf("Files = %v, %v", files, err)
	}

	empty, err := e.dels.Contents(ctx, "nothing")
	if err != nil || empty.Transcripts != 0 || len(empty.Assignments) != 0 {
		t.Fatalf("Contents of an unknown recording = %+v, %v", empty, err)
	}
}

func TestJobsForAssignments(t *testing.T) {
	e, ctx := newDeletionEnv(t), context.Background()
	got, bad, err := e.jobs.ListForAssignments(ctx, []shared.AssignmentID{"a-draft", "a-plain"}, []job.Type{"homework_draft"})
	if err != nil || len(bad) != 0 || len(got) != 1 || got[0].ID() != "j-draft" {
		t.Fatalf("ListForAssignments = %v, %v, %v; want j-draft", got, bad, err)
	}
	none, _, err := e.jobs.ListForAssignments(ctx, []shared.AssignmentID{"a-draft"}, []job.Type{"summarize"})
	if err != nil || len(none) != 0 {
		t.Fatalf("ListForAssignments of another type = %v, %v", none, err)
	}
}

func TestRecordingDeletionPurge(t *testing.T) {
	e, ctx := newDeletionEnv(t), context.Background()
	err := e.dels.Purge(ctx, e.rec, []shared.AssignmentID{"a-plain", "a-draft"},
		[]shared.JobID{"j-stt", "j-sum", "j-draft"}, recT0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	counts := []struct {
		query string
		want  int64
	}{
		{"SELECT count(*) FROM recordings", 0},
		{"SELECT count(*) FROM transcripts", 0},
		{"SELECT count(*) FROM transcript_parts", 0},
		{"SELECT count(*) FROM summaries", 0},
		{"SELECT count(*) FROM lecture_texts", 0},
		{"SELECT count(*) FROM homework_drafts", 0},
		{"SELECT count(*) FROM homework_draft_revisions", 0},
		{"SELECT count(*) FROM assignments WHERE id IN ('a-plain', 'a-draft')", 0},
		{"SELECT count(*) FROM assignments WHERE id = 'a-sent' AND transcript_id IS NULL", 1}, // detached, email kept
		{"SELECT count(*) FROM assignments WHERE id = 'a-other'", 1},
		{"SELECT count(*) FROM deliveries", 1},
		{"SELECT count(*) FROM jobs WHERE id IN ('j-stt', 'j-sum')", 0},
		{"SELECT count(*) FROM jobs WHERE id IN ('j-draft', 'j-other')", 2}, // a running job is never deleted
	}
	for _, c := range counts {
		if n := scanInt(t, e.db.sql, c.query); n != c.want {
			t.Errorf("%s = %d, want %d", c.query, n, c.want)
		}
	}

	removals := NewDeadlineEventRemovals(e.db)
	ids, err := removals.ListDeadlineRemovals(ctx, 10)
	if err != nil || len(ids) != 2 {
		t.Fatalf("ListDeadlineRemovals = %v, %v; want both deleted assignments", ids, err)
	}
	if err := removals.PurgeDeadlineRemoval(ctx, "a-plain"); err != nil {
		t.Fatal(err)
	}
	if err := removals.PurgeDeadlineRemoval(ctx, "a-plain"); err != nil {
		t.Fatalf("purging a removal again: %v", err)
	}
	ids, err = removals.ListDeadlineRemovals(ctx, 10)
	if err != nil || !reflect.DeepEqual(ids, []shared.AssignmentID{"a-draft"}) {
		t.Fatalf("ListDeadlineRemovals after a purge = %v, %v", ids, err)
	}
	if ids, err = removals.ListDeadlineRemovals(ctx, 0); err != nil || len(ids) != 0 {
		t.Fatalf("ListDeadlineRemovals(0) = %v, %v", ids, err)
	}
}

// An assignment of the recording the student deleted (ADR-0029) is not offered with it and is not purged: it stays,
// detached from the transcript, and no removal is queued for it by the purge.
func TestRecordingDeletionKeepsADeletedAssignment(t *testing.T) {
	e, ctx := newDeletionEnv(t), context.Background()
	mustExec(t, e.db, "UPDATE assignments SET deleted_at = 1, version = 2 WHERE id = 'a-plain'")

	got, err := e.dels.Contents(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assignments) != 2 || got.Assignments[0].ID != "a-draft" || got.Assignments[1].ID != "a-sent" {
		t.Fatalf("assignments offered = %+v, want a-draft and a-sent", got.Assignments)
	}

	// Purge removes what the use case chose from the contents; the deleted one is never among them.
	if err := e.dels.Purge(ctx, e.rec, []shared.AssignmentID{"a-draft"}, []shared.JobID{"j-stt", "j-sum", "j-draft"},
		recT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		query string
		want  int64
	}{
		{"SELECT count(*) FROM assignments WHERE id = 'a-plain' AND transcript_id IS NULL AND deleted_at = 1", 1},
		{"SELECT count(*) FROM deadline_event_removals WHERE assignment_id = 'a-plain'", 0},
		{"SELECT count(*) FROM transcripts", 0},
	} {
		if n := scanInt(t, e.db.sql, c.query); n != c.want {
			t.Errorf("%s = %d, want %d", c.query, n, c.want)
		}
	}
}

// The soft delete's queue: a second queue keeps the first time, the row goes with a rolled-back transaction, and a
// purge of a missing row is no error.
func TestQueueDeadlineRemoval(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	removals := NewDeadlineEventRemovals(db)
	at := recT0.Add(time.Hour)

	if err := removals.QueueDeadlineRemoval(ctx, "a1", at); err != nil {
		t.Fatal(err)
	}
	if err := removals.QueueDeadlineRemoval(ctx, "a1", at.Add(time.Hour)); err != nil {
		t.Fatalf("second queue: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_removals WHERE assignment_id = 'a1' AND created_at = ?",
		at.UnixMilli()); n != 1 {
		t.Fatalf("queued rows with the first time = %d, want 1", n)
	}

	rollback := errors.New("rolled back")
	err := db.Do(ctx, func(ctx context.Context) error {
		if err := removals.QueueDeadlineRemoval(ctx, "a2", at); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Do = %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_removals WHERE assignment_id = 'a2'"); n != 0 {
		t.Fatalf("a rolled-back queue left %d rows", n)
	}

	for range 2 {
		if err := removals.PurgeDeadlineRemoval(ctx, "a1"); err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := removals.ListDeadlineRemovals(ctx, 10); err != nil || len(ids) != 0 {
		t.Fatalf("removals after the purge = %v, %v", ids, err)
	}
}

func TestRecordingDeletionPurgeIsAllOrNothing(t *testing.T) {
	cases := []struct {
		name        string
		stale       bool
		assignments []shared.AssignmentID
		want        error
	}{
		{"stale recording", true, []shared.AssignmentID{"a-plain"}, shared.ErrVersionConflict},
		{"assignment with an email", false, []shared.AssignmentID{"a-plain", "a-sent"}, shared.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, ctx := newDeletionEnv(t), context.Background()
			if tc.stale {
				fresh, err := e.recs.Get(ctx, e.rec.ID())
				if err != nil {
					t.Fatal(err)
				}
				if err := fresh.MarkDeleting(recT0.Add(time.Hour), nil); err != nil {
					t.Fatal(err)
				}
				saveRecording(t, e.recs, fresh)
			}
			err := e.dels.Purge(ctx, e.rec, tc.assignments, []shared.JobID{"j-stt"}, recT0)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Purge = %v, want %v", err, tc.want)
			}
			for _, q := range []string{
				"SELECT count(*) FROM recordings", "SELECT count(*) FROM transcripts", "SELECT count(*) FROM summaries",
				"SELECT count(*) FROM assignments WHERE id = 'a-plain'", "SELECT count(*) FROM jobs WHERE id = 'j-stt'",
			} {
				if n := scanInt(t, e.db.sql, q); n != 1 {
					t.Errorf("%s = %d after a refused purge, want 1", q, n)
				}
			}
			if n := scanInt(t, e.db.sql, "SELECT count(*) FROM deadline_event_removals"); n != 0 {
				t.Errorf("deadline removals after a refused purge = %d", n)
			}
		})
	}
}
