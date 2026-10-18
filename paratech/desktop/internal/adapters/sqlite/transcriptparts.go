package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// Operation names of the live transcript parts, used in error messages (never with part text).
const (
	opSaveParts   = "sqlite: save transcript parts"
	opListParts   = "sqlite: list transcript parts"
	opCountParts  = "sqlite: count transcript parts"
	opDeleteParts = "sqlite: delete transcript parts"
)

// TranscriptParts stores the live transcript of a recording (TASK-079): the transcribe.LiveParts port and the reading
// side of the transcript viewer. Validation stays in transcript.Part.
type TranscriptParts struct{ db *DB }

// NewTranscriptParts returns the live transcript parts of db.
func NewTranscriptParts(db *DB) *TranscriptParts { return &TranscriptParts{db: db} }

// SaveParts upserts the parts by (recording, number) in one transaction. A recording that does not exist yields an
// error wrapping shared.ErrConflict (foreign key).
func (r *TranscriptParts) SaveParts(ctx context.Context, rec shared.RecordingID, parts []transcript.Part) error {
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		for _, p := range parts {
			s := p.Segment()
			if err := q.UpsertTranscriptPart(ctx, sqlcgen.UpsertTranscriptPartParams{
				RecordingID: string(rec), Seq: int64(p.Seq()), StartMs: s.StartMs, EndMs: s.EndMs, Text: s.Text,
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveParts, rec, err)
	}
	return nil
}

// ListParts returns at most limit parts of the recording in number order, skipping the first offset.
func (r *TranscriptParts) ListParts(ctx context.Context, rec shared.RecordingID, offset, limit int) ([]transcript.Part, error) {
	if offset < 0 || limit < 0 {
		return nil, fmt.Errorf("%s %s: negative offset or limit", opListParts, rec)
	}
	rows, err := r.db.Queries(ctx).ListTranscriptParts(ctx, sqlcgen.ListTranscriptPartsParams{
		RecordingID: string(rec), SkipRows: int64(offset), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListParts, rec, MapError(ctx, err))
	}

	out := make([]transcript.Part, 0, len(rows))
	for _, row := range rows {
		p, err := transcript.NewPart(int(row.Seq), transcript.Segment{StartMs: row.StartMs, EndMs: row.EndMs, Text: row.Text})
		if err != nil {
			return nil, fmt.Errorf("%s %s: stored part %d: %w", opListParts, rec, row.Seq, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// CountParts returns how many parts the recording has.
func (r *TranscriptParts) CountParts(ctx context.Context, rec shared.RecordingID) (int, error) {
	n, err := r.db.Queries(ctx).CountTranscriptParts(ctx, string(rec))
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opCountParts, rec, MapError(ctx, err))
	}
	return int(n), nil
}

// DeleteParts removes every part of the recording; none is not an error.
func (r *TranscriptParts) DeleteParts(ctx context.Context, rec shared.RecordingID) error {
	if err := r.db.Queries(ctx).DeleteTranscriptParts(ctx, string(rec)); err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteParts, rec, MapError(ctx, err))
	}
	return nil
}
