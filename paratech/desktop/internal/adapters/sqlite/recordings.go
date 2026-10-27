package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
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

// opRecordingOfTranscript names RecordingOf in its error messages.
const opRecordingOfTranscript = "sqlite: get the recording of a transcript"

// RecordingOf loads the recording transcript t was made from (homeworkflow.TranscriptRecordings, TASK-092); a missing
// transcript or recording wraps shared.ErrNotFound.
func (r *RecordingRepository) RecordingOf(ctx context.Context, t shared.TranscriptID) (*recording.Recording, error) {
	row, err := r.db.Queries(ctx).GetRecordingOfTranscript(ctx, string(t))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRecordingOfTranscript, t, MapError(ctx, err))
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

// opBySourceSHA256 names GetBySourceSHA256 in its error messages.
const opBySourceSHA256 = "sqlite: get the imported recording of a file"

// GetBySourceSHA256 loads the imported recording of the file with the digest (recordingimport.RecordingRepository,
// TASK-161); none wraps shared.ErrNotFound.
func (r *RecordingRepository) GetBySourceSHA256(ctx context.Context, sha256 string) (*recording.Recording, error) {
	row, err := r.db.Queries(ctx).GetRecordingBySourceSHA256(ctx, jobNullString(sha256))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opBySourceSHA256, MapError(ctx, err))
	}
	return recordingFromRow(row)
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

// ListActivity lists the recordings of the recordings screen: every recording in a non-terminal status plus the
// newest maxTerminal others, newest first by creation time, then id; a maxTerminal below 1 adds no terminal
// recording. A row that cannot be read or restored does not fail the listing: it is left out and its id goes to
// unreadable. A recording that disappeared between the two reads is skipped. Other database errors fail the call.
func (r *RecordingRepository) ListActivity(ctx context.Context, maxTerminal int) ([]*recording.Recording, []shared.RecordingID, error) {
	names := []string{}
	for _, s := range recording.UnfinishedStatuses() {
		names = append(names, string(s))
	}
	active, err := json.Marshal(names)
	if err != nil {
		return nil, nil, err
	}
	q := r.db.Queries(ctx)
	ids, err := q.ListRecordingActivityIDs(ctx, sqlcgen.ListRecordingActivityIDsParams{
		Active: string(active), MaxTerminal: int64(max(maxTerminal, 0)), // SQLite reads a negative LIMIT as no limit
	})
	if err != nil {
		return nil, nil, MapError(ctx, err)
	}

	return loadRecordings(ctx, q, ids)
}

// loadRecordings loads the recordings of ids in their order. A row that cannot be read or restored is left out and its
// id goes to unreadable; a recording that disappeared meanwhile is skipped. Other database errors fail the call.
func loadRecordings(ctx context.Context, q *sqlcgen.Queries, ids []string) ([]*recording.Recording, []shared.RecordingID, error) {
	recs, unreadable := make([]*recording.Recording, 0, len(ids)), []shared.RecordingID{}
	for _, id := range ids {
		row, err := q.GetRecording(ctx, id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			continue
		case isScanError(err):
			unreadable = append(unreadable, shared.RecordingID(id))
			continue
		case err != nil:
			return nil, nil, MapError(ctx, err)
		}
		rec, err := recordingFromRow(row)
		if err != nil {
			unreadable = append(unreadable, shared.RecordingID(id))
			continue
		}
		recs = append(recs, rec)
	}
	return recs, unreadable, nil
}

// ListUntranscribed returns, in id order, up to limit ids greater than after of COMPLETED recordings with a final
// track and no transcript; a limit below 1 returns none.
func (r *RecordingRepository) ListUntranscribed(ctx context.Context, after shared.RecordingID, limit int) ([]shared.RecordingID, error) {
	if limit < 1 {
		return []shared.RecordingID{}, nil // SQLite reads a negative LIMIT as no limit
	}
	ids, err := r.db.Queries(ctx).ListUntranscribedRecordingIDs(ctx, sqlcgen.ListUntranscribedRecordingIDsParams{
		After: string(after), Status: string(recording.StatusCompleted), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make([]shared.RecordingID, len(ids))
	for i, id := range ids {
		out[i] = shared.RecordingID(id)
	}
	return out, nil
}

// ListDeleting returns the ids of the recordings marked for deletion, oldest mark first (TASK-095).
func (r *RecordingRepository) ListDeleting(ctx context.Context) ([]shared.RecordingID, error) {
	ids, err := r.db.Queries(ctx).ListDeletingRecordingIDs(ctx)
	if err != nil {
		return nil, MapError(ctx, err)
	}
	out := make([]shared.RecordingID, len(ids))
	for i, id := range ids {
		out[i] = shared.RecordingID(id)
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

	var deleting []shared.AssignmentID
	if err := json.Unmarshal([]byte(row.DeletingAssignments), &deleting); err != nil {
		return nil, fmt.Errorf("sqlite: stored recording %s: assignments to delete: %w", row.ID, err)
	}
	correction, err := values.NewLessonCorrection(row.UserSubject.String, row.UserKind.String)
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored recording %s: %w", row.ID, err)
	}

	rec, err := recording.Restore(recording.State{
		Version:     row.Version,
		ID:          shared.RecordingID(row.ID),
		LessonID:    shared.LessonID(row.LessonID.String),
		Lesson:      lessonSnapshotFromRow(row),
		Correction:  correction,
		Status:      recording.Status(row.Status),
		MaxDuration: msDuration(row.MaxDurationMs),
		Duration:    msDuration(row.DurationMs.Int64),
		Chunks:      int(row.ChunkCount),
		ChunksDir:   chunks,
		Audio:       audio,
		Silent:      row.Silent != 0,
		Problem:     row.FailureReason.String,
		StartedAt:   jobFromMs(row.StartedAt),
		StoppedAt:   jobFromNullMs(row.StoppedAt),
		HeartbeatAt: jobFromNullMs(row.HeartbeatAt),
		CreatedAt:   jobFromMs(row.CreatedAt),
		UpdatedAt:   jobFromMs(row.UpdatedAt),

		DeletingAt:          jobFromNullMs(row.DeletingAt),
		DeletingAssignments: deleting,

		Origin:       recording.Origin(row.Origin),
		SourceSHA256: row.SourceSha256.String,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: stored recording %s: %w", row.ID, err)
	}
	return rec, nil
}

// lessonSnapshotFromRow rebuilds the snapshot through its constructor, so stored text is cleaned like new text.
func lessonSnapshotFromRow(row sqlcgen.Recording) values.LessonSnapshot {
	return values.NewLessonSnapshot(values.LessonSnapshotSpec{
		Title: row.LessonTitle.String, Subject: row.LessonSubject.String, Kind: row.LessonKind.String,
		Teacher: row.LessonTeacher.String, Start: jobFromNullMs(row.LessonStart), End: jobFromNullMs(row.LessonEnd),
	})
}

func recordingInsertParams(rec *recording.Recording) sqlcgen.InsertRecordingParams {
	u, snap := recordingUpdateParams(rec), rec.LessonSnapshot()
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
		Silent:        u.Silent,
		LessonTitle:   jobNullString(snap.Title()),
		LessonSubject: jobNullString(snap.Subject()),
		LessonKind:    jobNullString(snap.Kind()),
		LessonTeacher: jobNullString(snap.Teacher()),
		LessonStart:   jobNullMs(snap.ScheduledStart()),
		LessonEnd:     jobNullMs(snap.ScheduledEnd()),
		CreatedAt:     jobMs(rec.CreatedAt()),
		UpdatedAt:     u.UpdatedAt,

		DeletingAt:          u.DeletingAt,
		DeletingAssignments: u.DeletingAssignments,

		UserSubject: u.UserSubject,
		UserKind:    u.UserKind,

		Origin:       string(rec.Origin()),
		SourceSha256: jobNullString(rec.SourceSHA256()),
	}
}

// recordingUpdateParams holds the mutable columns.
func recordingUpdateParams(rec *recording.Recording) sqlcgen.UpdateRecordingParams {
	var duration sql.NullInt64
	if d, ok := rec.Duration(); ok {
		duration = sql.NullInt64{Int64: d.Milliseconds(), Valid: true}
	}
	ids := rec.DeletingAssignments()
	if ids == nil {
		ids = []shared.AssignmentID{}
	}
	deleting, _ := json.Marshal(ids) // a list of strings always marshals
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
		Silent:          jobBoolInt(rec.Silent()),
		UpdatedAt:       jobMs(rec.UpdatedAt()),

		DeletingAt:          jobNullMs(rec.DeletingAt()),
		DeletingAssignments: string(deleting),

		UserSubject: jobNullString(rec.LessonCorrection().Subject()),
		UserKind:    jobNullString(rec.LessonCorrection().Kind()),
	}
}

func msDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
