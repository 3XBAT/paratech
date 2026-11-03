package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// The delete_assignment card (ADR-0033 О-6 б): the agent asked to delete an assignment that has a Google deadline
// event. The card shows the assignment as stored and «Да» deletes it exactly as «Удалить» of the assignment page
// (DeleteAndStop: soft, with «Вернуть» on the page; the event goes with it through the calendar sync). The agent loop
// never reaches this: only the student's click does (hard rule 3).

const (
	opDeleteAssignmentCard    = "ui: agent delete assignment card"
	opDeleteAssignmentConfirm = "ui: agent delete assignment"
)

// agentCardDeleteAssignment is the kind of the card as the panel reads it; its target kind is "assignment".
const agentCardDeleteAssignment = "delete_assignment"

// msgDeleteWithEvent is what the card says besides the title and the deadline.
const msgDeleteWithEvent = "Вместе с заданием удалится и событие в Google Календаре."

// ErrAgentConfirmInUse refuses the delete while an email of the assignment may still go out (hard rule 7).
const ErrAgentConfirmInUse = agentConfirmError("Сейчас удалить нельзя: письмо по этому заданию ещё отправляется или его судьба неизвестна. Проверьте «Письма» и нажмите «Да» ещё раз.")

// AgentConfirmAssignments is the homework workflow (cases/homeworkflow) as the delete_assignment card uses it: the
// assignment as the page shows it and the page's own deletion.
type AgentConfirmAssignments interface {
	Assignment(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	DeleteAndStop(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (homeworkflow.Deletion, error)
}

// assignmentCard is the delete_assignment card of assignment id: waiting with its title, subject and deadline, or
// done once it is deleted (by this card or on the page).
func (s *AgentConfirmService) assignmentCard(ctx context.Context, id string) (AgentConfirmCard, error) {
	aid, err := s.cardAssignment(id)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	v, err := s.core.Assignments.Assignment(ctx, aid)
	if errors.Is(err, assignment.ErrDeleted) {
		return deleteAssignmentCard(id, agentStateDone), nil
	}
	if err != nil {
		return AgentConfirmCard{}, s.fail(opDeleteAssignmentCard, err)
	}
	card := deleteAssignmentCard(id, agentStateWaiting)
	card.Title, card.Subject, card.Due, card.Notice = v.Title, v.Subject, timeText(v.Due), msgDeleteWithEvent
	return card, nil
}

// confirmAssignmentDeletion is «Да» on the delete_assignment card: the assignment as it is at the click is deleted
// through the page's DeleteAndStop. A second click finds it deleted and shows the card done; nothing runs twice.
func (s *AgentConfirmService) confirmAssignmentDeletion(ctx context.Context, id string) (AgentConfirmCard, error) {
	aid, err := s.cardAssignment(id)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	v, err := s.core.Assignments.Assignment(ctx, aid)
	if errors.Is(err, assignment.ErrDeleted) {
		return deleteAssignmentCard(id, agentStateDone), nil
	}
	if err != nil {
		return AgentConfirmCard{}, s.fail(opDeleteAssignmentConfirm, err)
	}
	_, err = s.core.Assignments.DeleteAndStop(ctx, aid, v.Version)
	switch {
	case errors.Is(err, assignment.ErrInUse):
		s.log.Info(opDeleteAssignmentConfirm, "result", "in_use")
		return AgentConfirmCard{}, ErrAgentConfirmInUse
	case errors.Is(err, assignment.ErrDeleted):
		return deleteAssignmentCard(id, agentStateDone), nil
	case errors.Is(err, shared.ErrVersionConflict):
		s.log.Info(opDeleteAssignmentConfirm, "result", "changed")
		return AgentConfirmCard{}, ErrAgentConfirmUnavailable
	case err != nil:
		return AgentConfirmCard{}, s.fail(opDeleteAssignmentConfirm, err)
	}
	return deleteAssignmentCard(id, agentStateDone), nil
}

// declineAssignmentDeletion is «Нет»: nothing is stored to decline.
func (s *AgentConfirmService) declineAssignmentDeletion(id string) (AgentConfirmCard, error) {
	if _, err := s.cardAssignment(id); err != nil {
		return AgentConfirmCard{}, err
	}
	return deleteAssignmentCard(id, agentStateDeclined), nil
}

// cardAssignment checks the target of the card; without the deletion wired the kind is not one of this desktop.
func (s *AgentConfirmService) cardAssignment(id string) (shared.AssignmentID, error) {
	aid := shared.AssignmentID(id)
	if aid.Validate() != nil || s.core.Assignments == nil {
		return "", ErrAgentConfirmInvalid
	}
	return aid, nil
}

func deleteAssignmentCard(id, state string) AgentConfirmCard {
	return AgentConfirmCard{
		Kind: agentCardDeleteAssignment, TargetKind: string(values.KindAssignment), TargetID: id, AssignmentID: id, State: state,
	}
}
