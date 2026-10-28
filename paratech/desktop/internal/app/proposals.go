package app

import (
	"context"
	"fmt"
	"log/slog"

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
// recipient is a teacher of the library subject (ADR-0030), the deadline goes to the app calendar (deadlineCalendar;
// only this path writes a deadline, hard rule 3). The drafts let the agent's send name a draft version and the confirmation show it; the
// waiting proposals let the same deadline proposed again reuse its card (TASK-127). The works and the library let the
// student send the file of a finished work (TASK-180). «Удалить» of an assignment queues the removal of its deadline
// event for the calendar sync (ADR-0029). It needs c.deliveries and c.calendar.
func (c *core) wireProposals(e env, lib *library.Service, log *slog.Logger) error {
	var err error
	assignments := sqlite.NewAssignmentRepository(c.db)
	proposals := sqlite.NewProposalRepository(c.db)
	c.proposals, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: proposals, Assignments: assignments, AssignmentList: assignments,
		Artifacts: c.store, Recipients: subjectTeachers{lib}, Mailer: c.deliveries, Exporter: c.store,
		Calendar: deadlineCalendar{c.calendar}, Recordings: sqlite.NewRecordingRepository(c.db),
		Lessons: sqlite.NewLessonRepository(c.db), Clock: e.local, IDs: e.ids, Tx: c.db,
		Drafts: sqlite.NewDraftRepository(c.db), Waiting: proposals,
		Works:    workFiles{works: sqlite.NewWorkRepository(c.db), lib: lib},
		Subjects: subjectNames{lib},
		Removals: sqlite.NewDeadlineEventRemovals(c.db), Sync: calendarRequest{c: c, log: log, why: "assignment deletion"},
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

// subjectNames is the homeworkflow.SubjectNames of an assignment the student adds (ADR-0029): the current name of a
// library subject; lookup only.
type subjectNames struct{ lib *library.Service }

func (n subjectNames) SubjectName(ctx context.Context, id shared.SubjectID) (string, error) {
	list, err := n.lib.Subjects(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range list {
		if s.ID() == id {
			return s.Name().String(), nil
		}
	}
	return "", fmt.Errorf("app: subject %s: %w", id, shared.ErrNotFound)
}
