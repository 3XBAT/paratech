package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

func newByStudent(t *testing.T, id string) *assignment.Assignment {
	t.Helper()
	a, err := assignment.NewByStudent(assignment.StudentSpec{
		ID: shared.AssignmentID(id), Lesson: "l1", Title: "Решить номер 5", Note: "Задачник\nстр. 10",
		Due: asgT0.Add(48 * time.Hour), SubjectName: "Матанализ",
	}, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// An assignment the student added is stored with no source and no prompt version and reads back the same (ADR-0029).
func TestStudentAssignmentRoundtrip(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	a := newByStudent(t, "s-a1")
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	var source, prompt any
	if err := e.db.sql.QueryRow("SELECT source_index, prompt_version FROM assignments WHERE id = 's-a1'").Scan(&source, &prompt); err != nil {
		t.Fatal(err)
	}
	if source != nil || prompt != nil {
		t.Fatalf("row source_index = %v, prompt_version = %v; want NULL", source, prompt)
	}

	got, err := e.assignments.Get(ctx, "s-a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State() != a.State() || !got.ByStudent() || !got.UserEdited() || got.Correction().Subject() != "Матанализ" {
		t.Fatalf("got %+v, want %+v", got.State(), a.State())
	}
	list, err := e.assignments.ListRecent(ctx, 10)
	if err != nil || len(list) != 1 || list[0].ID() != "s-a1" {
		t.Fatalf("ListRecent = %v, %v", list, err)
	}
}

func TestAssignmentDeleteUnused(t *testing.T) {
	ctx := context.Background()
	stored := func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
		t.Helper()
		a := newByStudent(t, "a1")
		if err := e.assignments.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	referenced := func(insert string) func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
		return func(t *testing.T, e *courseworkEnv) *assignment.Assignment {
			a := stored(t, e)
			mustExec(t, e.db, insert)
			return a
		}
	}
	const proposalCols = `INSERT INTO proposals (id, kind, status, assignment_id, due_at, reason, confirmed_at, executed_at,
		version, created_at, updated_at) VALUES `
	cases := []struct {
		name  string
		setup func(t *testing.T, e *courseworkEnv) *assignment.Assignment
		want  error // nil: deleted
	}{
		{"unused", stored, nil},
		{"a declined proposal", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'DECLINED', 'a1', 1, '', NULL, NULL, 1, 0, 0)`), nil},
		{"a rejected proposal", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'REJECTED', 'a1', 1, 'invalid_due', NULL, NULL, 1, 0, 0)`), nil},
		{"a proposal of another assignment", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'VALIDATED', 'a2', 1, '', NULL, NULL, 1, 0, 0)`), nil},
		{"a proposal waiting for «Да»", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'VALIDATED', 'a1', 1, '', NULL, NULL, 1, 0, 0)`), assignment.ErrInUse},
		{"an executed proposal", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'EXECUTED', 'a1', 1, '', 1, 1, 1, 0, 0)`), assignment.ErrInUse},
		{"a stale proposal", referenced(proposalCols +
			`('p1', 'create_deadline_event', 'STALE', 'a1', 1, 'assignment_changed', 1, NULL, 1, 0, 0)`), assignment.ErrInUse},
		{"a draft", referenced("INSERT INTO homework_drafts (assignment_id, version) VALUES ('a1', 1)"), assignment.ErrInUse},
		{"a delivery", referenced(`INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id,
			idempotency_key, status, confirmed_at, created_at, updated_at)
			VALUES ('d1', 'a1', 'a@b.c', 's', 'exports/x', 'msg-1', 'dk-1', 'PENDING', 0, 0, 0)`), assignment.ErrInUse},
		{"a turnkey work", referenced(`INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
			VALUES ('w1', 'a1', 'triage', 1, 1, 1, 0, 0)`), assignment.ErrInUse},
		{"a failed turnkey work", referenced(`INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
			VALUES ('w1', 'a1', 'failed', 1, 1, 1, 0, 0)`), assignment.ErrInUse},
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
			return newByStudent(t, "a1")
		}, shared.ErrVersionConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCourseworkEnv(t)
			a := tc.setup(t, e)
			err := e.assignments.DeleteUnused(ctx, a)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("DeleteUnused: %v", err)
				}
				if _, err := e.assignments.Get(ctx, a.ID()); !errors.Is(err, shared.ErrNotFound) {
					t.Fatalf("Get after DeleteUnused = %v, want ErrNotFound", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("DeleteUnused = %v, want %v", err, tc.want)
			}
			if tc.want == assignment.ErrInUse {
				if errors.Is(err, shared.ErrVersionConflict) {
					t.Fatalf("DeleteUnused = %v: a kept row is not a version conflict", err)
				}
				if n := scanInt(t, e.db.sql, "SELECT count(*) FROM assignments"); n != 1 {
					t.Fatalf("assignments after a refused delete = %d, want 1", n)
				}
			}
		})
	}
}

// DeleteUnused joins the transaction of its caller: a rollback keeps the assignment.
func TestAssignmentDeleteUnusedInTheCallersTransaction(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	a := newByStudent(t, "a1")
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	err := e.db.Do(ctx, func(ctx context.Context) error {
		if err := e.assignments.DeleteUnused(ctx, a); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("Do = %v", err)
	}
	if _, err := e.assignments.Get(ctx, "a1"); err != nil {
		t.Fatalf("after the rollback: %v", err)
	}
}
