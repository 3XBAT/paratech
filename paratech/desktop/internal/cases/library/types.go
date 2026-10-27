package library

import (
	"log/slog"

	"paratech/desktop/internal/domain/library/inspection"
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

// Inspected is what Inspect tells about a material: how far its text was read and, when there was text, the reader's
// answer. The answer is a suggestion only (ADR-0022): nothing is filed, linked or sent because of it.
type Inspected struct {
	Material *material.Material
	// Text is TextExtracted, TextFailed (the file could not be read), TextPDFNotExtracted or TextNoText (a photo).
	Text material.TextStatus
	// Empty means the text was read and holds nothing, so there was nothing to inspect.
	Empty bool
	// Answer is the reader's answer; nil without text. A subject it named that is no longer in the library is dropped.
	Answer *inspection.Answer
}

// Deps are the ports the Service needs; Log is optional. Texts, Reader and Inspections are needed only by the reading
// of the text (MaterialText, Inspect): without them those answer ErrNotConfigured. Notes is optional: without it a
// subject with notes is still refused by the store, as ErrNotEmpty instead of ErrHasNotes. Images is optional: without
// it a photo is refused (ErrUnsupportedImage), never stored as it came.
type Deps struct {
	Notes       SubjectNotes
	Images      PageImages
	Subjects    SubjectRepository
	Materials   MaterialRepository
	Blobs       BlobStore
	Lessons     LessonReader
	Strings     SubjectStrings
	Clock       Clock
	IDs         IDs
	Texts       TextExtractor
	Reader      MaterialReader
	Inspections InspectionRepository
	Log         *slog.Logger
}
