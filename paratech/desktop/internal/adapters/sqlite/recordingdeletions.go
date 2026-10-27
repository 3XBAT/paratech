package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the recording deletion, used in error messages.
const (
	opDeletionContents = "sqlite: contents of a recording to delete"
	opDeletionFiles    = "sqlite: files of a recording to delete"
	opDeletionPurge    = "sqlite: purge a deleted recording"
	opListRemovals     = "sqlite: list deadline events to remove"
	opPurgeRemoval     = "sqlite: purge a removed deadline event"
	opQueueRemoval     = "sqlite: queue a deadline event to remove"
)

// RecordingDeletions reads what a recording holds and removes its rows (the recordingdelete ports, TASK-095,
// ADR-0018). Which rows go is decided by the use case and the domain; this only executes it.
type RecordingDeletions struct{ db *DB }

// NewRecordingDeletions returns the recording deletion store of db.
func NewRecordingDeletions(db *DB) *RecordingDeletions { return &RecordingDeletions{db: db} }

// Contents returns the transcripts, live parts, summaries and lecture texts of the recording and the assignments
// found in its transcripts. Sound and Jobs are left to the caller (the aggregate and the job store know them).
func (d *RecordingDeletions) Contents(ctx context.Context, id shared.RecordingID) (deletion.Contents, error) {
	q := d.db.Queries(ctx)
	counts, err := q.RecordingDeletionCounts(ctx, string(id))
	if err != nil {
		return deletion.Contents{}, fmt.Errorf("%s %s: %w", opDeletionContents, id, MapError(ctx, err))
	}
	rows, err := q.ListRecordingAssignments(ctx, string(id))
	if err != nil {
		return deletion.Contents{}, fmt.Errorf("%s %s: %w", opDeletionContents, id, MapError(ctx, err))
	}

	out := deletion.Contents{
		Transcripts: int(counts.Transcripts), Parts: int(counts.Parts), Summaries: int(counts.Summaries),
		LectureTexts: int(counts.LectureTexts), Assignments: make([]deletion.Assignment, 0, len(rows)),
	}
	for _, r := range rows {
		out.Assignments = append(out.Assignments, deletion.Assignment{
			ID: shared.AssignmentID(r.ID), Title: r.Title, HasDraft: r.HasDraft != 0, Deliveries: int(r.Deliveries),
		})
	}
	return out, nil
}

// Files returns the documents of the transcripts of the recording.
func (d *RecordingDeletions) Files(ctx context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error) {
	refs, err := d.db.Queries(ctx).ListTranscriptRefsByRecording(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opDeletionFiles, id, MapError(ctx, err))
	}
	out := make([]shared.ArtifactRef, 0, len(refs))
	for _, s := range refs {
		ref, err := shared.ParseArtifactRef(s)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", opDeletionFiles, id, err)
		}
		out = append(out, ref)
	}
	return out, nil
}

// Purge removes, in one transaction, the live parts, summaries, lecture texts and transcripts of rec, the chosen
// assignments with their drafts (each leaves a deadline event removal for the calendar sync), the finished jobs and
// finally the recording row, guarded by the version rec was loaded with: a stale rec yields shared.ErrVersionConflict
// and nothing is removed. A job that is not finished is kept; a delivery referencing an assignment fails the purge
// with shared.ErrConflict.
func (d *RecordingDeletions) Purge(ctx context.Context, rec *recording.Recording, assignments []shared.AssignmentID,
	jobs []shared.JobID, at time.Time) error {
	id := string(rec.ID())
	var terminal []string
	for _, s := range job.Statuses() {
		if s.IsTerminal() {
			terminal = append(terminal, string(s))
		}
	}

	err := d.db.Do(ctx, func(ctx context.Context) error {
		q := d.db.Queries(ctx)
		steps := []func() error{
			func() error { return q.DeleteTranscriptParts(ctx, id) },
			func() error { return q.DeleteSummariesOfRecording(ctx, id) },
			func() error { return q.DeleteLectureTextsOfRecording(ctx, id) },
		}
		for _, a := range assignments {
			steps = append(steps,
				func() error { return q.DeleteDraftOfAssignment(ctx, string(a)) },
				func() error { return q.DeleteChosenAssignment(ctx, string(a)) },
				func() error {
					return q.InsertDeadlineEventRemoval(ctx, sqlcgen.InsertDeadlineEventRemovalParams{
						AssignmentID: string(a), CreatedAt: jobMs(at),
					})
				})
		}
		for _, j := range jobs {
			steps = append(steps, func() error {
				return q.DeleteFinishedJob(ctx, sqlcgen.DeleteFinishedJobParams{ID: string(j), Statuses: terminal})
			})
		}
		steps = append(steps, func() error { return q.DeleteTranscriptsByRecording(ctx, id) })
		for _, step := range steps {
			if err := step(); err != nil {
				return MapError(ctx, err)
			}
		}

		n, err := q.DeleteRecording(ctx, sqlcgen.DeleteRecordingParams{ID: id, ExpectedVersion: rec.Version()})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDeletionPurge, id, err)
	}
	return nil
}

// DeadlineEventRemovals are the deadline events of deleted assignments still to remove from Google (the
// schedulesync.DeadlineRemovals port, TASK-095, and homeworkflow.DeadlineRemovals of the soft delete, TASK-229).
type DeadlineEventRemovals struct{ db *DB }

// NewDeadlineEventRemovals returns the deadline event removals of db.
func NewDeadlineEventRemovals(db *DB) *DeadlineEventRemovals { return &DeadlineEventRemovals{db: db} }

// QueueDeadlineRemoval queues the removal of the deadline event of assignment id at at, in the caller's transaction
// or its own; one already queued keeps its time.
func (r *DeadlineEventRemovals) QueueDeadlineRemoval(ctx context.Context, id shared.AssignmentID, at time.Time) error {
	err := r.db.Do(ctx, func(ctx context.Context) error {
		return MapError(ctx, r.db.Queries(ctx).InsertDeadlineEventRemoval(ctx, sqlcgen.InsertDeadlineEventRemovalParams{
			AssignmentID: string(id), CreatedAt: jobMs(at),
		}))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opQueueRemoval, id, err)
	}
	return nil
}

// ListDeadlineRemovals returns up to limit assignments whose deadline event is to be removed, oldest first; a limit
// below 1 returns none.
func (r *DeadlineEventRemovals) ListDeadlineRemovals(ctx context.Context, limit int) ([]shared.AssignmentID, error) {
	if limit < 1 {
		return []shared.AssignmentID{}, nil // SQLite reads a negative LIMIT as no limit
	}
	ids, err := r.db.Queries(ctx).ListDeadlineEventRemovals(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListRemovals, MapError(ctx, err))
	}
	out := make([]shared.AssignmentID, len(ids))
	for i, id := range ids {
		out[i] = shared.AssignmentID(id)
	}
	return out, nil
}

// PurgeDeadlineRemoval forgets the removal once the event is gone; a missing row is not an error.
func (r *DeadlineEventRemovals) PurgeDeadlineRemoval(ctx context.Context, id shared.AssignmentID) error {
	err := r.db.Do(ctx, func(ctx context.Context) error {
		return MapError(ctx, r.db.Queries(ctx).DeleteDeadlineEventRemoval(ctx, string(id)))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opPurgeRemoval, id, err)
	}
	return nil
}
