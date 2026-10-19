package activity

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// WithDraftAssignments makes ListJobs name the assignment of each homework draft job (TASK-094). Call it once, before
// the Service is used; without it the rows carry only the assignment id.
func (s *Service) WithDraftAssignments(d DraftAssignments) *Service {
	s.drafts = d
	return s
}

// nameDrafts fills the assignment of the draft rows: the id from the job payload, the title and the subject from the
// homework use case, read once per assignment. A gone or unreadable assignment leaves its row without a name.
func (s *Service) nameDrafts(ctx context.Context, rows []Job, jobs []*job.Job) {
	type name struct{ title, subject string }
	names := map[shared.AssignmentID]name{}
	for i, j := range jobs {
		if j.Type() != values.HomeworkDraftJobType {
			continue
		}
		req, err := values.ParseDraftPayload(j.Payload())
		if err != nil {
			continue
		}
		a := req.Assignment()
		rows[i].AssignmentID = a
		if s.drafts == nil {
			continue
		}
		n, ok := names[a]
		if !ok {
			title, subject, err := s.drafts.DraftAssignment(ctx, a)
			if err != nil && !errors.Is(err, shared.ErrNotFound) {
				s.log.Debug("the assignment of a draft job cannot be read", slog.String("job", j.ID().String()), slog.Any("error", err))
			}
			n = name{title: title, subject: subject}
			if err != nil {
				n = name{}
			}
			names[a] = n
		}
		rows[i].AssignmentTitle, rows[i].AssignmentSubject = n.title, n.subject
	}
}
