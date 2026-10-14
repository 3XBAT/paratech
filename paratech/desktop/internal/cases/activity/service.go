package activity

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
)

// ListLimit bounds each list, so the screen never loads the whole history.
const ListLimit = 100

// Service reads the recordings and jobs the screen shows. It changes nothing.
type Service struct {
	recordings RecordingHistory
	jobs       JobHistory
}

// New builds the Service.
func New(recordings RecordingHistory, jobs JobHistory) (*Service, error) {
	if recordings == nil || jobs == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	return &Service{recordings: recordings, jobs: jobs}, nil
}

// ListRecordings returns the newest recordings, at most ListLimit, newest first.
func (s *Service) ListRecordings(ctx context.Context) ([]Recording, error) {
	recs, err := s.recordings.ListRecent(ctx, ListLimit)
	if err != nil {
		return nil, fmt.Errorf("activity: list recordings: %w", err)
	}
	out := make([]Recording, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordingRow(r))
	}
	return out, nil
}

// ListJobs returns the newest jobs, at most ListLimit, newest first.
func (s *Service) ListJobs(ctx context.Context) ([]Job, error) {
	jobs, err := s.jobs.ListRecent(ctx, ListLimit)
	if err != nil {
		return nil, fmt.Errorf("activity: list jobs: %w", err)
	}
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
