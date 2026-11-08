package transcribe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/services/retranscribe"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names used in errors.
const (
	opActiveJob    = "transcribe: find the unfinished stt job"
	opRetranscribe = "transcribe: start the transcription again"
)

// ActiveJob returns the unfinished (queued, waiting or running) STT job of the recording, the one «Отменить
// расшифровку» cancels (TASK-392); ok is false when there is none.
func (e *Enqueuer) ActiveJob(ctx context.Context, id shared.RecordingID) (jobID shared.JobID, ok bool, err error) {
	jobs, err := e.jobs.ListByKeyPrefix(ctx, JobType, keyPrefix(id))
	if err != nil {
		return "", false, fmt.Errorf("%s %s: %w", opActiveJob, id, err)
	}
	j, ok := retranscribe.Unfinished(sttJobs(jobs))
	return j.ID, ok, nil
}

// Retranscribe creates a new STT job of a recording whose transcription was cancelled («Расшифровать снова»,
// TASK-392). In one transaction it reads the recording, its STT jobs and its transcript, lets retranscribe.Policy
// decide, pins the current model and stores the job keyed "stt:<recording>:<model>:r<n>". It refuses with
// ErrNotAllowed (also for a recording that is gone or cannot be transcribed) and with ErrNoModel; a repeated call
// finds the new job unfinished and refuses, so a double click makes one job.
func (e *Enqueuer) Retranscribe(ctx context.Context, id shared.RecordingID) (shared.JobID, error) {
	var (
		jobID shared.JobID
		model shared.SttModelID
	)
	err := e.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := e.recordings.Get(ctx, id)
		if errors.Is(err, shared.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotAllowed, err)
		}
		if err != nil {
			return err
		}
		if _, ok := rec.TranscribableAudio(); !ok {
			return fmt.Errorf("%w: the recording cannot be transcribed", ErrNotAllowed)
		}

		jobs, err := e.jobs.ListByKeyPrefix(ctx, JobType, keyPrefix(id))
		if err != nil {
			return err
		}
		has, err := e.hasTranscript(ctx, id)
		if err != nil {
			return err
		}
		d, err := e.policy.Decide(id, sttJobs(jobs), has)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNotAllowed, err)
		}

		model, err = e.models.ResolveForJob(ctx, "")
		if errors.Is(err, modelselection.ErrModelNotInstalled) || errors.Is(err, modelselection.ErrNoDefaultModel) {
			return fmt.Errorf("%w: %w", ErrNoModel, err)
		}
		if err != nil {
			return err
		}

		req, err := values.NewSTTRequest(id)
		if err != nil {
			return err
		}
		j, err := job.New(job.Spec{
			ID: e.ids.NewJobID(), Type: JobType, Key: d.Key(model), Payload: req.Payload(), PinnedModelID: model,
			MaxAttempts: e.maxAttempts,
		}, e.clock.Now())
		if err != nil {
			return err
		}
		jobID, _, err = e.jobs.Enqueue(ctx, j)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", opRetranscribe, id, err)
	}
	e.log.Info("transcription started again", slog.String("recording", id.String()), slog.String("job", jobID.String()),
		slog.String("model", string(model)))
	return jobID, nil
}

func (e *Enqueuer) hasTranscript(ctx context.Context, id shared.RecordingID) (bool, error) {
	_, err := e.transcripts.LatestByRecording(ctx, id)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	}
	return false, err
}

// sttJobs maps the jobs to what the policy reads of them.
func sttJobs(jobs []*job.Job) []retranscribe.Job {
	out := make([]retranscribe.Job, 0, len(jobs))
	for _, j := range jobs {
		s := retranscribe.StateFinished
		switch {
		case j.Status() == job.StatusCancelled:
			s = retranscribe.StateCancelled
		case !j.Status().IsTerminal():
			s = retranscribe.StateUnfinished
		}
		out = append(out, retranscribe.Job{ID: j.ID(), Created: j.CreatedAt(), State: s})
	}
	return out
}
