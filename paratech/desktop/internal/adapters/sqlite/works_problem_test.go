package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// TASK-226: the problem of the last reader call and the last send time survive closing and reopening the database.
func TestWorkProblemAndSentSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "works.db")
	sent := time.Date(2026, 10, 27, 12, 30, 15, 123e6, time.UTC) // stored in ms

	// run is one run of the app: open the database at path, run fn on its works, close it.
	run := func(fn func(db *DB, repo *WorkRepository)) {
		t.Helper()
		seedFromTemplate(t, path)
		db, err := Open(ctx, Options{Path: path, Migrate: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
		fn(db, NewWorkRepository(db))
	}
	get := func(repo *WorkRepository) *work.Work {
		t.Helper()
		w, err := repo.Get(ctx, "w1")
		if err != nil {
			t.Fatal(err)
		}
		return w
	}

	run(func(db *DB, repo *WorkRepository) {
		mustExec(t, db, `INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a1', 'hw', 0, 0)`)
		w, err := work.New("w1", work.Source{Assignment: "a1"}, libT0)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.NoteProblem(work.ProblemOffline, libT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, w); err != nil {
			t.Fatal(err)
		}
	})
	run(func(_ *DB, repo *WorkRepository) {
		w := get(repo)
		if w.LastProblem() != work.ProblemOffline || !w.SentAt().IsZero() {
			t.Fatalf("after a restart: problem %q, sent %v", w.LastProblem(), w.SentAt())
		}
		if err := w.Record(1, worksAnswer(work.FeasibleFull, 0), libT0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := w.RecordSent(sent); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, w); err != nil {
			t.Fatal(err)
		}
	})
	run(func(_ *DB, repo *WorkRepository) {
		w := get(repo)
		if w.LastProblem() != "" || !w.SentAt().Equal(sent) || w.Version() != 2 {
			t.Fatalf("after a second restart: problem %q, sent %v, version %d", w.LastProblem(), w.SentAt(), w.Version())
		}
	})
}

// Migration 35: a work stored before has no problem and was never sent; going down keeps it.
func TestMigrationToVersion35KeepsWorks(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 34); err != nil {
		t.Fatalf("up to 34: %v", err)
	}
	mustExec(t, db, `INSERT INTO assignments (id, title, version, created_at, updated_at) VALUES ('a1', 'Реферат', 1, 0, 0)`)
	mustExec(t, db, `INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
		VALUES ('w1', 'a1', 'failed', 1, 3, 4, 5, 6)`)

	if _, err := p.UpTo(ctx, 35); err != nil {
		t.Fatalf("up to 35: %v", err)
	}
	w, err := NewWorkRepository(db).Get(ctx, "w1")
	if err != nil || w.Status() != work.StatusFailed || w.LastProblem() != "" || !w.SentAt().IsZero() {
		t.Fatalf("an old work after the migration: %+v, %v", w, err)
	}
	if _, err := p.DownTo(ctx, 34); err != nil {
		t.Fatalf("down to 34: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM works WHERE id = 'w1' AND version = 4"); n != 1 {
		t.Fatalf("works after down = %d", n)
	}
}

// The columns take every problem of the domain and nothing else.
func TestWorkProblemColumn(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	mustExec(t, db, `INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a1', 'hw', 0, 0)`)
	repo := NewWorkRepository(db)
	w, _ := work.New("w1", work.Source{Assignment: "a1"}, libT0)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	for _, p := range work.Problems() {
		if err := w.NoteProblem(p, libT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, w); err != nil {
			t.Fatalf("Save with %s: %v", p, err)
		}
		if got, err := repo.Get(ctx, "w1"); err != nil || got.LastProblem() != p {
			t.Fatalf("Get with %s = %v, %v", p, got, err)
		}
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE works SET last_problem = 'model text' WHERE id = 'w1'`); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("an unknown problem: %v, want a constraint violation", err)
	}

	// A problem the aggregate does not allow after an answer is not loaded.
	if err := w.Record(1, worksAnswer(work.FeasibleFull, 0), libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "w1"); err != nil || got.LastProblem() != "" {
		t.Fatalf("Get after an answer = %v, %v", got, err)
	}
	mustExec(t, db, `UPDATE works SET last_problem = 'offline' WHERE id = 'w1'`)
	if _, err := repo.Get(ctx, "w1"); !errors.Is(err, work.ErrInvalidWork) {
		t.Fatalf("Get of a tampered work = %v", err)
	}
}

// LastSent is the newest SENT delivery of the sends of the work; other works, other kinds and unsent ones do not count.
func TestWorkLastSent(t *testing.T) {
	db, repo := newDeliveryEnv(t)
	ctx := context.Background()
	works, proposals := NewWorkRepository(db), NewProposalRepository(db)

	if got, err := works.LastSent(ctx, "w1"); err != nil || !got.IsZero() {
		t.Fatalf("LastSent of none = %v, %v", got, err)
	}

	// key confirms a send of work w and returns the idempotency key its email carries.
	key := func(id proposal.ID, w shared.WorkID) string {
		t.Helper()
		p, err := proposal.NewWorkSend(id, "a1", w, proposal.FormatDocx, dlvT0)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Validate(workSendFacts(t), dlvT0); err != nil {
			t.Fatal(err)
		}
		if err := p.Confirm(dlvT0); err != nil {
			t.Fatal(err)
		}
		if err := proposals.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		cmd, err := p.WorkEmailCommand("Реферат.docx")
		if err != nil {
			t.Fatal(err)
		}
		return cmd.IdempotencyKey()
	}
	deliver := func(id, key string, sentAt time.Time) {
		t.Helper()
		d := newTestDelivery(t, id, key)
		if !sentAt.IsZero() {
			if err := d.StartSending(sentAt); err != nil {
				t.Fatal(err)
			}
			if _, err := d.RecordResult(nil, sentAt); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.Save(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	first, second := dlvT0.Add(time.Hour), dlvT0.Add(2*time.Hour)
	deliver("d1", key("p1", "w1"), first)
	deliver("d2", key("p2", "w1"), second)
	deliver("d3", key("p3", "w1"), time.Time{})            // still pending
	deliver("d4", key("p4", "w2"), dlvT0.Add(3*time.Hour)) // another work
	deliver("d5", "proposal:p5", dlvT0.Add(4*time.Hour))   // no proposal of a work
	if got, err := works.LastSent(ctx, "w1"); err != nil || !got.Equal(second) {
		t.Fatalf("LastSent = %v, %v; want %v", got, err, second)
	}
}
