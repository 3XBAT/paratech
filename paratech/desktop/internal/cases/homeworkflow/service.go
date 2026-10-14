package homeworkflow

import "fmt"

// Service runs the homework workflow (PRD sections 9, 10 and 13.3). This task adds the action proposal pipeline
// (proposals.go); post-processing (TASK-042) and drafts (TASK-043) extend it.
type Service struct {
	proposals   ProposalRepository
	assignments AssignmentRepository
	artifacts   ArtifactStore
	recipients  RecipientSource
	mailer      HomeworkMailer
	exporter    HomeworkExporter
	calendar    DeadlineCalendar
	clock       Clock
	ids         IDGenerator
	tx          Tx
}

// New builds the Service.
func New(d Deps) (*Service, error) {
	if d.Proposals == nil || d.Assignments == nil || d.Artifacts == nil || d.Recipients == nil || d.Mailer == nil ||
		d.Exporter == nil || d.Calendar == nil || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}

	return &Service{
		proposals: d.Proposals, assignments: d.Assignments, artifacts: d.Artifacts, recipients: d.Recipients,
		mailer: d.Mailer, exporter: d.Exporter, calendar: d.Calendar, clock: d.Clock, ids: d.IDs, tx: d.Tx,
	}, nil
}
