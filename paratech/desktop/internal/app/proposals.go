package app

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// wireProposals builds the action proposal pipeline (PRD section 13.3): the model proposes, the core validates against
// local state, the user confirms, and only then the adapter acts. The mailer is the delivery service (a delivery keyed
// by the command, never resent while its outcome is unknown), the exporter writes through the Storage Manager, the
// recipient comes from the settings, the deadline goes to the app calendar (deadlineCalendar; only this path writes a
// deadline, hard rule 3). The drafts let the agent's send name a draft version and the confirmation show it; the
// waiting proposals let the same deadline proposed again reuse its card (TASK-127). The works and the library let the
// student send the file of a finished work (TASK-180). It needs c.deliveries and c.calendar.
func (c *core) wireProposals(e env, settings settingsLoader, lib *library.Service) error {
	var err error
	assignments := sqlite.NewAssignmentRepository(c.db)
	proposals := sqlite.NewProposalRepository(c.db)
	c.proposals, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: proposals, Assignments: assignments, AssignmentList: assignments,
		Artifacts: c.store, Recipients: recipientSettings{settings}, Mailer: c.deliveries, Exporter: c.store,
		Calendar: deadlineCalendar{c.calendar}, Recordings: sqlite.NewRecordingRepository(c.db),
		Lessons: sqlite.NewLessonRepository(c.db), Clock: e.local, IDs: e.ids, Tx: c.db,
		Drafts: sqlite.NewDraftRepository(c.db), Waiting: proposals,
		Works: workFiles{works: sqlite.NewWorkRepository(c.db), lib: lib},
	})
	if err != nil {
		return fmt.Errorf("app: action proposals: %w", err)
	}
	return nil
}

// workFiles is the homeworkflow.WorkFiles of the send of a work: the stored works and the library.
type workFiles struct {
	works *sqlite.WorkRepository
	lib   *library.Service
}

func (f workFiles) Work(ctx context.Context, id shared.WorkID) (*work.Work, error) {
	return f.works.Get(ctx, id)
}

func (f workFiles) Material(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	return f.lib.Material(ctx, id)
}

func (f workFiles) Subjects(ctx context.Context) ([]*subject.Subject, error) {
	return f.lib.Subjects(ctx)
}
