package transcribe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names used in errors.
const (
	opEnqueue = "transcribe: enqueue stt job"
	opSweep   = "transcribe: enqueue pending recordings"
)

// Enqueuer creates the STT job of a completed recording (PRD sections 7.1 and 8). RecordingCompleted satisfies the
// completion listener of the recording session structurally (ARCH-20); EnqueuePending is the startup sweep that
// catches up on completions a crash lost.
//
// The job pins the model chosen by ModelResolver and has the idempotency key "stt:<recording id>:<model id>". A
// recording gets one automatic STT job: when any STT job of the recording exists (whatever its model or status),
// nothing is created, so a repeated event or a sweep after the default model changed adds no second transcription.
// The recording aggregate decides whether it can be transcribed (recording.TranscribableAudio, ARCH-22). Without an
// installed model no job is created and the log says why; the next startup sweep tries again.
type Enqueuer struct {
	recordings  RecordingReader
	pending     PendingRecordings
	models      ModelResolver
	jobs        JobEnqueuer
	ids         IDGenerator
	tx          Tx
	clock       Clock
	log         *slog.Logger
	maxAttempts int
}

// NewEnqueuer checks that every port is given.
func NewEnqueuer(d EnqueueDeps) (*Enqueuer, error) {
	if d.Recordings == nil || d.Pending == nil || d.Models == nil || d.Jobs == nil || d.IDs == nil || d.Tx == nil ||
		d.Clock == nil {
		return nil, fmt.Errorf("%w: every enqueue port except the logger is required", ErrInvalidConfig)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	e := &Enqueuer{
		recordings: d.Recordings, pending: d.Pending, models: d.Models, jobs: d.Jobs, ids: d.IDs, tx: d.Tx,
		clock: d.Clock, log: d.Log, maxAttempts: d.MaxAttempts,
	}
	if e.maxAttempts == 0 {
		e.maxAttempts = backoff.DefaultSpec().MaxAttempts
	}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	return e, nil
}

// outcome is what one enqueue did.
type outcome int

const (
	outcomeCreated  outcome = iota + 1
	outcomeExisting         // the recording has an STT job already
	outcomeSkipped          // gone, or not transcribable
	outcomeNoModel          // no model to pin; nothing created
)

// RecordingCompleted enqueues the STT job of the recording (the recording session calls it after the COMPLETED save).
// A missing model is not an error: the recording stays as it is and the log says that transcription waits.
func (e *Enqueuer) RecordingCompleted(ctx context.Context, id shared.RecordingID) error {
	_, err := e.enqueue(ctx, id)
	return err
}

// EnqueuePending runs the enqueue for every COMPLETED recording without a transcript, page by page, and returns how
// many jobs it created. It is idempotent like RecordingCompleted. Without an installed model it stops at the first
// recording, which already logged why. A recording that fails does not stop the others; the error names how many
// failed and the first cause.
func (e *Enqueuer) EnqueuePending(ctx context.Context) (int, error) {
	var (
		created, failed int
		first           error
		after           shared.RecordingID
	)
	for {
		ids, err := e.pending.ListUntranscribed(ctx, after, sweepPage)
		if err != nil {
			return created, fmt.Errorf("%s: %w", opSweep, err)
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return created, fmt.Errorf("%s: %w", opSweep, err)
			}
			out, err := e.enqueue(ctx, id)
			switch {
			case err != nil:
				failed++
				if first == nil {
					first = err
				}
			case out == outcomeCreated:
				created++
			case out == outcomeNoModel:
				return created, nil
			}
		}
		if len(ids) < sweepPage {
			break
		}
		after = ids[len(ids)-1]
	}
	if first != nil {
		return created, fmt.Errorf("%s: %d recordings: %w", opSweep, failed, first)
	}
	return created, nil
}

// enqueue reads the recording, picks the model and stores the job in one transaction.
func (e *Enqueuer) enqueue(ctx context.Context, id shared.RecordingID) (outcome, error) {
	var (
		out   outcome
		jobID shared.JobID
		model shared.SttModelID
		why   error
	)
	err := e.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := e.recordings.Get(ctx, id)
		if errors.Is(err, shared.ErrNotFound) {
			out = outcomeSkipped // deleted meanwhile
			return nil
		}
		if err != nil {
			return err
		}
		if _, ok := rec.TranscribableAudio(); !ok {
			out = outcomeSkipped
			return nil
		}

		has, err := e.jobs.HasKeyPrefix(ctx, JobType, keyPrefix(id))
		if err != nil {
			return err
		}
		if has {
			out = outcomeExisting
			return nil
		}

		model, err = e.models.ResolveForJob(ctx, "")
		if errors.Is(err, modelselection.ErrModelNotInstalled) || errors.Is(err, modelselection.ErrNoDefaultModel) {
			out, why = outcomeNoModel, err
			return nil
		}
		if err != nil {
			return err
		}

		req, err := values.NewSTTRequest(id)
		if err != nil {
			return err
		}
		j, err := job.New(job.Spec{
			ID: e.ids.NewJobID(), Type: JobType, Key: jobKey(id, model), Payload: req.Payload(), PinnedModelID: model,
			MaxAttempts: e.maxAttempts,
		}, e.clock.Now())
		if err != nil {
			return err
		}
		var created bool
		if jobID, created, err = e.jobs.Enqueue(ctx, j); err != nil {
			return err
		}
		out = outcomeExisting
		if created {
			out = outcomeCreated
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opEnqueue, id, err)
	}
	e.logOutcome(id, out, jobID, model, why)
	return out, nil
}

func (e *Enqueuer) logOutcome(id shared.RecordingID, out outcome, jobID shared.JobID, model shared.SttModelID, why error) {
	rec := slog.String("recording", id.String())
	switch {
	case out == outcomeCreated:
		e.log.Info("transcription job enqueued", rec, slog.String("job", jobID.String()), slog.String("model", string(model)))
	case out == outcomeNoModel && errors.Is(why, modelselection.ErrNoDefaultModel):
		e.log.Warn("transcription waits: no default speech recognition model selected", rec)
	case out == outcomeNoModel:
		e.log.Warn("transcription waits: no speech recognition model installed", rec)
	case out == outcomeExisting:
		e.log.Debug("transcription job exists", rec)
	default:
		e.log.Debug("recording not transcribed", rec)
	}
}

// keyPrefix is the start of the key of every STT job of the recording; ids cannot contain ':' (they are file-name
// segments), so it names exactly one recording.
func keyPrefix(id shared.RecordingID) string { return fmt.Sprintf("%s:%s:", JobType, id) }

// jobKey is the idempotency key of the STT job of the recording pinned to the model.
func jobKey(id shared.RecordingID, model shared.SttModelID) string {
	return keyPrefix(id) + string(model)
}
