package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names of the transcript repository, used in its error messages.
const (
	opGetTranscript    = "sqlite: get transcript"
	opSaveTranscript   = "sqlite: save transcript"
	opLatestTranscript = "sqlite: latest transcript of recording"
)

// TranscriptRepository stores transcripts (the transcribe.TranscriptRepository port). Validation stays in
// transcript.Transcript: the repository only converts rows.
type TranscriptRepository struct{ db *DB }

// NewTranscriptRepository returns the transcript repository of db.
func NewTranscriptRepository(db *DB) *TranscriptRepository { return &TranscriptRepository{db: db} }

// Get loads a transcript; a missing one wraps shared.ErrNotFound.
func (r *TranscriptRepository) Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error) {
	row, err := r.db.Queries(ctx).GetTranscript(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetTranscript, id, MapError(ctx, err))
	}

	t, err := transcriptFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetTranscript, id, err)
	}
	return t, nil
}

// LatestByRecording loads the newest transcript of the recording; none wraps shared.ErrNotFound.
func (r *TranscriptRepository) LatestByRecording(ctx context.Context, rec shared.RecordingID) (*transcript.Transcript, error) {
	row, err := r.db.Queries(ctx).LatestTranscriptByRecording(ctx, string(rec))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opLatestTranscript, rec, MapError(ctx, err))
	}

	t, err := transcriptFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opLatestTranscript, rec, err)
	}
	return t, nil
}

// Save writes the transcript: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id that
// exists already, or a stale version, yields shared.ErrVersionConflict; a recording or model that does not exist
// yields an error wrapping shared.ErrConflict (foreign key).
func (r *TranscriptRepository) Save(ctx context.Context, t *transcript.Transcript) error {
	if t.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveTranscript, t.Version())
	}

	next := t.Version() + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if t.Version() == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertTranscript(ctx, sqlcgen.InsertTranscriptParams{
				ID: string(t.ID()), RecordingID: string(t.RecordingID()), ModelID: jobNullString(string(t.ModelID())),
				ModelName: string(t.ModelName()), ModelVersion: string(t.ModelVersion()),
				Language: jobNullString(string(t.Language())), TextRef: t.Text().String(), CreatedAt: jobMs(t.CreatedAt()),
			})))
		}
		n, err := q.UpdateTranscript(ctx, sqlcgen.UpdateTranscriptParams{
			Language: jobNullString(string(t.Language())), TextRef: t.Text().String(), ID: string(t.ID()),
			ExpectedVersion: t.Version(),
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveTranscript, t.ID(), err)
	}

	t.Persisted(next)
	return nil
}

func transcriptFromRow(row sqlcgen.Transcript) (*transcript.Transcript, error) {
	text, err := shared.ParseArtifactRef(row.TextRef)
	if err != nil {
		return nil, fmt.Errorf("stored transcript %s: %w", row.ID, err)
	}

	t, err := transcript.Restore(transcript.State{
		Spec: transcript.Spec{
			ID: shared.TranscriptID(row.ID), RecordingID: shared.RecordingID(row.RecordingID),
			ModelID: shared.SttModelID(row.ModelID.String), ModelName: values.ModelName(row.ModelName),
			ModelVersion: values.ModelVersion(row.ModelVersion), Language: transcript.Language(row.Language.String),
			Text: text,
		},
		Version:   row.Version,
		CreatedAt: jobFromMs(row.CreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored transcript %s: %w", row.ID, err)
	}
	return t, nil
}
