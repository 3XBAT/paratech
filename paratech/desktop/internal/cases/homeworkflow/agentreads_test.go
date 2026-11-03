package homeworkflow

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

func TestHasDeadlineEvent(t *testing.T) {
	t.Run("none, waiting, executed", func(t *testing.T) {
		e := newAgentProposalEnv(t, false)
		ctx := t.Context()
		has := func() bool {
			t.Helper()
			ok, err := e.svc.HasDeadlineEvent(ctx, "a1")
			if err != nil {
				t.Fatal(err)
			}
			return ok
		}
		if has() {
			t.Fatal("no proposal: has an event")
		}
		v, err := e.svc.ProposeDeadline(ctx, "a1")
		if err != nil {
			t.Fatal(err)
		}
		if has() {
			t.Fatal("a proposal waiting for «Да»: has an event")
		}
		if _, err := e.svc.ConfirmDeadline(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if !has() {
			t.Fatal("an executed deadline proposal: no event")
		}
		if ok, err := e.svc.HasDeadlineEvent(ctx, "a2"); err != nil || ok {
			t.Fatalf("another assignment = %v, %v", ok, err)
		}
	})

	t.Run("confirmed, the calendar failed: counts", func(t *testing.T) {
		e := newAgentProposalEnv(t, false)
		v, err := e.svc.ProposeDeadline(t.Context(), "a1")
		if err != nil {
			t.Fatal(err)
		}
		e.calendar.Fail(ErrCalendarNotConnected)
		if _, err := e.svc.ConfirmDeadline(t.Context(), v.ID); !errors.Is(err, ErrCalendarNotConnected) {
			t.Fatal(err)
		}
		if e.proposals[v.ID].Status != proposal.StatusConfirmed {
			t.Fatal("not confirmed")
		}
		if ok, err := e.svc.HasDeadlineEvent(t.Context(), "a1"); err != nil || !ok {
			t.Fatalf("confirmed = %v, %v; want an event (it may exist)", ok, err)
		}
	})

	t.Run("stale: counts", func(t *testing.T) {
		e := newAgentProposalEnv(t, false)
		v, err := e.svc.ProposeDeadline(t.Context(), "a1")
		if err != nil {
			t.Fatal(err)
		}
		// Confirmed (the calendar call failed), then gone STALE before an execution was recorded.
		e.calendar.Fail(ErrCalendarNotConnected)
		if _, err := e.svc.ConfirmDeadline(t.Context(), v.ID); !errors.Is(err, ErrCalendarNotConnected) {
			t.Fatal(err)
		}
		ps := e.proposals[v.ID]
		ps.Status, ps.Reason = proposal.StatusStale, proposal.ReasonAssignmentDeleted
		e.proposals[v.ID] = ps
		if ok, err := e.svc.HasDeadlineEvent(t.Context(), "a1"); err != nil || !ok {
			t.Fatalf("stale = %v, %v; want an event (a confirmed one may have made it)", ok, err)
		}
		if ok, err := e.svc.HasDeadlineEvent(t.Context(), "a2"); err != nil || ok {
			t.Fatalf("another assignment = %v, %v", ok, err)
		}
	})

	t.Run("the list cannot be read", func(t *testing.T) {
		e := newAgentProposalEnv(t, false)
		svc := *e.svc
		svc.waiting = failingWaiting{}
		if _, err := svc.HasDeadlineEvent(t.Context(), "a1"); err == nil {
			t.Fatal("a read error is no answer")
		}
		svc.waiting = nil
		if _, err := svc.HasDeadlineEvent(t.Context(), "a1"); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("no port = %v", err)
		}
	})
}

func TestAssignmentStateSeesADeletedOne(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	v, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Задание"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := e.svc.DeleteAssignment(ctx, v.ID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.AssignmentState(ctx, v.ID)
	if err != nil || !got.Deleted || got.Version != deleted.Version || got.Title != "Задание" {
		t.Fatalf("AssignmentState = %+v, %v; want the deleted one at version %d", got, err, deleted.Version)
	}
	if _, err := e.svc.AssignmentState(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}
