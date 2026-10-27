package homeworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// deletedRepo is an AssignmentRepository whose every assignment the student has deleted.
type deletedRepo struct{ AssignmentRepository }

func (r deletedRepo) Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	a, err := r.AssignmentRepository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.Delete(t0.Add(time.Minute)); err != nil {
		return nil, err
	}
	return a, nil
}

func wantDeleted(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, shared.ErrNotFound) || !errors.Is(err, assignment.ErrDeleted) {
		t.Fatalf("%s of a deleted assignment: err = %v, want ErrNotFound and ErrDeleted", what, err)
	}
}

// The agent's proposals treat a deleted assignment as unknown (ADR-0029): ProposeDeadline and ProposeEmailOfVersion
// refuse it and store nothing, a model proposal naming it is REJECTED as an unknown assignment.
func TestProposalsOfADeletedAssignment(t *testing.T) {
	e := newAgentProposalEnv(t, true)
	e.exportVersion(t, 2)
	e.svc.assignments = deletedRepo{e.svc.assignments}

	_, err := e.svc.ProposeDeadline(t.Context(), "a1")
	wantDeleted(t, "ProposeDeadline", err)
	_, err = e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2)
	wantDeleted(t, "ProposeEmailOfVersion", err)
	if len(e.proposals) != 0 {
		t.Fatalf("%d proposals stored", len(e.proposals))
	}

	for _, out := range []string{outEmail, outExport} {
		v := propose(t, e.env, out)
		if v.Status != proposal.StatusRejected || v.Reason != proposal.ReasonUnknownAssignment {
			t.Fatalf("model proposal %s = %+v, want REJECTED unknown assignment", out, v)
		}
	}
	if e.sideEffects() != 0 {
		t.Fatal("a proposal had a side effect")
	}
}

// «Да» on a validated proposal whose assignment the student deleted since goes STALE as assignment_deleted: nothing
// is sent, exported or put into the calendar, and it can never be executed afterwards (ADR-0029). So does the repeat
// of a confirmed one whose earlier execution failed.
func TestYesOnAProposalOfADeletedAssignment(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		out       string
		yes       func(e *env, v ProposalView) error
		confirmed bool // confirmed (an execution that failed) before the delete
	}{
		{"send_homework_email", outEmail, func(e *env, v ProposalView) error {
			_, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, v.AttachmentFormat)
			return err
		}, false},
		{"export_homework", outExport, func(e *env, v ProposalView) error {
			if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
				return err
			}
			_, err := e.svc.Execute(ctx, v.ID)
			return err
		}, false},
		{"create_deadline_event", outDeadline, func(e *env, v ProposalView) error {
			_, err := e.svc.ConfirmDeadline(ctx, v.ID)
			return err
		}, false},
		{"create_deadline_event confirmed before the delete", outDeadline, func(e *env, v ProposalView) error {
			_, err := e.svc.Execute(ctx, v.ID)
			return err
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t).withHomework(t)
			v := propose(t, e, tc.out)
			if v.Status != proposal.StatusValidated {
				t.Fatalf("proposal %+v, want VALIDATED", v)
			}
			if tc.confirmed {
				if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
					t.Fatal(err)
				}
			}
			e.svc.assignments = deletedRepo{e.svc.assignments}

			if err := tc.yes(e, v); !errors.Is(err, proposal.ErrStale) {
				t.Fatalf("«Да» = %v, want ErrStale", err)
			}
			got, err := e.svc.Get(ctx, v.ID)
			if err != nil || got.Status != proposal.StatusStale || got.Reason != proposal.ReasonAssignmentDeleted {
				t.Fatalf("proposal %+v, %v; want STALE assignment_deleted", got, err)
			}
			if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, proposal.ErrNotConfirmed) {
				t.Fatalf("Execute after STALE = %v, want ErrNotConfirmed", err)
			}
			if n := e.sideEffects(); n != 0 {
				t.Fatalf("%d side effects", n)
			}
		})
	}
}

// A draft of a deleted assignment is not generated: nothing is enqueued.
func TestGenerateDraftOfADeletedAssignment(t *testing.T) {
	e := newDraftEnv(t)
	e.mu.Lock()
	st := e.assignments["a1"]
	st.DeletedAt, st.Version = ppT0, st.Version+1
	e.assignments["a1"] = st
	e.mu.Unlock()

	_, err := e.svc.Generate(context.Background(), "a1", "")
	wantDeleted(t, "Generate", err)
	if n := e.jobCount(); n != 0 {
		t.Fatalf("%d jobs enqueued", n)
	}
}

// A deleted assignment found in a material is a tombstone: the homework search of the material run again neither
// revives nor re-creates it (ADR-0029), and the live one is still updated.
func TestMaterialReextractionKeepsADeletedAssignment(t *testing.T) {
	e := newMaterialEnv(t)
	e.addMaterial("m1", material.TypeDOCX, slides("Задание к семинару"), "l1")
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.materialSnapshot("j1", MaterialHomeworkJobType, "m1")); err != nil {
		t.Fatal(err)
	}
	list, err := ppAssignments{e.ppStore}.ListByMaterial(context.Background(), "m1")
	if err != nil || len(list) != 2 {
		t.Fatalf("assignments = %v, %v", list, err)
	}
	gone := list[0].ID()

	e.mu.Lock()
	st := e.assignments[gone]
	st.DeletedAt, st.Version = ppT0, st.Version+1
	e.assignments[gone] = st
	tombstone := st
	stored := len(e.assignments)
	e.mu.Unlock()

	e.llm.Answer(values.OperationExtractHomework, `{"subject_hint":"Алгебра","assignments":[`+
		`{"title":"Model title","description":"","due":"2026-10-17T23:59:00+03:00"},`+
		`{"title":"Map of rivers and lakes","description":"Use colours","due":null}]}`)
	if err := e.handle(e.materialSnapshot("j2", MaterialHomeworkJobType, "m1")); err != nil {
		t.Fatal(err)
	}

	e.mu.Lock()
	got, n := e.assignments[gone], len(e.assignments)
	e.mu.Unlock()
	if got.DeletedAt.IsZero() || got.Version != tombstone.Version || got.Title != tombstone.Title || n != stored {
		t.Fatalf("tombstone = %+v (%d stored), was %+v (%d stored)", got, n, tombstone, stored)
	}
	live, err := ppAssignments{e.ppStore}.ListByMaterial(context.Background(), "m1")
	if err != nil || len(live) != 1 || live[0].ID() == gone || live[0].Details().Title() != "Map of rivers and lakes" {
		t.Fatalf("live assignments = %v, %v", live, err)
	}
}
