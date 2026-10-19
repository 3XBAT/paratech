package homeworkflow

import "fmt"

// Service runs the homework workflow (PRD sections 9, 10 and 13.3): the action proposal pipeline (proposals.go) and
// the user's edits of assignments (assignments.go). The post-processing jobs of a transcript run in PostProcessor
// (postprocess.go) and the homework drafts in Drafts (drafts.go), which need other ports.
type Service struct {
	proposals   ProposalRepository
	assignments AssignmentRepository
	list        AssignmentList
	artifacts   ArtifactStore
	recipients  RecipientSource
	mailer      HomeworkMailer
	exporter    HomeworkExporter
	calendar    DeadlineCalendar
	lessons     lessonFinder
	clock       Clock
	ids         IDGenerator
	tx          Tx
}

// New builds the Service.
func New(d Deps) (*Service, error) {
	if d.Proposals == nil || d.Assignments == nil || d.AssignmentList == nil || d.Artifacts == nil ||
		d.Recipients == nil || d.Mailer == nil || d.Exporter == nil || d.Calendar == nil || d.Recordings == nil ||
		d.Lessons == nil || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}

	return &Service{
		proposals: d.Proposals, assignments: d.Assignments, list: d.AssignmentList, artifacts: d.Artifacts,
		recipients: d.Recipients, mailer: d.Mailer, exporter: d.Exporter, calendar: d.Calendar, clock: d.Clock,
		ids: d.IDs, tx: d.Tx, lessons: lessonFinder{recordings: d.Recordings, lessons: d.Lessons, clock: d.Clock},
	}, nil
}
