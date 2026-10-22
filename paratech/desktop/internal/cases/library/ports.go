package library

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/library/material"
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
	// shared.ErrDuplicate, a subject, folder or lesson that does not exist shared.ErrConflict.
	Save(ctx context.Context, m *material.Material) error
}

// BlobStore keeps the bytes of the files in the private artifact store (hard rule 5). Implemented by the filestore
// adapter.
type BlobStore interface {
	// Size returns the size of a stored file; a missing one wraps shared.ErrNotFound.
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)

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

// IDs generates the identifiers of new subjects, folders and materials (ARCH-24).
type IDs interface {
	NewSubjectID() shared.SubjectID
	NewFolderID() shared.FolderID
	NewMaterialID() shared.MaterialID
}
