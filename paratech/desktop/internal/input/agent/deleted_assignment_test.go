package agent

import (
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// Every tool naming an assignment the student deleted answers unknown_id before its use case runs (ADR-0029): nothing
// is journaled, no use case is called, no card is shown.
func TestToolsNamingADeletedAssignmentAnswerUnknownID(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T) agentturn.Call
	}{
		{"triage_assignment", func(t *testing.T) agentturn.Call {
			return writeCall(t, "c1", values.ToolTriageAssignment, `{"assignment":"a1"}`)
		}},
		{"delivery_status", func(t *testing.T) agentturn.Call {
			return readCall(t, "c1", values.ToolDeliveryStatus, `{"assignment":"a1"}`)
		}},
		{"propose_deadline", func(t *testing.T) agentturn.Call {
			return proposeCall(t, "c1", values.ToolProposeDeadline, `{"assignment":"a1"}`)
		}},
		{"propose_send", func(t *testing.T) agentturn.Call {
			return proposeCall(t, "c1", values.ToolProposeSend, `{"assignment":"a1","version":"v1"}`)
		}},
		{"edit_due", func(t *testing.T) agentturn.Call {
			return writeCall(t, "c1", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, p, _, tl := proposeWorld(t)
			f := &fakeTurnkey{views: map[shared.WorkID]turnkey.View{}, started: turnkey.Started{View: questionsView(2), Created: true}}
			w.actions.turnkey = f
			w.deliveries.views["a1"] = []senddelivery.View{{ID: "d1", AssignmentID: "a1", Status: delivery.StatusSent, Attempts: 1}}
			w.assignments.deleted = map[shared.AssignmentID]bool{"a1": true}

			r := tl.run(t.Context(), req, tt.call(t))
			if !r.out.Result.IsError || r.out.Result.ResultJSON != `{"error":"`+string(values.RefuseUnknownID)+`"}` {
				t.Fatalf("result = %s, want unknown_id", r.out.Result.ResultJSON)
			}
			if len(r.cards) != 0 || len(r.out.Seen) != 0 || len(f.calls) != 0 || len(p.made()) != 0 {
				t.Fatalf("cards %v, seen %v, triage %v, proposals %v: the call ran", r.cards, r.out.Seen, f.calls, p.made())
			}
			if _, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); !errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("the call was journaled: %v", err)
			}
		})
	}
}

// A proposal use case that finds the assignment deleted (it was deleted after the guard read it) is unknown_id too,
// not not_found.
func TestProposalOfADeletedAssignmentIsAnUnknownID(t *testing.T) {
	deleted := fmt.Errorf("assignment a1: %w: %w", shared.ErrNotFound, assignment.ErrDeleted)
	for _, tool := range []values.Tool{values.ToolProposeDeadline, values.ToolProposeSend} {
		t.Run(tool.String(), func(t *testing.T) {
			_, p, _, tl := proposeWorld(t)
			p.deadline = func(shared.AssignmentID) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, deleted
			}
			p.email = func(shared.AssignmentID, int) (homeworkflow.ProposalView, error) {
				return homeworkflow.ProposalView{}, deleted
			}
			args := `{"assignment":"a1"}`
			if tool == values.ToolProposeSend {
				args = `{"assignment":"a1","version":"v1"}`
			}
			r := tl.run(t.Context(), req, proposeCall(t, "c1", tool, args))
			if r.out.Result.ResultJSON != `{"error":"`+string(values.RefuseUnknownID)+`"}` || len(r.cards) != 0 {
				t.Fatalf("result = %s, cards %v; want unknown_id", r.out.Result.ResultJSON, r.cards)
			}
		})
	}
}
