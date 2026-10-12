package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// RecordingRepository stores recordings (the recordingsession.RecordingRepository port). The status machine stays in
// recording.Recording: the repository restores the aggregate and persists what its methods produced.
type RecordingRepository struct{ db *DB }

// NewRecordingRepository returns the recording repository of db.
func NewRecordingRepository(db *DB) *RecordingRepository { return &RecordingRepository{db: db} }

// Get loads a recording by id.
func (r *RecordingRepository) Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	row, err := r.db.Queries(ctx).GetRecording(ctx, string(id))
	if err != nil {
		return nil, MapError(ctx, err)
	}
	return recordingFromRow(row)
}

// Save writes the recording: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)).
func (r *RecordingRepository) Save(ctx context.Context, rec *recording.Recording) error {
	next := rec.Version() + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if rec.Version() == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertRecording(ctx, recordingInsertParams(rec))))
		}
		n, err := q.UpdateRecording(ctx, recordingUpdateParams(rec))
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return err
	}
	rec.Persisted(next)
	return nil
}

// ListByStatus returns the recordings in any of the statuses, oldest first.
func (r *RecordingRepository) ListByStatus(ctx context.Context, statuses ...recording.Status) ([]*recording.Recording, error) {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}

	rows, err := r.db.Queries(ctx).ListRecordingsByStatus(ctx, names)
	if err != nil {
		return nil, MapError(ctx, err)
	}

	out := make([]*recording.Recording, 0, len(rows))
	for _, row := range rows {
		rec, err := recordingFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Delete removes the row of rec if it still has the version rec was loaded with (ADR-0004(d)).
func (r *RecordingRepository) Delete(ctx context.Context, rec *recording.Recording) error {
	return r.db.Do(ctx, func(ctx context.Context) error {
		n, err := r.db.Queries(ctx).DeleteRecording(ctx, sqlcgen.DeleteRecordingParams{
			ID: string(rec.ID()), ExpectedVersion: rec.Version(),
		})
		return VersionCAS(ctx, n, err)
	})
}

// RecordingTranscripts finds and removes the transcripts of a recording for its deletion (the
// recordingsession.RecordingTranscripts port, PRD section 13.1).
type RecordingTranscripts struct{ db *DB }

// NewRecordingTranscripts returns the transcript lookup of db.
func NewRecordingTranscripts(db *DB) *RecordingTranscripts { return &RecordingTranscripts{db: db} }

// ListArtifactsByRecording returns the files of the transcripts of the recording.
func (t *RecordingTranscripts) ListArtifactsByRecording(ctx context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error) {
	refs, err := t.db.Queries(ctx).ListTranscriptRefsByRecording(ctx, string(id))
	if err != nil {
		return nil, MapError(ctx, err)
	}

	out := make([]shared.ArtifactRef, 0, len(refs))
	for _, s := range refs {
		ref, err := shared.ParseArtifactRef(s)
		if err != nil {
			return nil, fmt.Errorf("sqlite: stored transcript file of recording %s: %w", id, err)
		}
		out = append(out, ref)
	}
	return out, nil
}

// DeleteByRecording removes the transcript rows of the recording; without transcripts it does nothing.
func (t *RecordingTranscripts) DeleteByRecording(ctx context.Context, id shared.RecordingID) error {
	return t.db.Do(ctx, func(ctx context.Context) error {
		return MapError(ctx, t.db.Queries(ctx).DeleteTranscriptsByRecording(ctx, string(id)))
	})
}

func recordingFromRow(row sqlcgen.Recording) (*recording.Recording, error) {
	chunks, err := shared.ParseArtifactRef(row.ChunksRef)
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored recording %s: %w", row.ID, err)
	}

	var audio shared.ArtifactRef
	if row.AudioRef.Valid {
		if audio, err = shared.ParseArtifactRef(row.AudioRef.String); err != nil {
			return nil, fmt.Errorf("sqlite: stored recording %s: %w", row.ID, err)
		}
	}

	rec, err := recording.Restore(recording.State{
		Version:     row.Version,
		ID:          shared.RecordingID(row.ID),
		LessonID:    shared.LessonID(row.LessonID.String),
		Status:      recording.Status(row.Status),
		MaxDuration: msDuration(row.MaxDurationMs),
		Duration:    msDuration(row.DurationMs.Int64),
		Chunks:      int(row.ChunkCount),
		ChunksDir:   chunks,
		Audio:       audio,
		Problem:     row.FailureReason.String,
		StartedAt:   jobFromMs(row.StartedAt),
		StoppedAt:   jobFromNullMs(row.StoppedAt),
		HeartbeatAt: jobFromNullMs(row.HeartbeatAt),
		CreatedAt:   jobFromMs(row.CreatedAt),
		UpdatedAt:   jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored recording %s: %w", row.ID, err)
	}
	return rec, nil
}

func recordingInsertParams(rec *recording.Recording) sqlcgen.InsertRecordingParams {
	u := recordingUpdateParams(rec)
	return sqlcgen.InsertRecordingParams{
		ID:            u.ID,
		LessonID:      jobNullString(string(rec.LessonID())),
		Status:        u.Status,
		StartedAt:     u.StartedAt,
		StoppedAt:     u.StoppedAt,
		HeartbeatAt:   u.HeartbeatAt,
		MaxDurationMs: rec.MaxDuration().Milliseconds(),
		DurationMs:    u.DurationMs,
		ChunkCount:    u.ChunkCount,
		ChunksRef:     rec.ChunksDir().String(),
		AudioRef:      u.AudioRef,
		FailureReason: u.FailureReason,
		CreatedAt:     jobMs(rec.CreatedAt()),
		UpdatedAt:     u.UpdatedAt,
	}
}

// recordingUpdateParams holds the mutable columns.
func recordingUpdateParams(rec *recording.Recording) sqlcgen.UpdateRecordingParams {
	var duration sql.NullInt64
	if d, ok := rec.Duration(); ok {
		duration = sql.NullInt64{Int64: d.Milliseconds(), Valid: true}
	}
	return sqlcgen.UpdateRecordingParams{
		ID:              string(rec.ID()),
		ExpectedVersion: rec.Version(),
		Status:          string(rec.Status()),
		StartedAt:       jobMs(rec.StartedAt()),
		StoppedAt:       jobNullMs(rec.StoppedAt()),
		HeartbeatAt:     jobNullMs(rec.HeartbeatAt()),
		DurationMs:      duration,
		ChunkCount:      int64(rec.Chunks()),
		AudioRef:        jobNullString(rec.Audio().String()),
		FailureReason:   jobNullString(rec.Problem()),
		UpdatedAt:       jobMs(rec.UpdatedAt()),
	}
}

func msDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
