package recordingdelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	coursework "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// Operation names, for error texts and logs.
const (
	opPreview = "recordingdelete: preview"
	opDelete  = "recordingdelete: delete"
	opResume  = "recordingdelete: resume"
)

// The job types that work for a recording: by the recording, by one of its transcripts, by a chosen assignment.
func recordingJobTypes() []job.Type { return []job.Type{transcription.STTJobType} }

func transcriptJobTypes() []job.Type {
	return []job.Type{coursework.SummarizeJobType, coursework.ExtractHomeworkJobType, coursework.LectureTextJobType}
}

func assignmentJobTypes() []job.Type { return []job.Type{coursework.HomeworkDraftJobType} }

// Service deletes recordings; see the package documentation for the order of the steps.
type Service struct {
	recordings RecordingRepository
	store      DeletionStore
	jobs       JobFinder
	canceller  JobCanceller
	artifacts  ArtifactStore
	recovery   RecoveryGuard
	clock      Clock
	sleeper    Sleeper
	tx         Tx
	sync       SyncRequester
	log        *slog.Logger
	cfg        Config
}

// New checks the ports and the configuration.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Recordings == nil || d.Store == nil || d.Jobs == nil || d.Canceller == nil || d.Artifacts == nil ||
		d.Recovery == nil || d.Clock == nil || d.Sleeper == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port except Sync and Log is required", ErrInvalidConfig)
	}
	if err := recording.ValidateTrackExtension(cfg.TrackExtension); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if cfg.Wait < 0 || cfg.Poll < 0 {
		return nil, fmt.Errorf("%w: negative wait %v or poll %v", ErrInvalidConfig, cfg.Wait, cfg.Poll)
	}
	if cfg.Wait == 0 {
		cfg.Wait = DefaultWait
	}
	if cfg.Poll == 0 {
		cfg.Poll = DefaultPoll
	}
	s := &Service{
		recordings: d.Recordings, store: d.Store, jobs: d.Jobs, canceller: d.Canceller, artifacts: d.Artifacts,
		recovery: d.Recovery, clock: d.Clock, sleeper: d.Sleeper, tx: d.Tx, sync: d.Sync, log: d.Log, cfg: cfg,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Preview returns what deleting the recording removes, for the confirmation. A recording already being deleted is
// ErrNotFound: it is gone from the lists.
func (s *Service) Preview(ctx context.Context, id shared.RecordingID) (Preview, error) {
	if err := id.Validate(); err != nil {
		return Preview{}, fmt.Errorf("%s: %w: %w", opPreview, ErrInvalidRequest, err)
	}
	rec, err := s.get(ctx, id)
	if err != nil {
		return Preview{}, fmt.Errorf("%s %s: %w", opPreview, id, err)
	}
	if rec.Deleting() {
		return Preview{}, fmt.Errorf("%s %s: %w", opPreview, id, ErrNotFound)
	}

	contents, err := s.store.Contents(ctx, id)
	if err != nil {
		return Preview{}, fmt.Errorf("%s %s: %w: %w", opPreview, id, ErrStorage, err)
	}
	jobs, err := s.jobsOf(ctx, rec)
	if err != nil {
		return Preview{}, fmt.Errorf("%s %s: %w", opPreview, id, err)
	}
	contents.Sound, contents.Jobs = rec.HasSound(), len(unfinished(jobs))

	p := Preview{Recording: id, Title: rec.LessonSnapshot().Title(), StartedAt: rec.StartedAt(), Contents: contents}
	switch {
	case rec.CheckDeletable() != nil:
		p.Refusal = RefusalInProgress
	case s.recovery.Recovering(id):
		p.Refusal = RefusalRecovering
	}
	return p, nil
}

// Delete deletes the recording with the chosen found assignments. A recording in progress is refused with
// ErrInProgress, one the recovery is assembling with ErrRecovering, a chosen assignment that is not one of the
// recording with ErrInvalidRequest; nothing is changed then. Deleting a recording already marked (a second request,
// or after a crash) goes on with its first choice of assignments. OutcomePending means the recording is marked and
// hidden and the next start finishes the deletion.
func (s *Service) Delete(ctx context.Context, req Request) (Result, error) {
	id := req.Recording
	if err := id.Validate(); err != nil {
		return Result{}, fmt.Errorf("%s: %w: %w", opDelete, ErrInvalidRequest, err)
	}
	if err := s.mark(ctx, req); err != nil {
		return Result{}, fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	res, err := s.finish(ctx, id, s.cfg.Wait)
	if err != nil {
		return Result{}, fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	return res, nil
}

// DeleteMany deletes several recordings, one after the other, under one confirmation. A recording that fails is
// reported in Failures and the others go on; at most MaxBatch recordings, each once.
func (s *Service) DeleteMany(ctx context.Context, reqs []Request) (Batch, error) {
	if len(reqs) > MaxBatch {
		return Batch{}, fmt.Errorf("%s: %w: %d recordings, at most %d", opDelete, ErrInvalidRequest, len(reqs), MaxBatch)
	}
	var out Batch
	seen := make(map[shared.RecordingID]bool, len(reqs))
	for _, req := range reqs {
		if seen[req.Recording] {
			continue
		}
		seen[req.Recording] = true
		if err := ctx.Err(); err != nil {
			out.Failures = append(out.Failures, Failure{Recording: req.Recording, Err: err})
			continue
		}
		res, err := s.Delete(ctx, req)
		if err != nil {
			out.Failures = append(out.Failures, Failure{Recording: req.Recording, Err: err})
			continue
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

// Resume finishes the deletions a crash or a busy job left behind (the app calls it at the start, after the job
// recovery and before the recording recovery and the workers). It does not wait for running jobs: what cannot finish
// now stays marked for the next start. It returns how many recordings it deleted.
func (s *Service) Resume(ctx context.Context) (int, error) {
	ids, err := s.recordings.ListDeleting(ctx)
	if err != nil {
		return 0, fmt.Errorf("%s: %w: %w", opResume, ErrStorage, err)
	}
	var errs []error
	n := 0
	for _, id := range ids {
		res, err := s.finish(ctx, id, 0)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		case res.Outcome == OutcomeDeleted:
			n++
		}
	}
	if err := errors.Join(errs...); err != nil {
		return n, fmt.Errorf("%s: %w", opResume, err)
	}
	return n, nil
}

// mark stores the deletion mark with the choice of assignments, unless the recording is already marked.
func (s *Service) mark(ctx context.Context, req Request) error {
	if s.recovery.Recovering(req.Recording) {
		return ErrRecovering
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := s.get(ctx, req.Recording)
		if err != nil || rec.Deleting() {
			return err
		}
		if err := rec.CheckDeletable(); err != nil {
			return fmt.Errorf("%w: %w", ErrInProgress, err)
		}
		contents, err := s.store.Contents(ctx, req.Recording)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrStorage, err)
		}
		if err := deletion.CheckChoice(contents.Assignments, req.Assignments); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		if err := rec.MarkDeleting(s.clock.Now(), req.Assignments); err != nil {
			return mapMarkError(err)
		}
		if err := s.recordings.Save(ctx, rec); err != nil {
			return fmt.Errorf("%w: %w", ErrStorage, err)
		}
		return nil
	})
}

func mapMarkError(err error) error {
	switch {
	case errors.Is(err, recording.ErrNotDeletable):
		return fmt.Errorf("%w: %w", ErrInProgress, err)
	case errors.Is(err, recording.ErrInvalidRecording):
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return err
}
