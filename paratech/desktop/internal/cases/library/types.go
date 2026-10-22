package library

import (
	"log/slog"

	"paratech/desktop/internal/domain/library/material"
)

// NewSubject is what CreateSubject needs. Semester is a values.Semester code ("2026-autumn"); "" is the semester of
// today.
type NewSubject struct {
	Name     string
	Aliases  []string
	Semester string
}

// Upload is a file the student chose: its bytes and its original name (shown only).
type Upload struct {
	Name string
	Data []byte
}

// Imported is the material of an upload. Existing: the same bytes were in the library already, so no new material
// was made (a removed one came back into «Входящие»).
type Imported struct {
	Material *material.Material
	Existing bool
}

// Deps are the ports the Service needs; Log is optional.
type Deps struct {
	Subjects  SubjectRepository
	Materials MaterialRepository
	Blobs     BlobStore
	Lessons   LessonReader
	Strings   SubjectStrings
	Clock     Clock
	IDs       IDs
	Log       *slog.Logger
}
