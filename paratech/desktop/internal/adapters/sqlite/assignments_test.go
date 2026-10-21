package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

var asgT0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// courseworkEnv is a database with a lesson l1 and a stored transcript t1 of a completed recording.
type courseworkEnv struct {
	*transcriptEnv
	assignments *AssignmentRepository
	summaries   *SummaryRepository
}

func newCourseworkEnv(t *testing.T) *courseworkEnv {
	t.Helper()
	e := &courseworkEnv{transcriptEnv: newTranscriptEnv(t)}
	if err := e.repo.Save(context.Background(), e.newTranscript(t, "t1", "ru")); err != nil {
		t.Fatal(err)
	}
	if err := NewLessonRepository(e.db).Save(context.Background(), newTestLesson(t, "l1", "", 0)); err != nil {
		t.Fatal(err)
	}
	e.assignments, e.summaries = NewAssignmentRepository(e.db), NewSummaryRepository(e.db)
	return e
}

func newExtracted(t *testing.T, id string, transcript shared.TranscriptID, index int, due time.Time) *assignment.Assignment {
	t.Helper()
	d, err := assignment.NewDetails("Homework "+id, "Pages 1-10\nand exercise 3", due)
	if err != nil {
		t.Fatal(err)
	}
	a, err := assignment.NewExtracted(assignment.Spec{
		ID: shared.AssignmentID(id), Lesson: "l1", Origin: assignment.Origin{Transcript: transcript, Index: index},
		PromptVersion: 1, Details: d,
	}, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAssignmentSaveGet(t *testing.T) {
	for _, due := range []time.Time{asgT0.Add(48 * time.Hour), {}} {
		t.Run("due "+due.String(), func(t *testing.T) {
			e, ctx := newCourseworkEnv(t), context.Background()
			a := newExtracted(t, "a1", "t1", 0, due)
			if err := e.assignments.Save(ctx, a); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if a.Version() != 1 {
				t.Fatalf("version after insert = %d, want 1", a.Version())
			}

			for name, get := range map[string]func() (*assignment.Assignment, error){
				"by id":     func() (*assignment.Assignment, error) { return e.assignments.Get(ctx, "a1") },
				"by source": func() (*assignment.Assignment, error) { return e.assignments.GetBySource(ctx, "t1", 0) },
			} {
				got, err := get()
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if got.State() != a.State() {
					t.Fatalf("%s: got %+v, want %+v", name, got.State(), a.State())
				}
			}
		})
	}
}

func TestAssignmentUpdateKeepsTheEdit(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	a := newExtracted(t, "a1", "t1", 0, asgT0.Add(48*time.Hour))
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	d, err := assignment.NewDetails("My title", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Edit(d, asgT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatalf("Save after Edit: %v", err)
	}

	got, err := e.assignments.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UserEdited() || got.Version() != 2 || !got.Details().Equal(d) || !got.UpdatedAt().Equal(asgT0.Add(time.Hour)) {
		t.Fatalf("stored after Edit: %+v", got.State())
	}
	if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments WHERE due_at IS NULL AND user_edited = 1"); n != 1 {
		t.Fatalf("rows with a cleared deadline and the edit flag = %d, want 1", n)
	}
}

func TestAssignmentSaveConflicts(t *testing.T) {
	ctx := context.Background()
	t.Run("stale version", func(t *testing.T) {
		e := newCourseworkEnv(t)
		a := newExtracted(t, "a1", "t1", 0, time.Time{})
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		stale, err := e.assignments.Get(ctx, "a1")
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Edit(a.Details(), asgT0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err := stale.Reextract(a.Details(), 2, asgT0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := e.assignments.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
		}
		if stale.Version() != 1 {
			t.Fatalf("a failed Save changed the version to %d", stale.Version())
		}
	})
	t.Run("same id twice", func(t *testing.T) {
		e := newCourseworkEnv(t)
		if err := e.assignments.Save(ctx, newExtracted(t, "a1", "t1", 0, time.Time{})); err != nil {
			t.Fatal(err)
		}
		if err := e.assignments.Save(ctx, newExtracted(t, "a1", "t1", 1, time.Time{})); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("second insert of a1 = %v, want ErrVersionConflict", err)
		}
	})
	t.Run("same source twice", func(t *testing.T) {
		e := newCourseworkEnv(t)
		if err := e.assignments.Save(ctx, newExtracted(t, "a1", "t1", 0, time.Time{})); err != nil {
			t.Fatal(err)
		}
		err := e.assignments.Save(ctx, newExtracted(t, "a2", "t1", 0, time.Time{}))
		if !errors.Is(err, shared.ErrDuplicate) || errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("second assignment for the same item = %v, want ErrDuplicate", err)
		}
	})
	t.Run("unknown transcript", func(t *testing.T) {
		e := newCourseworkEnv(t)
		err := e.assignments.Save(ctx, newExtracted(t, "a1", "missing", 0, time.Time{}))
		if !errors.Is(err, shared.ErrConflict) {
			t.Fatalf("Save with an unknown transcript = %v, want ErrConflict", err)
		}
	})
	t.Run("purged row", func(t *testing.T) {
		e := newCourseworkEnv(t)
		a := newExtracted(t, "a1", "t1", 0, time.Time{})
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		mustExec(t, e.db, "DELETE FROM assignments WHERE id = 'a1'")
		if err := e.assignments.Save(ctx, a); !errors.Is(err, shared.ErrVersionConflict) {
			t.Fatalf("Save of a purged row = %v, want ErrVersionConflict", err)
		}
		if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments"); n != 0 {
			t.Fatal("a purged row was inserted again")
		}
	})
}

func TestAssignmentNotFound(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if _, err := e.assignments.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	if _, err := e.assignments.GetBySource(ctx, "t1", 0); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("GetBySource = %v, want ErrNotFound", err)
	}
}

func TestAssignmentsListByTranscript(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	for i, id := range []string{"b", "a", "c"} {
		if err := e.assignments.Save(ctx, newExtracted(t, id, "t1", i, time.Time{})); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.assignments.ListByTranscript(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []shared.AssignmentID
	for _, a := range got {
		ids = append(ids, a.ID())
	}
	if len(ids) != 3 || ids[0] != "b" || ids[1] != "a" || ids[2] != "c" {
		t.Fatalf("ListByTranscript = %v, want b a c (answer order)", ids)
	}
	if none, err := e.assignments.ListByTranscript(ctx, "other"); err != nil || len(none) != 0 {
		t.Fatalf("ListByTranscript(other) = %v, %v", none, err)
	}
}

func TestAssignmentDelete(t *testing.T) {
	ctx := context.Background()
	stored := func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
		t.Helper()
		a := newExtracted(t, "a1", "t1", 0, time.Time{})
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, e *courseworkEnv) *assignment.Assignment
		want  error // nil: deleted
	}{
		{"current version", stored, nil},
		{"stale version", func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
			a := stored(t, e)
			stale, err := e.assignments.Get(ctx, a.ID())
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Edit(a.Details(), asgT0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := e.assignments.Save(ctx, a); err != nil {
				t.Fatal(err)
			}
			return stale
		}, shared.ErrVersionConflict},
		{"already gone", func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
			a := stored(t, e)
			mustExec(t, e.db, "DELETE FROM assignments WHERE id = 'a1'")
			return a
		}, shared.ErrVersionConflict},
		{"never stored", func(t *testing.T, _ *courseworkEnv) *assignment.Assignment {
			return newExtracted(t, "a1", "t1", 0, time.Time{})
		}, shared.ErrVersionConflict},
		{"referenced by a delivery", func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
			a := stored(t, e)
			mustExec(t, e.db, `INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id,
				idempotency_key, status, confirmed_at, created_at, updated_at)
				VALUES ('d1', 'a1', 'a@b.c', 's', 'exports/x', 'msg-1', 'dk-1', 'PENDING', 0, 0, 0)`)
			return a
		}, assignment.ErrInUse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCourseworkEnv(t)
			a := tc.setup(t, e)
			err := e.assignments.Delete(ctx, a)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Delete: %v", err)
				}
				if _, err := e.assignments.Get(ctx, a.ID()); !errors.Is(err, shared.ErrNotFound) {
					t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Delete = %v, want %v", err, tc.want)
			}
			if tc.want == assignment.ErrInUse {
				if errors.Is(err, shared.ErrVersionConflict) {
					t.Fatalf("Delete = %v: a kept row is not a version conflict", err)
				}
				if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments"); n != 1 {
					t.Fatalf("assignments after a refused delete = %d, want 1", n)
				}
			}
		})
	}

}

// Deleting the transcript (with its recording) keeps the assignments: they are the user's data (AC-20).
func TestAssignmentSurvivesTranscriptDeletion(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	if err := e.assignments.Save(ctx, newExtracted(t, "a1", "t1", 0, time.Time{})); err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.db, "DELETE FROM transcripts WHERE id = 't1'")

	got, err := e.assignments.Get(ctx, "a1")
	if err != nil {
		t.Fatalf("Get after the transcript was deleted: %v", err)
	}
	if _, ok := got.Origin(); ok || got.TranscriptID() != "" || got.SourceIndex() != 0 {
		t.Fatalf("assignment after the transcript was deleted: %+v", got.State())
	}
	if _, err := e.assignments.GetBySource(ctx, "t1", 0); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("GetBySource after deletion = %v, want ErrNotFound", err)
	}
}

// Version 2 of the schema adds columns to a table that may already hold rows of version 1.
func TestMigrationToVersion2KeepsAssignments(t *testing.T) {
	t.Parallel() // migrates a database of its own for real
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 1); err != nil {
		t.Fatalf("up to 1: %v", err)
	}
	mustExec(t, db, `INSERT INTO assignments (id, title, description, version, created_at, updated_at)
		VALUES ('old', 'Old homework', '', 1, 0, 0)`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate to 2: %v", err)
	}

	got, err := NewAssignmentRepository(db).Get(ctx, "old")
	if err != nil {
		t.Fatalf("Get of a version 1 row: %v", err)
	}
	if got.SourceIndex() != -1 || got.PromptVersion() != 0 || got.UserEdited() {
		t.Fatalf("version 1 row after the migration: %+v", got.State())
	}
	var src sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, "SELECT source_index FROM assignments WHERE id = 'old'").Scan(&src); err != nil || src.Valid {
		t.Fatalf("source_index = %v, %v; want NULL", src, err)
	}
}
