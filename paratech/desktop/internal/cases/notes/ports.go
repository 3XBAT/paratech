package notes

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// NoteRepository stores the notes. Implemented by the sqlite adapter. The lists hold notes of any status.
type NoteRepository interface {
	// Get loads a note; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.NoteID) (*note.Note, error)

	// Save writes it under its version: a stale one, or an id that exists, wraps shared.ErrVersionConflict; a subject
	// or material that does not exist shared.ErrConflict.
	Save(ctx context.Context, n *note.Note) error

	ListBySubject(ctx context.Context, sub shared.SubjectID) ([]*note.Note, error)
	ListByRecording(ctx context.Context, rec shared.RecordingID) ([]*note.Note, error)
	ListByLesson(ctx context.Context, l shared.LessonID) ([]*note.Note, error)
}

// Materials stores and reads the files of the library. Implemented by library.Service; its errors (an empty, too
// large or unsupported file, a missing subject) pass through as they are.
type Materials interface {
	// FileNotePart stores the bytes once by sha256 (existing: the material was there already) and files a material
	// that is in «Входящие» under sub by lessonKind; one filed elsewhere stays where it is.
	FileNotePart(ctx context.Context, sub shared.SubjectID, lessonKind, name string, data []byte) (*material.Material, bool, error)

	// Material loads a material in any status; a missing one wraps shared.ErrNotFound.
	Material(ctx context.Context, id shared.MaterialID) (*material.Material, error)
}

// Recordings reads a recording for its anchor. Implemented by the sqlite adapter.
type Recordings interface {
	// Get loads a recording; a missing (purged) one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// Lessons reads a schedule lesson for its anchor. Implemented by the sqlite adapter.
type Lessons interface {
	// Get loads a live lesson; a missing or removed one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// Clock supplies the current time; its location is the one the date of an anchor is taken in (ARCH-24). The app
// wires the local clock.
type Clock interface {
	Now() time.Time
}

// IDs generates the identifiers of new notes (ARCH-24).
type IDs interface {
	NewNoteID() shared.NoteID
}
