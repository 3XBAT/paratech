package recordingimport

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Originals opens the audio file the student picked outside the data directory (ADR-0027). Implemented by the
// localfile adapter. The file is only read, never changed, moved or deleted.
type Originals interface {
	// Open opens the file at the absolute OS path read-only and returns it with its size; the caller closes it. It is
	// seekable, so the tail is read before the copy (the duration of an Ogg file). A path that is not absolute, not on
	// a local drive or not a regular file is values.ErrInvalidImportFile of the transcription context, a missing file
	// shared.ErrNotFound and other failures shared.ErrStorage. Errors never carry the path.
	Open(ctx context.Context, path string) (io.ReadSeekCloser, int64, error)
}

// ArtifactStore writes and deletes the copy through the Storage Manager (PRD section 5.2, hard rule 5). Implemented by
// the filestore adapter.
type ArtifactStore interface {
	// Write stores the content atomically (temporary file, fsync, rename): a failed write leaves nothing at ref. An
	// error of the content reader is wrapped.
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error

	// Delete removes the file; one already gone is not an error.
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// DiskSpace reports the free disk space of the data directory. Implemented by the filestore adapter.
type DiskSpace interface {
	// FreeBytes returns the bytes available to this user on the disk of the data directory.
	FreeBytes(ctx context.Context) (int64, error)
}

// RecordingRepository stores the recording aggregate (PRD section 6.2). Implemented by the sqlite adapter.
type RecordingRepository interface {
	// GetBySourceSHA256 loads the imported recording of the file with the digest, marked for deletion or not; an
	// error wrapping shared.ErrNotFound when there is none.
	GetBySourceSHA256(ctx context.Context, sha256 string) (*recording.Recording, error)

	// Save inserts a new recording (version 0) or is the version compare-and-swap of ADR-0004(d). A second recording
	// with the digest of a stored one is shared.ErrDuplicate.
	Save(ctx context.Context, r *recording.Recording) error
}

// LessonReader reads the past lesson the file is a recording of (PRD section 11). Implemented by the sqlite adapter.
type LessonReader interface {
	// Get returns a live lesson; an error wrapping shared.ErrNotFound when it is missing or deleted.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// CompletionListener is told that a recording was stored COMPLETED, so that its transcription is enqueued (PRD
// sections 7.1 and 8). The app wires the transcription enqueuer, which pins the model and is idempotent by id; the
// job runner applies the power policy. An error is only logged: the startup sweep enqueues what was missed.
type CompletionListener interface {
	// RecordingCompleted enqueues the transcription of the recording id.
	RecordingCompleted(ctx context.Context, id shared.RecordingID) error
}

// Clock supplies the current time (ARCH-24).
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24).
type IDGenerator interface {
	// NewRecordingID returns a fresh recording identifier.
	NewRecordingID() shared.RecordingID
}

// Tx is the unit-of-work port (ARCH-23): fn runs in one write transaction; it performs no file I/O.
type Tx interface {
	// Do runs fn in one write transaction and commits if fn returns nil.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
