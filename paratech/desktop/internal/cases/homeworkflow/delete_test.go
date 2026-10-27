package homeworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// deleteOrigins stores one assignment of every origin (ADR-0029): extracted from a recording, found in a material,
// added by the student and added by the agent. It returns their IDs by origin.
func deleteOrigins(t *testing.T, e *createEnv) map[string]shared.AssignmentID {
	t.Helper()
	ctx := context.Background()

	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	extracted := e.assignmentsOf("t1")[0].ID

	e.mu.Lock()
	e.assignments["am1"] = assignment.State{
		Version: 1, ID: "am1", Material: "m1", SourceIndex: 0, PromptVersion: 1, Title: "Из материала",
		CreatedAt: ppT0, UpdatedAt: ppT0,
	}
	e.mu.Unlock()

	student, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Своё задание"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := e.svc.CreateAssignment(ctx, NewAssignment{ID: "ag-1", Title: "От агента", Subject: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]shared.AssignmentID{
		"extracted from a recording": extracted, "found in a material": "am1",
		"added by the student": student.ID, "added by the agent": agent.ID,
	}
}

func TestDeleteAndRestoreEveryOrigin(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	for origin, id := range deleteOrigins(t, e) {
		t.Run(origin, func(t *testing.T) {
			before, err := e.svc.Assignment(ctx, id)
			if err != nil {
				t.Fatal(err)
			}

			e.clock.Advance(time.Minute)
			deleted, err := e.svc.DeleteAssignment(ctx, id, before.Version)
			if err != nil {
				t.Fatalf("DeleteAssignment: %v", err)
			}
			if !deleted.Deleted || deleted.Version != before.Version+1 || deleted.Title != before.Title {
				t.Fatalf("deleted view = %+v, want deleted at version %d", deleted, before.Version+1)
			}
			if e.assignments[id].DeletedAt.IsZero() {
				t.Fatal("no deleted_at stored")
			}

			// Hidden from the readers of the use case.
			if _, err := e.svc.Assignment(ctx, id); !errors.Is(err, shared.ErrNotFound) || !errors.Is(err, assignment.ErrDeleted) {
				t.Fatalf("Assignment of a deleted one: err = %v, want ErrNotFound and ErrDeleted", err)
			}
			list, err := e.svc.ListAssignments(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range list {
				if v.ID == id {
					t.Fatal("ListAssignments shows the deleted assignment")
				}
			}
			if _, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: id, Title: "Правка"}); !errors.Is(err, assignment.ErrDeleted) {
				t.Fatalf("edit of a deleted one: err = %v", err)
			}
			if _, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: id, Subject: "Физика"}); !errors.Is(err, assignment.ErrDeleted) {
				t.Fatalf("correction of a deleted one: err = %v", err)
			}
			if _, err := e.svc.DeleteAssignment(ctx, id, deleted.Version); !errors.Is(err, assignment.ErrDeleted) {
				t.Fatalf("second delete: err = %v", err)
			}

			restored, err := e.svc.RestoreAssignment(ctx, id, deleted.Version)
			if err != nil {
				t.Fatalf("RestoreAssignment: %v", err)
			}
			if restored.Deleted || restored.Version != deleted.Version+1 || restored.Title != before.Title {
				t.Fatalf("restored view = %+v", restored)
			}
			if _, err := e.svc.Assignment(ctx, id); err != nil {
				t.Fatalf("Assignment after the restore: %v", err)
			}
			if _, err := e.svc.RestoreAssignment(ctx, id, restored.Version); !errors.Is(err, assignment.ErrNotDeleted) {
				t.Fatalf("restore of one not deleted: err = %v", err)
			}
		})
	}
}

func TestDeleteAssignmentRefusals(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(e *createEnv, id shared.AssignmentID)
		version func(v int64) int64
		want    error
	}{
		{name: "another version", version: func(v int64) int64 { return v + 1 }, want: shared.ErrVersionConflict},
		{name: "an older version", version: func(v int64) int64 { return v - 1 }, want: shared.ErrVersionConflict},
		{
			name: "something in progress uses it", want: assignment.ErrInUse,
			prepare: func(e *createEnv, id shared.AssignmentID) { e.busy[id] = true },
		},
		{
			name: "the check fails", want: errBusyRead,
			prepare: func(e *createEnv, _ shared.AssignmentID) { e.busyErr = errBusyRead },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCreateEnv(t, testSubjects)
			ctx := context.Background()
			v, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Задание"})
			if err != nil {
				t.Fatal(err)
			}
			if c.prepare != nil {
				c.prepare(e, v.ID)
			}
			version := v.Version
			if c.version != nil {
				version = c.version(version)
			}

			if _, err := e.svc.DeleteAssignment(ctx, v.ID, version); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			st := e.assignments[v.ID]
			if !st.DeletedAt.IsZero() || st.Version != v.Version {
				t.Fatalf("a refused delete changed the assignment: %+v", st)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		e := newCreateEnv(t, testSubjects)
		if _, err := e.svc.DeleteAssignment(context.Background(), "nope", 1); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
		if _, err := e.svc.RestoreAssignment(context.Background(), "nope", 1); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("restore: err = %v", err)
		}
	})
}

var errBusyRead = errors.New("disk I/O error")

func TestRestoreAssignmentChecksTheVersion(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	v, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Задание"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.DeleteAssignment(ctx, v.ID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RestoreAssignment(ctx, v.ID, v.Version); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale restore: err = %v", err)
	}
	if e.assignments[v.ID].DeletedAt.IsZero() {
		t.Fatal("a refused restore brought it back")
	}
	if _, err := e.svc.RestoreAssignment(ctx, v.ID, d.Version); err != nil {
		t.Fatal(err)
	}
}

// The agent's Undo of its create_assignment after the student deleted the assignment is refused as changed: the
// delete moved the version on (ADR-0029). A repeat of the create does not bring it back.
func TestDeletedAgentAssignmentIsNotUndoneOrRepeated(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	in := NewAssignment{ID: "ag-1", Title: "От агента"}
	v, err := e.svc.CreateAssignment(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.DeleteAssignment(ctx, v.ID, v.Version); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, v.Version); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Undo of a deleted one: err = %v, want ErrVersionConflict", err)
	}
	if _, err := e.svc.CreateAssignment(ctx, in); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("repeat of the create: err = %v, want ErrDuplicate", err)
	}
	if e.assignments[v.ID].DeletedAt.IsZero() {
		t.Fatal("the deleted assignment came back")
	}
}

// Reprocessing a recording never re-creates or revives a deleted extracted assignment: it is a tombstone, as a
// user-edited one (ADR-0029). A shorter answer does not withdraw it either.
func TestReprocessingKeepsADeletedAssignment(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	first := e.assignmentsOf("t1")
	gone := []shared.AssignmentID{first[0].ID, first[1].ID}
	for _, st := range first {
		if _, err := e.svc.DeleteAssignment(ctx, st.ID, st.Version); err != nil {
			t.Fatal(err)
		}
	}
	deleted := e.assignmentsOf("t1")

	// The same items again with other values and other hints, then a shorter answer.
	e.llm.Answer(values.OperationExtractHomework, `{"subject_hint":"Алгебра","teacher_hint":"Петров","assignments":[`+
		`{"title":"Model title","description":"","due":"2026-10-17T23:59:00+03:00"},`+
		`{"title":"Map of rivers and lakes","description":"Use colours","due":null}]}`)
	if err := e.handle(e.snapshot("j2", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	e.llm.Answer(values.OperationExtractHomework, `{"assignments":[]}`)
	if err := e.handle(e.snapshot("j3", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}

	got := e.assignmentsOf("t1")
	if len(got) != len(deleted) {
		t.Fatalf("assignments = %+v, want the %d tombstones only", got, len(deleted))
	}
	for i, st := range got {
		if st.ID != gone[i] || st.DeletedAt.IsZero() || st.Version != deleted[i].Version || st.Title != deleted[i].Title {
			t.Fatalf("tombstone %d changed: %+v, was %+v", i, st, deleted[i])
		}
	}
	list, err := e.svc.ListAssignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if v.ID == gone[0] || v.ID == gone[1] {
			t.Fatalf("reprocessing brought %s back to the list", v.ID)
		}
	}
}
