package recordingdelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// finish runs the steps after the mark: stop the jobs (waiting at most wait), delete the files, purge the rows. A
// recording that is gone was deleted meanwhile (another request or the resume).
func (s *Service) finish(ctx context.Context, id shared.RecordingID, wait time.Duration) (Result, error) {
	gone := Result{Recording: id, Outcome: OutcomeDeleted}
	pending := Result{Recording: id, Outcome: OutcomePending}

	rec, jobs, err := s.stopJobs(ctx, id, wait)
	switch {
	case errors.Is(err, ErrNotFound):
		return gone, nil
	case errors.Is(err, errBusy):
		s.log.Warn("recording deletion waits for its jobs; the next start finishes it", slog.String("recording", id.String()))
		return pending, nil
	case err != nil:
		return Result{}, err
	case !rec.Deleting():
		return Result{}, fmt.Errorf("%w: not marked for deletion", ErrNotFound)
	}

	if err := s.deleteFiles(ctx, rec, jobs); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		s.log.Warn("recording files not deleted; the next start tries again", slog.String("recording", id.String()),
			slog.Any("error", err))
		return pending, nil
	}

	res, err := s.purge(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		return gone, nil
	case errors.Is(err, errBusy):
		s.log.Warn("a job of the recording started again; the next start finishes the deletion",
			slog.String("recording", id.String()))
		return pending, nil
	case err != nil:
		return Result{}, err
	}
	s.log.Info("recording deleted", slog.String("recording", id.String()), slog.Int("assignments", len(res.Deleted)),
		slog.Int("assignments_kept", len(res.Kept)))

	// The deadline events of the deleted assignments are removed by the calendar sync; a lost request is repaired by
	// the next sync, the removals stay stored until then.
	if len(res.Deleted) > 0 && s.sync != nil {
		s.sync.RequestSync(ctx)
	}
	return res, nil
}

// stopJobs cancels the unfinished jobs of the marked recording and looks again until none is left, at least twice and
// for at most wait. A job claimed or enqueued meanwhile is found by the next look. It returns the recording and its
// jobs, all finished, or errBusy.
func (s *Service) stopJobs(ctx context.Context, id shared.RecordingID, wait time.Duration) (
	*recording.Recording, []*job.Job, error) {
	deadline := s.clock.Now().Add(wait)
	for look := 0; ; look++ {
		rec, err := s.get(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		jobs, err := s.jobsOf(ctx, rec)
		if err != nil {
			return nil, nil, err
		}
		active := unfinished(jobs)
		if len(active) == 0 {
			return rec, jobs, nil
		}
		if look > 0 && !s.clock.Now().Before(deadline) {
			return nil, nil, errBusy
		}

		for _, j := range active {
			// Refused now (claimed a moment ago, an attempt ending): the next look tries again.
			if err := s.canceller.Cancel(ctx, j.ID()); err != nil {
				s.log.Debug("job not cancelled yet", slog.String("job", j.ID().String()), slog.Any("error", err))
			}
		}
		if look > 0 {
			if err := s.sleeper.Sleep(ctx, s.cfg.Poll); err != nil {
				return nil, nil, err
			}
		}
	}
}

// deleteFiles removes the chunks, the track and the transcript documents of the recording. Every delete is idempotent.
func (s *Service) deleteFiles(ctx context.Context, rec *recording.Recording, jobs []*job.Job) error {
	docs, err := s.store.Files(ctx, rec.ID())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStorage, err)
	}

	files := []shared.ArtifactRef{rec.ChunksDir()}
	if a := rec.Audio(); !a.IsZero() {
		files = append(files, a)
	}
	// A track written just before a crash may exist without being recorded on the aggregate.
	if track, err := rec.TrackRef(s.cfg.TrackExtension); err == nil {
		files = append(files, track)
	}
	files = append(files, docs...)
	// The document of an STT job is written before its transcript row, so a cancelled job may leave one without a row.
	for _, j := range jobs {
		if j.Type() != transcription.STTJobType {
			continue
		}
		if ref, err := transcript.DocumentRef(shared.TranscriptID(j.ID())); err == nil {
			files = append(files, ref)
		}
	}

	seen := make(map[shared.ArtifactRef]bool, len(files))
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		if err := s.artifacts.Delete(ctx, f); err != nil {
			return fmt.Errorf("%w: %w", ErrStorage, err)
		}
	}
	return nil
}

// purge removes the rows in one transaction, after checking again that no job of the recording is unfinished: the
// write transaction keeps a job from being claimed or enqueued until the commit.
func (s *Service) purge(ctx context.Context, id shared.RecordingID) (Result, error) {
	res := Result{Recording: id, Outcome: OutcomeDeleted}
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := s.get(ctx, id)
		if err != nil {
			return err
		}
		jobs, err := s.jobsOf(ctx, rec)
		if err != nil {
			return err
		}
		if len(unfinished(jobs)) > 0 {
			return errBusy
		}
		contents, err := s.store.Contents(ctx, id)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrStorage, err)
		}

		res.Deleted = deletion.Deletable(contents.Assignments, rec.DeletingAssignments())
		res.Kept = kept(contents.Assignments, res.Deleted)
		ids := make([]shared.JobID, len(jobs))
		for i, j := range jobs {
			ids[i] = j.ID()
		}
		if err := s.store.Purge(ctx, rec, res.Deleted, ids, s.clock.Now()); err != nil {
			return fmt.Errorf("%w: %w", ErrStorage, err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// get loads a recording; a missing one is ErrNotFound, other failures ErrStorage.
func (s *Service) get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	rec, err := s.recordings.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	return rec, nil
}

// jobsOf returns the jobs of the recording, of its transcripts and of the assignments chosen with it. Rows that cannot
// be read are left as they are.
func (s *Service) jobsOf(ctx context.Context, rec *recording.Recording) ([]*job.Job, error) {
	byRecording, bad, err := s.jobs.ListForRecordings(ctx, []shared.RecordingID{rec.ID()}, recordingJobTypes(),
		transcriptJobTypes())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	out := byRecording[rec.ID()]
	if ids := rec.DeletingAssignments(); len(ids) > 0 {
		drafts, badDrafts, err := s.jobs.ListForAssignments(ctx, ids, assignmentJobTypes())
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrStorage, err)
		}
		out, bad = append(out, drafts...), append(bad, badDrafts...)
	}
	if len(bad) > 0 {
		s.log.Warn("unreadable jobs of a recording are left as they are", slog.String("recording", rec.ID().String()),
			slog.Int("jobs", len(bad)))
	}
	return out, nil
}

// unfinished returns the jobs that are not in a terminal status.
func unfinished(jobs []*job.Job) []*job.Job {
	var out []*job.Job
	for _, j := range jobs {
		if !j.Status().IsTerminal() {
			out = append(out, j)
		}
	}
	return out
}

// kept returns the found assignments that are not deleted, in found order.
func kept(found []deletion.Assignment, deleted []shared.AssignmentID) []shared.AssignmentID {
	gone := make(map[shared.AssignmentID]bool, len(deleted))
	for _, id := range deleted {
		gone[id] = true
	}
	out := []shared.AssignmentID{}
	for _, a := range found {
		if !gone[a.ID] {
			out = append(out, a.ID)
			gone[a.ID] = true
		}
	}
	return out
}
