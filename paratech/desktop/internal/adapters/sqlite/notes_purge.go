package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the purge of the notes of a removed subject (TASK-308).
const (
	opListSubjectNoteIDs = "sqlite: list notes of a subject"
	opPurgeNote          = "sqlite: purge note of a removed subject"
)

// ListIDsBySubject returns the ids of the notes of sub in any status, also while sub is removed (the readers hide
// them then), oldest first.
func (r *NoteRepository) ListIDsBySubject(ctx context.Context, sub shared.SubjectID) ([]shared.NoteID, error) {
	ids, err := r.db.Queries(ctx).ListNoteIDsOfSubject(ctx, string(sub))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSubjectNoteIDs, MapError(ctx, err))
	}
	out := make([]shared.NoteID, 0, len(ids))
	for _, id := range ids {
		out = append(out, shared.NoteID(id))
	}
	return out, nil
}

// PurgeOfRemovedSubject deletes note id of sub with its parts (CASCADE; the materials stay) if sub is still removed at
// or before cutoff; false when not.
func (r *NoteRepository) PurgeOfRemovedSubject(ctx context.Context, id shared.NoteID, sub shared.SubjectID,
	cutoff time.Time) (bool, error) {
	n, err := r.db.Queries(ctx).PurgeNoteOfRemovedSubject(ctx, sqlcgen.PurgeNoteOfRemovedSubjectParams{
		ID: string(id), SubjectID: string(sub), Cutoff: jobNullMs(cutoff),
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opPurgeNote, id, MapError(ctx, err))
	}
	return n > 0, nil
}
