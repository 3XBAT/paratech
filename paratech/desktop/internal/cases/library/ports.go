package library

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// SubjectRepository stores the subjects. Implemented by the sqlite adapter.
type SubjectRepository interface {
	// List returns every subject.
	List(ctx context.Context) ([]*subject.Subject, error)

	// Get loads a subject; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.SubjectID) (*subject.Subject, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict, a taken name or alias key
	// shared.ErrDuplicate, a removed folder that holds a file shared.ErrConflict.
	Save(ctx context.Context, s *subject.Subject) error

	// Delete removes it under its version: a stale one wraps shared.ErrVersionConflict, one that holds a file
	// shared.ErrConflict.
	Delete(ctx context.Context, s *subject.Subject) error
}

// MaterialRepository stores the files of the library. Implemented by the sqlite adapter.
type MaterialRepository interface {
	// Get loads a material; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.MaterialID) (*material.Material, error)

	// GetByDigest loads the material with the sha256; none wraps shared.ErrNotFound.
	GetByDigest(ctx context.Context, d material.Digest) (*material.Material, error)

	// ListIn returns the materials of a subject, or of «Входящие» for "", in any status.
	ListIn(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error)

	// Count returns how many materials (any status) are in folder of sub, or under sub at all for folder "".
	Count(ctx context.Context, sub shared.SubjectID, folder shared.FolderID) (int, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict, a stored sha256
	// shared.ErrDuplicate, a subject, folder or lesson that does not exist shared.ErrConflict. The text status is
	// written on the first save only; later it changes through SaveText.
	Save(ctx context.Context, m *material.Material) error

	// SaveText writes only the text status of a stored material, under its version and without a new one (see
	// material.SetText): a stale version wraps shared.ErrVersionConflict.
	SaveText(ctx context.Context, m *material.Material) error
}

// BlobStore keeps the bytes of the files in the private artifact store (hard rule 5). Implemented by the filestore
// adapter.
type BlobStore interface {
	// Size returns the size of a stored file; a missing one wraps shared.ErrNotFound.
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)

	// Open opens a stored file for reading; a missing one wraps shared.ErrNotFound.
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// Write stores the content atomically.
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}

// LessonReader checks the lesson a file is linked to. Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson; a missing or removed one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// SubjectStrings reads the free subject strings of the schedule and the recordings with their counts. Implemented by
// the sqlite adapter.
type SubjectStrings interface {
	ListSubjectStrings(ctx context.Context) ([]subjectmatch.Candidate, error)
}

// Clock supplies the current time; its location is the one a new subject's semester is derived in (ARCH-24). The app
// wires the local clock.
type Clock interface {
	Now() time.Time
}

// TextExtractor reads the text of a file of the library (TASK-122). Implemented by the doctext adapter. A type whose
// text is not read (PDF) wraps material.ErrTextNotExtracted; a damaged file or one beyond the reader's limits
// material.ErrTextUnreadable.
type TextExtractor interface {
	Extract(ctx context.Context, typ material.Type, data []byte) (material.Text, error)
}

// MaterialReader is the quarantined reader INSPECT_MATERIAL on the Hub (ADR-0022). Implemented by the hubclient
// adapter. The output is untrusted model text: only inspection.Request.Parse reads it. Errors follow the Hub client:
// shared.ErrQuotaExceeded; inspection.ErrInvalidAnswer for any permanent provider error: the Hub's schema refusal of
// the answer (charged) but also a model refusal the Hub did not charge, both counted as one used attempt; anything
// else (no network, no Hub, a refused key) uses no attempt.
type MaterialReader interface {
	InspectMaterial(ctx context.Context, req inspection.Request) (string, error)
}

// InspectionRepository stores the reader's outcome per material. Implemented by the sqlite adapter.
type InspectionRepository interface {
	// Get loads the inspection of a material; none wraps shared.ErrNotFound.
	Get(ctx context.Context, m shared.MaterialID) (*inspection.Inspection, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict.
	Save(ctx context.Context, i *inspection.Inspection) error
}

// SubjectNotes lists the student's own notes of a subject in any status (ADR-0027), so a subject that has them is
// not removed. Implemented by the sqlite adapter.
type SubjectNotes interface {
	ListBySubject(ctx context.Context, sub shared.SubjectID) ([]*note.Note, error)
}

// IDs generates the identifiers of new subjects, folders and materials (ARCH-24).
type IDs interface {
	NewSubjectID() shared.SubjectID
	NewFolderID() shared.FolderID
	NewMaterialID() shared.MaterialID
}
