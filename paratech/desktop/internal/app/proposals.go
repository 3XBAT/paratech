package app

import (
	"fmt"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
)

// wireProposals builds the action proposal pipeline (PRD section 13.3): the model proposes, the core validates against
// local state, the user confirms, and only then the adapter acts. The mailer is the delivery service (a delivery keyed
// by the command, never resent while its outcome is unknown), the exporter writes through the Storage Manager, the
// recipient comes from the settings, the deadline goes to the app calendar (deadlineCalendar; only this path writes a
// deadline, hard rule 3). It needs c.deliveries and c.calendar.
func (c *core) wireProposals(e env, settings settingsLoader) error {
	var err error
	c.proposals, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: sqlite.NewProposalRepository(c.db), Assignments: sqlite.NewAssignmentRepository(c.db),
		Artifacts: c.store, Recipients: recipientSettings{settings}, Mailer: c.deliveries, Exporter: c.store,
		Calendar: deadlineCalendar{c.calendar}, Clock: e.local, IDs: e.ids, Tx: c.db,
	})
	if err != nil {
		return fmt.Errorf("app: action proposals: %w", err)
	}
	return nil
}
