package agent

import (
	"context"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// previewAssignmentDeletion is the C path of change_assignment{delete}, an assignment with a Google event (ADR-0033
// О-6 б): nothing is deleted here. It checks the assignment is there and binds the card to it; the journal records
// the call PROPOSED with a preview of that ID. Only the student's «Да» in input/wails (AgentConfirmService) deletes
// it, through the use case of the assignment page; this package has no method that does.
func (t tools) previewAssignmentDeletion(ctx context.Context, call values.Call) (values.Ref, values.Refusal) {
	if act, _ := call.Args().Enum(values.ArgAction); act != values.AssignmentDelete {
		return values.Ref{}, values.RefuseUnavailable
	}
	a, _ := call.Args().ID("assignment")
	if _, err := t.actions.assignments.Assignment(ctx, shared.AssignmentID(a.ID())); err != nil {
		return values.Ref{}, t.proposalRefusal(ctx, call.Tool(), err)
	}
	target, ok := refOfID(values.KindPreview, a.ID())
	if !ok {
		return values.Ref{}, values.RefuseFailed
	}
	return target, ""
}

// cardOf is the card a proposal shows: the delete_assignment card of an assignment (title, deadline, «и событие в
// Google Календаре», read by the card itself), else the confirm card of the proposal or preview.
func cardOf(c agentturn.Call, target values.Ref) values.Card {
	if c.Call.Tool() == values.ToolChangeAssignment {
		a, _ := c.Call.Args().ID("assignment")
		return values.DeleteAssignmentCard{Assignment: a}
	}
	return values.ConfirmCard{Target: target}
}
