package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

var _ answering.Notes = answerNotes{}

type mapNotes map[shared.NoteID]*note.Note

func (m mapNotes) Get(_ context.Context, id shared.NoteID) (*note.Note, error) {
	if id == "broken" {
		return nil, errors.New("disk")
	}
	n, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("note %s: %w", id, shared.ErrNotFound)
	}
	return n, nil
}

func TestAnswerNotes(t *testing.T) {
	at := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	newNote := func(id shared.NoteID) *note.Note {
		a, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorUndated})
		if err != nil {
			t.Fatal(err)
		}
		n, err := note.New(note.Spec{ID: id, Subject: "s1", Anchor: a, Title: "Конспект", Parts: []shared.MaterialID{"m1"}}, at)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	removed := newNote("removed")
	if _, err := removed.Remove(at); err != nil {
		t.Fatal(err)
	}
	notes := answerNotes{notes: mapNotes{"active": newNote("active"), "removed": removed}}

	for _, tt := range []struct {
		id      shared.NoteID
		want    bool
		wantErr bool
	}{
		{"active", true, false},
		{"removed", false, false},
		{"purged", false, false},
		{"broken", false, true},
	} {
		got, err := notes.NoteActive(t.Context(), tt.id)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("NoteActive(%s) = %v, %v", tt.id, got, err)
		}
	}
}
