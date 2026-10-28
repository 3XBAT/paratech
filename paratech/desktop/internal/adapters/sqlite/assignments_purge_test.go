package sqlite

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// seedPurgeLinks stores a link of every kind of assignment id: an export proposal, a SENT, a FAILED and (when
// unresolved is set) that delivery status, a draft with two versions, a work with a round.
func seedPurgeLinks(t *testing.T, db *DB, id string, unresolved string) {
	t.Helper()
	ms := jobMs(asgT0)
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO proposals (id, kind, status, assignment_id, created_at, updated_at)
		  VALUES (?, 'export_homework', 'PROPOSED', ?, ?, ?)`, []any{"p-" + id, id, ms, ms}},
		{`INSERT INTO homework_drafts (assignment_id, version) VALUES (?, 1)`, []any{id}},
		{`INSERT INTO homework_draft_revisions (assignment_id, number, origin, title, body, created_at)
		  VALUES (?, 1, 'edited', 'Черновик', 'Текст', ?), (?, 2, 'edited', 'Черновик', 'Текст 2', ?)`, []any{id, ms, id, ms}},
		{`INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
		  VALUES (?, ?, 'done', 1, 1, 1, ?, ?)`, []any{"w-" + id, id, ms, ms}},
		{`INSERT INTO work_rounds (work_id, round, answer) VALUES (?, 1, '{}')`, []any{"w-" + id}},
	}
	statuses := []string{"SENT", "FAILED"}
	if unresolved != "" {
		statuses = append(statuses, unresolved)
	}
	for i, s := range statuses {
		did := "d-" + id + "-" + s
		var sent any
		if s == "SENT" {
			sent = ms
		}
		stmts = append(stmts, struct {
			q    string
			args []any
		}{`INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id, idempotency_key, status,
		     attempts, confirmed_at, sent_at, version, created_at, updated_at)
		   VALUES (?, ?, 'teacher@example.test', 'ДЗ', ?, ?, ?, ?, 1, ?, ?, 1, ?, ?)`,
			[]any{did, id, "exports/" + id + "/homework.md", "<" + did + "@paratech.invalid>", "k-" + did, s, ms, sent, ms + int64(i), ms}})
	}
	for _, s := range stmts {
		if _, err := db.sql.ExecContext(context.Background(), s.q, s.args...); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

// purgeCounts is what is stored of assignment id: its row and every link.
func purgeCounts(t *testing.T, db *DB, id string) map[string]int64 {
	t.Helper()
	return map[string]int64{
		"assignments": scanInt(t, db.sql, "SELECT count(*) FROM assignments WHERE id = ?", id),
		"proposals":   scanInt(t, db.sql, "SELECT count(*) FROM proposals WHERE assignment_id = ?", id),
		"deliveries":  scanInt(t, db.sql, "SELECT count(*) FROM deliveries WHERE assignment_id = ?", id),
		"drafts":      scanInt(t, db.sql, "SELECT count(*) FROM homework_drafts WHERE assignment_id = ?", id),
		"revisions":   scanInt(t, db.sql, "SELECT count(*) FROM homework_draft_revisions WHERE assignment_id = ?", id),
		"works":       scanInt(t, db.sql, "SELECT count(*) FROM works WHERE assignment_id = ?", id),
		"rounds":      scanInt(t, db.sql, "SELECT count(*) FROM work_rounds WHERE work_id = ?", "w-"+id),
	}
}

func TestPurgeDeletedAssignmentQueries(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	deletedAt := asgT0.Add(time.Hour)
	for _, id := range []string{"s-old", "s-held", "s-new", "s-live"} {
		a := newByStudent(t, id)
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		if id != "s-live" {
			at := deletedAt
			if id == "s-new" {
				at = deletedAt.Add(2 * time.Hour)
			}
			if err := a.Delete(at); err != nil {
				t.Fatal(err)
			}
			if err := e.assignments.Save(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
		unresolved := ""
		if id == "s-held" {
			unresolved = "UNKNOWN"
		}
		seedPurgeLinks(t, e.db, id, unresolved)
	}
	cutoff := deletedAt.Add(time.Minute) // s-old and s-held are before it, s-new after

	ids := func(after time.Time, afterID shared.AssignmentID, limit int) []shared.AssignmentID {
		t.Helper()
		list, err := e.assignments.ListDeletedBefore(ctx, cutoff, after, afterID, limit)
		if err != nil {
			t.Fatal(err)
		}
		out := []shared.AssignmentID{}
		for _, a := range list {
			out = append(out, a.ID())
		}
		return out
	}
	if got := ids(time.Time{}, "", 10); !slices.Equal(got, []shared.AssignmentID{"s-held", "s-old"}) {
		t.Fatalf("ListDeletedBefore = %v, want [s-held s-old]", got)
	}
	if got := ids(time.Time{}, "", 1); !slices.Equal(got, []shared.AssignmentID{"s-held"}) {
		t.Fatalf("limit 1 = %v", got)
	}
	// The keyset: after s-held (same time, greater id) comes s-old, after s-old nothing; an earlier time lists both.
	if got := ids(deletedAt, "s-held", 10); !slices.Equal(got, []shared.AssignmentID{"s-old"}) {
		t.Fatalf("after s-held = %v, want [s-old]", got)
	}
	if got := ids(deletedAt, "s-old", 10); len(got) != 0 {
		t.Fatalf("after s-old = %v, want none", got)
	}
	if got := ids(deletedAt.Add(-time.Millisecond), "zzz", 10); len(got) != 2 {
		t.Fatalf("after an earlier time = %v, want both", got)
	}
	if _, err := e.assignments.ListDeletedBefore(ctx, cutoff, time.Time{}, "", 0); err == nil {
		t.Fatal("limit 0 accepted")
	}

	for id, want := range map[shared.AssignmentID]bool{"s-old": true, "s-held": true, "s-new": false, "s-live": false, "missing": false} {
		if got, err := e.assignments.StillDeletedBefore(ctx, id, cutoff); err != nil || got != want {
			t.Fatalf("StillDeletedBefore %s = %v, %v; want %v", id, got, err, want)
		}
	}

	// The row is refused while a draft or a delivery references it.
	if _, err := e.assignments.PurgeDeleted(ctx, "s-old", cutoff); !errors.Is(err, assignment.ErrInUse) {
		t.Fatalf("PurgeDeleted with links: err = %v, want ErrInUse", err)
	}

	proposals, deliveries := NewProposalRepository(e.db), NewDeliveryRepository(e.db)
	drafts, works := NewDraftRepository(e.db), NewWorkRepository(e.db)
	for _, id := range []shared.AssignmentID{"s-old", "s-held"} {
		if n, err := proposals.DeleteOfAssignment(ctx, id, cutoff); err != nil || n != 1 {
			t.Fatalf("proposals of %s = %d, %v", id, n, err)
		}
		if n, err := deliveries.DeleteFinalOfAssignment(ctx, id, cutoff); err != nil || n != 2 {
			t.Fatalf("final deliveries of %s = %d, %v; want SENT and FAILED", id, n, err)
		}
		if n, err := drafts.DeleteOfAssignment(ctx, id, cutoff); err != nil || n != 1 {
			t.Fatalf("draft of %s = %d, %v", id, n, err)
		}
		if n, err := works.DeleteOfAssignment(ctx, id, cutoff); err != nil || n != 1 {
			t.Fatalf("works of %s = %d, %v", id, n, err)
		}
	}

	// The UNKNOWN delivery stays and keeps the row (hard rule 7).
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM deliveries WHERE assignment_id = 's-held' AND status = 'UNKNOWN'"); n != 1 {
		t.Fatalf("unresolved deliveries left = %d, want 1", n)
	}
	if _, err := e.assignments.PurgeDeleted(ctx, "s-held", cutoff); !errors.Is(err, assignment.ErrInUse) {
		t.Fatalf("PurgeDeleted with an unresolved delivery: err = %v", err)
	}

	// The guard: a later delete, a live one and a missing one are not removed.
	for _, id := range []shared.AssignmentID{"s-new", "s-live", "missing"} {
		if done, err := e.assignments.PurgeDeleted(ctx, id, cutoff); err != nil || done {
			t.Fatalf("PurgeDeleted %s = %v, %v; want false", id, done, err)
		}
	}
	if done, err := e.assignments.PurgeDeleted(ctx, "s-old", cutoff); err != nil || !done {
		t.Fatalf("PurgeDeleted s-old = %v, %v", done, err)
	}
	for kind, n := range purgeCounts(t, e.db, "s-old") {
		if n != 0 {
			t.Errorf("s-old: %d rows left in %s", n, kind)
		}
	}
	// Repeats are no-ops.
	if done, err := e.assignments.PurgeDeleted(ctx, "s-old", cutoff); err != nil || done {
		t.Fatalf("repeat = %v, %v", done, err)
	}
	if n, err := proposals.DeleteOfAssignment(ctx, "s-old", cutoff); err != nil || n != 0 {
		t.Fatalf("repeat proposals = %d, %v", n, err)
	}
	if n, err := drafts.DeleteOfAssignment(ctx, "s-old", cutoff); err != nil || n != 0 {
		t.Fatalf("repeat draft = %d, %v", n, err)
	}

	// The guard of every delete: a later delete and a live one lose nothing.
	for _, id := range []shared.AssignmentID{"s-new", "s-live"} {
		for step, del := range map[string]func() (int, error){
			"proposals":  func() (int, error) { return proposals.DeleteOfAssignment(ctx, id, cutoff) },
			"deliveries": func() (int, error) { return deliveries.DeleteFinalOfAssignment(ctx, id, cutoff) },
			"draft":      func() (int, error) { return drafts.DeleteOfAssignment(ctx, id, cutoff) },
			"works":      func() (int, error) { return works.DeleteOfAssignment(ctx, id, cutoff) },
		} {
			if n, err := del(); err != nil || n != 0 {
				t.Fatalf("%s of %s = %d, %v; want nothing (not due)", step, id, n, err)
			}
		}
	}

	want := map[string]int64{
		"assignments": 1, "proposals": 1, "deliveries": 2, "drafts": 1, "revisions": 2, "works": 1, "rounds": 1,
	}
	for _, id := range []string{"s-new", "s-live"} {
		got := purgeCounts(t, e.db, id)
		for kind, n := range want {
			if got[kind] != n {
				t.Errorf("%s: %s = %d, want %d (untouched)", id, kind, got[kind], n)
			}
		}
	}
}

// A restore committed after the listing: the purge deletes, run in one transaction, remove nothing of it.
func TestPurgeDeletesKeepAnAssignmentRestoredAfterTheListing(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	deletedAt := asgT0.Add(time.Hour)
	a := newByStudent(t, "s1")
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(deletedAt); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	seedPurgeLinks(t, e.db, "s1", "")
	cutoff := deletedAt.Add(time.Minute)
	if list, err := e.assignments.ListDeletedBefore(ctx, cutoff, time.Time{}, "", 10); err != nil || len(list) != 1 {
		t.Fatalf("ListDeletedBefore = %v, %v", list, err)
	}
	before := purgeCounts(t, e.db, "s1")

	if err := a.Restore(deletedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}

	proposals, deliveries := NewProposalRepository(e.db), NewDeliveryRepository(e.db)
	drafts, works := NewDraftRepository(e.db), NewWorkRepository(e.db)
	err := e.db.Do(ctx, func(ctx context.Context) error {
		if due, err := e.assignments.StillDeletedBefore(ctx, "s1", cutoff); err != nil || due {
			t.Errorf("StillDeletedBefore after the restore = %v, %v", due, err)
		}
		for step, del := range map[string]func() (int, error){
			"proposals":  func() (int, error) { return proposals.DeleteOfAssignment(ctx, "s1", cutoff) },
			"deliveries": func() (int, error) { return deliveries.DeleteFinalOfAssignment(ctx, "s1", cutoff) },
			"draft":      func() (int, error) { return drafts.DeleteOfAssignment(ctx, "s1", cutoff) },
			"works":      func() (int, error) { return works.DeleteOfAssignment(ctx, "s1", cutoff) },
		} {
			if n, err := del(); err != nil || n != 0 {
				t.Errorf("%s = %d, %v; want nothing removed", step, n, err)
			}
		}
		if done, err := e.assignments.PurgeDeleted(ctx, "s1", cutoff); err != nil || done {
			t.Errorf("PurgeDeleted = %v, %v; want false", done, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after := purgeCounts(t, e.db, "s1")
	for kind, n := range before {
		if after[kind] != n {
			t.Errorf("%s = %d, was %d", kind, after[kind], n)
		}
	}
}

// The cutoff rounds up to whole milliseconds, so the query agrees with assignment.PurgeDue for a stored time.
func TestCutoffMs(t *testing.T) {
	base := time.UnixMilli(1_800_000_000_000)
	for _, tc := range []struct {
		cutoff time.Time
		want   int64
	}{
		{base, base.UnixMilli()},
		{base.Add(time.Nanosecond), base.UnixMilli() + 1},
		{base.Add(999 * time.Microsecond), base.UnixMilli() + 1},
	} {
		if got := cutoffMs(tc.cutoff); !got.Valid || got.Int64 != tc.want {
			t.Errorf("cutoffMs(%v) = %v, want %d", tc.cutoff, got, tc.want)
		}
	}
	if cutoffMs(time.Time{}).Valid {
		t.Error("a zero cutoff is not NULL")
	}
}
