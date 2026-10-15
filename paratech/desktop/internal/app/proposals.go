package app

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/proposal"
)

// calendarNotConnected is the homeworkflow.DeadlineCalendar stand-in until Google Calendar is wired (TASK-051): it
// never acts and never reports a success, so a confirmed create_deadline_event proposal stays CONFIRMED.
type calendarNotConnected struct{}

func (calendarNotConnected) UpsertDeadline(_ context.Context, cmd proposal.CalendarCommand) error {
	return fmt.Errorf("app: deadline of %s: %w", cmd.AssignmentID(), homeworkflow.ErrCalendarNotConnected)
}

// wireProposals builds the action proposal pipeline (PRD section 13.3): the model proposes, the core validates against
// local state, the user confirms, and only then the adapter acts. The mailer is the delivery service (a delivery keyed
// by the command, never resent while its outcome is unknown), the exporter writes through the Storage Manager, the
// recipient comes from the settings. It needs c.deliveries.
func (c *core) wireProposals(e env, settings settingsLoader) error {
	var err error
	c.proposals, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: sqlite.NewProposalRepository(c.db), Assignments: sqlite.NewAssignmentRepository(c.db),
		Artifacts: c.store, Recipients: recipientSettings{settings}, Mailer: c.deliveries, Exporter: c.store,
		Calendar: calendarNotConnected{}, Clock: e.local, IDs: e.ids, Tx: c.db,
	})
	if err != nil {
		return fmt.Errorf("app: action proposals: %w", err)
	}
	return nil
}
