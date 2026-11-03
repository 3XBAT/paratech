package wails

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// cardAssignments is the page's deletion over a map: a deleted one is hidden as homeworkflow.Service hides it.
type cardAssignments struct {
	views   map[shared.AssignmentID]homeworkflow.AssignmentView
	deleted map[shared.AssignmentID]bool
	inUse   bool
	deletes []int64 // the versions DeleteAndStop was called with
}

func (c *cardAssignments) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	if c.deleted[id] {
		return homeworkflow.AssignmentView{}, fmt.Errorf("assignment %s: %w: %w", id, shared.ErrNotFound, assignment.ErrDeleted)
	}
	v, ok := c.views[id]
	if !ok {
		return v, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	return v, nil
}

func (c *cardAssignments) DeleteAndStop(_ context.Context, id shared.AssignmentID, expected int64) (homeworkflow.Deletion, error) {
	c.deletes = append(c.deletes, expected)
	v := c.views[id]
	switch {
	case c.inUse:
		return homeworkflow.Deletion{}, assignment.ErrInUse
	case v.Version != expected:
		return homeworkflow.Deletion{}, shared.ErrVersionConflict
	}
	c.deleted[id] = true
	v.Version++
	v.Deleted = true
	return homeworkflow.Deletion{Assignment: v}, nil
}

func newAssignmentCardService(a *cardAssignments) *AgentConfirmService {
	s := newConfirmService(newConfirmProposals())
	s.core.Assignments = a
	return s
}

func newCardAssignments() *cardAssignments {
	return &cardAssignments{
		views: map[shared.AssignmentID]homeworkflow.AssignmentView{
			"a1": {ID: "a1", Title: "Задачи 1-5", Subject: "Физика", Due: confirmDue, Version: 3},
		},
		deleted: map[shared.AssignmentID]bool{},
	}
}

// The delete_assignment card shows the assignment as stored and «Да» deletes it through the page's DeleteAndStop at
// the version it has at the click; a second «Да» deletes nothing more.
func TestDeleteAssignmentCard(t *testing.T) {
	a := newCardAssignments()
	s := newAssignmentCardService(a)
	ctx := t.Context()

	card, err := s.Card(ctx, "assignment", "a1")
	if err != nil {
		t.Fatal(err)
	}
	want := AgentConfirmCard{
		Kind: "delete_assignment", TargetKind: "assignment", TargetID: "a1", AssignmentID: "a1", State: "waiting",
		Title: "Задачи 1-5", Subject: "Физика", Due: "2026-10-24T18:00:00Z", Notice: msgDeleteWithEvent,
	}
	if fmt.Sprint(card) != fmt.Sprint(want) {
		t.Fatalf("card = %+v\nwant %+v", card, want)
	}

	done, err := s.Confirm(ctx, "assignment", "a1", "", "", "", "")
	if err != nil || done.State != "done" || !a.deleted["a1"] || len(a.deletes) != 1 || a.deletes[0] != 3 {
		t.Fatalf("confirm = %+v, %v; deletes %v", done, err, a.deletes)
	}
	again, err := s.Confirm(ctx, "assignment", "a1", "", "", "", "")
	if err != nil || again.State != "done" || len(a.deletes) != 1 {
		t.Fatalf("second «Да» = %+v, %v; deletes %v", again, err, a.deletes)
	}
	if c, err := s.Card(ctx, "assignment", "a1"); err != nil || c.State != "done" {
		t.Fatalf("card after the delete = %+v, %v", c, err)
	}
}

func TestDeleteAssignmentCardRefusals(t *testing.T) {
	t.Run("decline deletes nothing", func(t *testing.T) {
		a := newCardAssignments()
		got, err := newAssignmentCardService(a).Decline(t.Context(), "assignment", "a1")
		if err != nil || got.State != "declined" || len(a.deletes) != 0 {
			t.Fatalf("decline = %+v, %v", got, err)
		}
	})
	t.Run("an email may still go out", func(t *testing.T) {
		a := newCardAssignments()
		a.inUse = true
		if _, err := newAssignmentCardService(a).Confirm(t.Context(), "assignment", "a1", "", "", "", ""); !errors.Is(err, ErrAgentConfirmInUse) {
			t.Fatalf("confirm = %v", err)
		}
		if a.deleted["a1"] {
			t.Fatal("deleted while in use")
		}
	})
	t.Run("unknown and invalid", func(t *testing.T) {
		s := newAssignmentCardService(newCardAssignments())
		if _, err := s.Card(t.Context(), "assignment", "zz"); !errors.Is(err, ErrAgentConfirmNotFound) {
			t.Fatalf("unknown = %v", err)
		}
		if _, err := s.Confirm(t.Context(), "assignment", "", "", "", "", ""); !errors.Is(err, ErrAgentConfirmInvalid) {
			t.Fatalf("invalid = %v", err)
		}
	})
	t.Run("not wired", func(t *testing.T) {
		s := newConfirmService(newConfirmProposals())
		if _, err := s.Confirm(t.Context(), "assignment", "a1", "", "", "", ""); !errors.Is(err, ErrAgentConfirmInvalid) {
			t.Fatalf("confirm = %v", err)
		}
	})
}
