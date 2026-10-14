package activity

import (
	"context"
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
	log        *slog.Logger

	// The screen polls every few seconds; the unreadable ids are logged only when they change, not on every poll.
	mu                sync.Mutex
	lastBadRecordings []shared.RecordingID
	lastBadJobs       []shared.JobID
}

// New builds the Service. log may be nil: skipped rows are then not reported.
func New(recordings RecordingHistory, jobs JobHistory, log *slog.Logger) (*Service, error) {
	if recordings == nil || jobs == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{recordings: recordings, jobs: jobs, log: log}, nil
}

// ListRecordings returns every unfinished recording plus the newest finished ones (at most ListLimit), newest first.
// An unreadable row is logged and left out, so it does not hide the others.
func (s *Service) ListRecordings(ctx context.Context) ([]Recording, error) {
	recs, unreadable, err := s.recordings.ListActivity(ctx, ListLimit)
	if err != nil {
		return nil, fmt.Errorf("activity: list recordings: %w", err)
	}
	s.mu.Lock()
	if !slices.Equal(unreadable, s.lastBadRecordings) {
		s.lastBadRecordings = slices.Clone(unreadable)
		if len(unreadable) > 0 {
			s.log.Warn("unreadable recordings are left out of the recordings screen", slog.Any("ids", unreadable))
		}
	}
	s.mu.Unlock()
	out := make([]Recording, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordingRow(r))
	}
	return out, nil
}

// ListJobs returns every unfinished job plus the newest finished ones (at most ListLimit), newest first. An
// unreadable row is logged and left out, so it does not hide the others.
func (s *Service) ListJobs(ctx context.Context) ([]Job, error) {
	jobs, unreadable, err := s.jobs.ListActivity(ctx, ListLimit)
	if err != nil {
		return nil, fmt.Errorf("activity: list jobs: %w", err)
	}
	s.mu.Lock()
	if !slices.Equal(unreadable, s.lastBadJobs) {
		s.lastBadJobs = slices.Clone(unreadable)
		if len(unreadable) > 0 {
			s.log.Warn("unreadable jobs are left out of the jobs screen", slog.Any("ids", unreadable))
		}
	}
	s.mu.Unlock()
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
