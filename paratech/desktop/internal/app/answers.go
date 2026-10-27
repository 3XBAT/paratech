package app

import (
	"context"
	"errors"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// noteGetter loads a note of the student; the sqlite note repository.
type noteGetter interface {
	Get(ctx context.Context, id shared.NoteID) (*note.Note, error)
}

// answerNotes is the Notes port of the answers over the note repository: a note purged or removed is not there, so
// its citation shows «источник удалён» (ADR-0027).
type answerNotes struct {
	notes noteGetter
}

// NoteActive reports whether the note exists and is not removed.
func (n answerNotes) NoteActive(ctx context.Context, id shared.NoteID) (bool, error) {
	got, err := n.notes.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return got.Active(), nil
}
