package activity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// ListLimit bounds the finished (terminal) part of each list, so the screen never loads the whole history.
// Unfinished recordings and jobs are always listed.
const ListLimit = 100

// Service reads the recordings and jobs the screen shows. It changes nothing.
type Service struct {
	recordings RecordingHistory
	jobs       JobHistory
	lessons    Lessons
	hidden     []job.Type
	log        *slog.Logger

	// The screen polls every few seconds; the unreadable ids are logged only when they change, not on every poll.
	mu                sync.Mutex
	lastBadRecordings []shared.RecordingID
	lastBadJobs       []shared.JobID
	lastBadChainJobs  []shared.JobID
	lastBadLessons    []shared.LessonID
}

// New builds the Service. log may be nil: skipped rows are then not reported. hidden are the job types of background
// service work that involves no user, such as the calendar sync (TASK-074): the jobs screen leaves them out. The jobs
// of the processing chain of a recording are left out of the jobs list too: their recording row shows them.
func New(recordings RecordingHistory, jobs JobHistory, lessons Lessons, log *slog.Logger, hidden ...job.Type) (*Service, error) {
	if recordings == nil || jobs == nil || lessons == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{recordings: recordings, jobs: jobs, lessons: lessons, hidden: slices.Clone(hidden), log: log}, nil
}

// ListRecordings returns every unfinished recording plus the newest finished ones (at most ListLimit), newest first,
// each with its lesson title and the status of its whole chain (TASK-076). An unreadable row is logged and left out,
// so it does not hide the others; an unreadable job or lesson only leaves its part of the row out.
func (s *Service) ListRecordings(ctx context.Context) ([]Recording, error) {
	recs, unreadable, err := s.recordings.ListActivity(ctx, ListLimit)
	if err != nil {
		return nil, fmt.Errorf("activity: list recordings: %w", err)
	}
	reportChanged(s, &s.lastBadRecordings, unreadable, "unreadable recordings are left out of the recordings screen")

	ids := make([]shared.RecordingID, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, r.ID())
	}
	chainJobs, badJobs, err := s.jobs.ListForRecordings(ctx, ids, recordingJobTypes(), transcriptJobTypes())
	if err != nil {
		return nil, fmt.Errorf("activity: list the jobs of the recordings: %w", err)
	}
	reportChanged(s, &s.lastBadChainJobs, badJobs, "jobs that cannot be read are left out of the recording statuses")

	titles, err := s.lessonTitles(ctx, recs)
	if err != nil {
		return nil, err
	}

	out := make([]Recording, 0, len(recs))
	for _, r := range recs {
		row := recordingRow(r)
		row.LessonTitle = titles[r.LessonID()]
		row.Chain = chainOf(r, chainJobs[r.ID()])
		out = append(out, row)
	}
	return out, nil
}

// lessonTitles reads the title of every lesson of recs once. A lesson that is gone has no title; one that cannot be
// read is logged (when the set changes) and has no title either, so it does not hide the recordings.
func (s *Service) lessonTitles(ctx context.Context, recs []*recording.Recording) (map[shared.LessonID]string, error) {
	titles := make(map[shared.LessonID]string)
	var bad []shared.LessonID
	for _, r := range recs {
		id := r.LessonID()
		if _, done := titles[id]; done || id == "" {
			continue
		}
		l, err := s.lessons.Get(ctx, id)
		switch {
		case err == nil:
			titles[id] = l.Content().Title
		case ctx.Err() != nil:
			return nil, fmt.Errorf("activity: read lesson %s: %w", id, ctx.Err())
		case errors.Is(err, shared.ErrNotFound):
			titles[id] = ""
		default:
			titles[id] = ""
			bad = append(bad, id)
		}
	}
	reportChanged(s, &s.lastBadLessons, bad, "lessons that cannot be read leave their recordings without a title")
	return titles, nil
}

// reportChanged logs ids under msg when they differ from *last; a poll with the same ids stays quiet.
func reportChanged[T comparable](s *Service, last *[]T, ids []T, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Equal(ids, *last) {
		return
	}
	*last = slices.Clone(ids)
	if len(ids) > 0 {
		s.log.Warn(msg, slog.Any("ids", ids))
	}
}

// ListJobs returns every unfinished job plus the newest finished ones (at most ListLimit), newest first, without the
// hidden types and the chain types (their recording rows show them, TASK-076). An unreadable row is logged and left out, so it does not hide the others.
func (s *Service) ListJobs(ctx context.Context) ([]Job, error) {
	jobs, unreadable, err := s.jobs.ListActivity(ctx, ListLimit, append(slices.Clone(s.hidden), chainJobTypes()...))
	if err != nil {
		return nil, fmt.Errorf("activity: list jobs: %w", err)
	}
	reportChanged(s, &s.lastBadJobs, unreadable, "unreadable jobs are left out of the jobs screen")
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobRow(j))
	}
	return out, nil
}

func recordingRow(r *recording.Recording) Recording {
	d, ok := r.Duration()
	return Recording{
		ID: r.ID(), LessonID: r.LessonID(), Status: r.Status(), StartedAt: r.StartedAt(), StoppedAt: r.StoppedAt(),
		Duration: d, HasDuration: ok, Problem: r.Problem(),
		Stoppable: recording.CanTransition(r.Status(), recording.StatusStopping),
	}
}

func jobRow(j *job.Job) Job {
	return Job{
		ID: j.ID(), Type: j.Type(), Status: j.Status(), Waiting: j.Status().IsWaiting(),
		Attempts: j.Attempts(), MaxAttempts: j.MaxAttempts(), Percent: j.Progress().Percent(), LastError: j.LastError(),
		CreatedAt: j.CreatedAt(), UpdatedAt: j.UpdatedAt(), NextRunAt: j.NextRunAt(),
		RunNowOffered: j.Status().OffersRunNow(),
	}
}
