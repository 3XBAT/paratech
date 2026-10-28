package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// «Выполнено» (ADR-0033 §9) stores done_at with the version compare-and-swap, reads back the same by every loader and
// is cleared by MarkUndone.
func TestAssignmentDoneRoundtrip(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	extracted, mine := newExtracted(t, "a1", "t1", 0, time.Time{}), newByStudent(t, "s-a2")
	for _, a := range []*assignment.Assignment{extracted, mine} {
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE done_at IS NULL"); n != 2 {
		t.Fatalf("rows without done_at = %d, want 2", n)
	}

	doneAt := asgT0.Add(time.Hour)
	for _, a := range []*assignment.Assignment{extracted, mine} {
		if changed, err := a.MarkDone(doneAt); err != nil || !changed {
			t.Fatalf("MarkDone = %v, %v", changed, err)
		}
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		if a.Version() != 2 {
			t.Fatalf("version after MarkDone = %d, want 2", a.Version())
		}
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE done_at = ?", jobMs(doneAt)); n != 2 {
		t.Fatalf("rows with done_at = %d, want 2", n)
	}

	got, err := e.assignments.Get(ctx, "a1")
	if err != nil || got.State() != extracted.State() || !got.Done() {
		t.Fatalf("Get = %+v, %v; want %+v", got, err, extracted.State())
	}
	if src, err := e.assignments.GetBySource(ctx, "t1", 0); err != nil || !src.DoneAt().Equal(doneAt) {
		t.Fatalf("GetBySource = %v, %v", src, err)
	}
	// Readers stay as they are (TASK-314): a done assignment is still listed, with its mark.
	recent, err := e.assignments.ListRecent(ctx, 10)
	if err != nil || len(recent) != 2 || !recent[0].Done() || !recent[1].Done() {
		t.Fatalf("ListRecent = %v, %v; want both, done", recent, err)
	}
	if list, err := e.assignments.ListByTranscript(ctx, "t1"); err != nil || len(list) != 1 || !list[0].Done() {
		t.Fatalf("ListByTranscript = %v, %v", list, err)
	}

	// A deleted done assignment keeps the mark through the purge reader.
	if err := mine.Delete(doneAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, mine); err != nil {
		t.Fatal(err)
	}
	deleted, err := e.assignments.ListDeletedBefore(ctx, doneAt.Add(time.Hour), time.Time{}, "", 10)
	if err != nil || len(deleted) != 1 || deleted[0].State() != mine.State() || !deleted[0].Done() {
		t.Fatalf("ListDeletedBefore = %v, %v", deleted, err)
	}

	if changed, err := got.MarkUndone(doneAt.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("MarkUndone = %v, %v", changed, err)
	}
	if err := e.assignments.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE id = 'a1' AND done_at IS NULL AND version = 3"); n != 1 {
		t.Fatal("MarkUndone did not clear done_at")
	}
}

// A mark saved over a stale version is a version conflict and leaves the row as the other writer stored it.
func TestAssignmentDoneVersionConflict(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if err := e.assignments.Save(ctx, newByStudent(t, "s-a1")); err != nil {
		t.Fatal(err)
	}
	first, err := e.assignments.Get(ctx, "s-a1")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := e.assignments.Get(ctx, "s-a1")
	if err != nil {
		t.Fatal(err)
	}

	doneAt := asgT0.Add(time.Hour)
	if _, err := first.MarkDone(doneAt); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := stale.MarkDone(doneAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
	}

	got, err := e.assignments.Get(ctx, "s-a1")
	if err != nil || got.State() != first.State() {
		t.Fatalf("stored = %+v, %v; want %+v", got, err, first.State())
	}
}

// Version 46 adds done_at as NULL to every earlier assignment and goes back down keeping the rows.
func TestMigrationAssignmentDone(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, assignmentDoneVersion-1); err != nil {
		t.Fatalf("up to %d: %v", assignmentDoneVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO assignments (id, title, created_at, updated_at, version) VALUES ('a1', 'hw', 1, 1, 1)`)

	if _, err := p.UpTo(ctx, assignmentDoneVersion); err != nil {
		t.Fatalf("migrate to %d: %v", assignmentDoneVersion, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM assignments WHERE id = 'a1' AND done_at IS NULL"); n != 1 {
		t.Fatalf("earlier rows without done_at = %d, want 1", n)
	}
	mustExec(t, db, "UPDATE assignments SET done_at = 5 WHERE id = 'a1'")

	if _, err := p.DownTo(ctx, assignmentDoneVersion-1); err != nil {
		t.Fatalf("down to %d: %v", assignmentDoneVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('assignments') WHERE name = 'done_at'"); n != 0 {
		t.Fatal("assignments.done_at still exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM assignments"); n != 1 {
		t.Fatalf("assignments after down = %d, want 1", n)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
