package homeworkflow

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

const (
	opDeadlineEvent   = "homeworkflow: deadline event of an assignment"
	opAssignmentState = "homeworkflow: state of an assignment"
)

// maxEventScan bounds the executed, confirmed and stale proposals looked through for a deadline event.
const maxEventScan = 1000

// HasDeadlineEvent tells from the local projection whether assignment id has a deadline event in Google Calendar: a
// deadline proposal of it was executed, or confirmed and maybe executed (a crash or a failed calendar call), or went
// STALE (a confirmed one may have made the event before), so the agent asks «Да» before deleting it (ADR-0033 О-6 б).
// Nothing is read from Google. An event the calendar sync removed after a delete and a restore still counts, as does
// every assignment when a list was cut at maxEventScan: the answer errs towards «Да». Errors: no Waiting port
// (ErrInvalidConfig) or the store could not be read.
func (s *Service) HasDeadlineEvent(ctx context.Context, id shared.AssignmentID) (bool, error) {
	if s.waiting == nil {
		return false, fmt.Errorf("%s: %w: no proposal list", opDeadlineEvent, ErrInvalidConfig)
	}
	for _, st := range []proposal.Status{proposal.StatusExecuted, proposal.StatusConfirmed, proposal.StatusStale} {
		list, err := s.waiting.ListByStatus(ctx, st, maxEventScan)
		if err != nil {
			return false, fmt.Errorf("%s %s: %w", opDeadlineEvent, id, err)
		}
		if len(list) >= maxEventScan {
			return true, nil
		}
		for _, p := range list {
			if p.Kind() == proposal.KindDeadlineEvent && p.AssignmentID() == id {
				return true, nil
			}
		}
	}
	return false, nil
}

// AssignmentState returns an assignment also when it is deleted (AssignmentView.Deleted), for the agent's Undo and
// crash recovery of its delete and restore, which compare versions. No reader shows it: every other reader skips a
// deleted assignment (ADR-0029). A missing one wraps shared.ErrNotFound.
func (s *Service) AssignmentState(ctx context.Context, id shared.AssignmentID) (AssignmentView, error) {
	a, err := s.assignments.Get(ctx, id)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s %s: %w", opAssignmentState, id, err)
	}
	v, err := s.view(ctx, s.lessons, a)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s %s: %w", opAssignmentState, id, err)
	}
	return v, nil
}
