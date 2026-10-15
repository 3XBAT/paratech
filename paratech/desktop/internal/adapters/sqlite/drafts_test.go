package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/shared"
)

// draftEnv is the coursework database with the stored assignments a1 and a2.
type draftEnv struct {
	*courseworkEnv
	drafts *DraftRepository
}

func newDraftEnv(t *testing.T) *draftEnv {
	t.Helper()
	e := &draftEnv{courseworkEnv: newCourseworkEnv(t)}
	for i, id := range []string{"a1", "a2"} {
		if err := e.assignments.Save(context.Background(), newExtracted(t, id, "t1", i, time.Time{})); err != nil {
			t.Fatal(err)
		}
	}
	e.drafts = NewDraftRepository(e.db)
	return e
}

// editedDraft is a never-stored draft of a with one edited version.
func editedDraft(t *testing.T, a shared.AssignmentID) *homeworkdraft.Draft {
	t.Helper()
	d, err := homeworkdraft.New(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Edit(0, "Rivers", "Rivers flow to the sea.", asgT0); err != nil {
		t.Fatal(err)
	}
	return d
}

func generate(t *testing.T, d *homeworkdraft.Draft, job shared.JobID, at time.Time) {
	t.Helper()
	if _, err := d.AddGenerated(homeworkdraft.Generation{
		Base: d.LatestNumber(), Job: job, Title: "Rivers", Body: "Deltas form where rivers reach the sea.",
		Instruction: "shorter", PromptVersion: 1,
	}, at); err != nil {
		t.Fatal(err)
	}
}

func revisionCount(t *testing.T, e *draftEnv) int64 {
	t.Helper()
	return scanInt(t, e.db.sql, "SELECT count(*) FROM homework_draft_revisions")
}

func TestDraftSaveGet(t *testing.T) {
	e, ctx := newDraftEnv(t), context.Background()
	d := editedDraft(t, "a1")
	if err := e.drafts.Save(ctx, d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if d.Version() != 1 {
		t.Fatalf("version after the first save = %d, want 1", d.Version())
	}

	got, err := e.drafts.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	generate(t, got, "job-1", asgT0.Add(time.Minute))
	if err := e.drafts.Save(ctx, got); err != nil {
		t.Fatalf("Save of a stored draft: %v", err)
	}
	if got.Version() != 2 || revisionCount(t, e) != 2 {
		t.Fatalf("version %d, %d stored versions; want 2 and 2", got.Version(), revisionCount(t, e))
	}
	// Saving the same instance again works and adds nothing.
	if err := e.drafts.Save(ctx, got); err != nil || got.Version() != 3 || revisionCount(t, e) != 2 {
		t.Fatalf("second save of one instance = %v, version %d, %d versions", err, got.Version(), revisionCount(t, e))
	}

	again, err := e.drafts.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.State(), got.State()) {
		t.Fatalf("reloaded draft:\n got %+v\nwant %+v", again.State(), got.State())
	}
	if !again.ProducedBy("job-1") {
		t.Fatal("the generated version lost its job")
	}
}

func TestDraftGetMissing(t *testing.T) {
	e := newDraftEnv(t)
	if _, err := e.drafts.Get(context.Background(), "a1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of no draft = %v, want ErrNotFound", err)
	}
}

func TestDraftSaveConflicts(t *testing.T) {
	ctx := context.Background()
	stored := func(t *testing.T, e *draftEnv) *homeworkdraft.Draft {
		t.Helper()
		d := editedDraft(t, "a1")
		generate(t, d, "job-1", asgT0.Add(time.Minute))
		if err := e.drafts.Save(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, e *draftEnv) *homeworkdraft.Draft
		want  error
		count int64 // stored versions afterwards
	}{
		{"second draft of an assignment", func(t *testing.T, e *draftEnv) *homeworkdraft.Draft {
			stored(t, e)
			return editedDraft(t, "a1")
		}, shared.ErrVersionConflict, 2},
		{"stale version", func(t *testing.T, e *draftEnv) *homeworkdraft.Draft {
			d := stored(t, e)
			stale, err := e.drafts.Get(ctx, "a1")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Edit(2, "Rivers", "The winner.", asgT0.Add(2*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := e.drafts.Save(ctx, d); err != nil {
				t.Fatal(err)
			}
			if _, err := stale.Edit(2, "Rivers", "The loser.", asgT0.Add(3*time.Minute)); err != nil {
				t.Fatal(err)
			}
			return stale
		}, shared.ErrVersionConflict, 3},
		{"purged draft", func(t *testing.T, e *draftEnv) *homeworkdraft.Draft {
			d := stored(t, e)
			mustExec(t, e.db, "DELETE FROM homework_drafts WHERE assignment_id = 'a1'")
			return d
		}, shared.ErrVersionConflict, 0},
		{"unknown assignment", func(t *testing.T, _ *draftEnv) *homeworkdraft.Draft {
			return editedDraft(t, "nope")
		}, shared.ErrConflict, 0},
		{"one job, two drafts", func(t *testing.T, e *draftEnv) *homeworkdraft.Draft {
			stored(t, e)
			d := editedDraft(t, "a2")
			generate(t, d, "job-1", asgT0.Add(time.Minute))
			return d
		}, shared.ErrDuplicate, 2},
		{"no versions", func(t *testing.T, _ *draftEnv) *homeworkdraft.Draft {
			d, err := homeworkdraft.New("a2")
			if err != nil {
				t.Fatal(err)
			}
			return d
		}, homeworkdraft.ErrInvalidDraft, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newDraftEnv(t)
			d := tc.setup(t, e)
			before := d.Version()
			if err := e.drafts.Save(ctx, d); !errors.Is(err, tc.want) {
				t.Fatalf("Save = %v, want %v", err, tc.want)
			}
			if d.Version() != before {
				t.Fatalf("a refused save moved the version to %d", d.Version())
			}
			if n := revisionCount(t, e); n != tc.count {
				t.Fatalf("%d stored versions after a refused save, want %d", n, tc.count)
			}
			if tc.want == shared.ErrDuplicate {
				if _, err := e.drafts.Get(ctx, "a2"); !errors.Is(err, shared.ErrNotFound) {
					t.Fatalf("the refused draft was stored: %v", err)
				}
			}
		})
	}
}

// Versions are append-only: a save inserts only the versions the database does not have and never rewrites one.
func TestDraftSaveOnlyAppends(t *testing.T) {
	e, ctx := newDraftEnv(t), context.Background()
	d := editedDraft(t, "a1")
	if err := e.drafts.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.db, "UPDATE homework_draft_revisions SET body = 'changed in the row' WHERE number = 1")
	generate(t, d, "job-1", asgT0.Add(time.Minute))
	if err := e.drafts.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := e.db.sql.QueryRowContext(ctx, "SELECT body FROM homework_draft_revisions WHERE number = 1").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "changed in the row" || revisionCount(t, e) != 2 {
		t.Fatalf("version 1 body %q, %d versions: the save rewrote a stored version", body, revisionCount(t, e))
	}
}

// The student's draft keeps its assignment: reprocessing cannot delete an assignment that has one (ON DELETE RESTRICT).
func TestDraftKeepsItsAssignment(t *testing.T) {
	e, ctx := newDraftEnv(t), context.Background()
	if err := e.drafts.Save(ctx, editedDraft(t, "a1")); err != nil {
		t.Fatal(err)
	}
	a, err := e.assignments.Get(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	err = e.assignments.Delete(ctx, a)
	if !errors.Is(err, assignment.ErrInUse) || errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Delete of an assignment with a draft = %v, want ErrInUse", err)
	}
	if _, err := e.drafts.Get(ctx, "a1"); err != nil {
		t.Fatalf("draft after the refused delete: %v", err)
	}

	// Deleting the draft row removes its versions with it.
	mustExec(t, e.db, "DELETE FROM homework_drafts WHERE assignment_id = 'a1'")
	if n := revisionCount(t, e); n != 0 {
		t.Fatalf("%d versions left without their draft", n)
	}
	if err := e.assignments.Delete(ctx, a); err != nil {
		t.Fatalf("Delete without a draft: %v", err)
	}
}

// The schema refuses versions the aggregate cannot have, and a row that breaks the history is refused when loaded.
func TestDraftRowsAreChecked(t *testing.T) {
	ctx := context.Background()
	const insert = `INSERT INTO homework_draft_revisions (assignment_id, number, origin, title, body, instruction,
		prompt_version, job_id, created_at) VALUES `
	refused := []struct {
		name, values string
	}{
		{"number 0", `('a1', 0, 'edited', 't', 'b', '', NULL, NULL, 0)`},
		{"unknown origin", `('a1', 1, 'model', 't', 'b', '', NULL, NULL, 0)`},
		{"empty title", `('a1', 1, 'edited', '', 'b', '', NULL, NULL, 0)`},
		{"title too long", `('a1', 1, 'edited', printf('%201s', 'x'), 'b', '', NULL, NULL, 0)`},
		{"edited with a job", `('a1', 1, 'edited', 't', 'b', '', NULL, 'job-1', 0)`},
		{"edited with an instruction", `('a1', 1, 'edited', 't', 'b', 'shorter', NULL, NULL, 0)`},
		{"generated without a job", `('a1', 1, 'generated', 't', 'b', '', 1, NULL, 0)`},
		{"generated without a prompt version", `('a1', 1, 'generated', 't', 'b', '', NULL, 'job-1', 0)`},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			e := newDraftEnv(t)
			mustExec(t, e.db, "INSERT INTO homework_drafts (assignment_id, version) VALUES ('a1', 1)")
			_, err := e.db.sql.ExecContext(ctx, insert+tc.values)
			if !errors.Is(MapError(ctx, err), ErrConstraint) {
				t.Fatalf("insert = %v, want a constraint violation", err)
			}
		})
	}

	t.Run("gap in the history", func(t *testing.T) {
		e := newDraftEnv(t)
		d := editedDraft(t, "a1")
		generate(t, d, "job-1", asgT0.Add(time.Minute))
		if err := e.drafts.Save(ctx, d); err != nil {
			t.Fatal(err)
		}
		mustExec(t, e.db, "DELETE FROM homework_draft_revisions WHERE number = 1")
		if _, err := e.drafts.Get(ctx, "a1"); !errors.Is(err, homeworkdraft.ErrInvalidDraft) {
			t.Fatalf("Get of a history with a gap = %v, want ErrInvalidDraft", err)
		}
	})
}
