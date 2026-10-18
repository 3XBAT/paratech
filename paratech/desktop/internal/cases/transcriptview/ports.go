package transcriptview

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// RecordingReader gives read access to recordings. Implemented by the sqlite adapter.
type RecordingReader interface {
	// Get loads a recording; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// LessonReader names the lesson of a recording. Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson; a missing or removed one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// TranscriptFinder finds the stored transcript of a recording. Implemented by the sqlite adapter.
type TranscriptFinder interface {
	// LatestByRecording loads the newest transcript of the recording; none wraps shared.ErrNotFound.
	LatestByRecording(ctx context.Context, rec shared.RecordingID) (*transcript.Transcript, error)
}

// JobFinder finds the STT job of a recording, for the state of the recognition. Implemented by the sqlite adapter.
type JobFinder interface {
	// LatestByKeyPrefix returns the newest job of type typ, in any status, whose idempotency key starts with prefix;
	// none wraps shared.ErrNotFound.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)
}

// LiveParts reads what the recognition stored so far (the live transcript). Implemented by the sqlite adapter.
type LiveParts interface {
	// ListParts returns at most limit parts of the recording in number order, skipping the first offset.
	ListParts(ctx context.Context, rec shared.RecordingID, offset, limit int) ([]transcript.Part, error)

	// CountParts returns how many parts the recording has.
	CountParts(ctx context.Context, rec shared.RecordingID) (int, error)
}

// ArtifactStore reads the transcript document and the recording's track through the Storage Manager (hard rule 5).
// Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact; the caller closes it. A missing one yields shared.ErrNotFound.
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// OpenSeekable returns a seekable reader of the artifact and its size; the caller closes it. A missing one
	// yields shared.ErrNotFound.
	OpenSeekable(ctx context.Context, ref shared.ArtifactRef) (io.ReadSeekCloser, int64, error)
}

// TextWriter writes an exported transcript to the path the user chose in the save dialog, atomically: on a failure
// nothing remains under that name. Implemented by the textfile adapter.
type TextWriter interface {
	WriteFile(ctx context.Context, path string, content []byte) error
}

// Clock supplies the current time; its location is the one titles are shown in (ARCH-24). The app wires the local
// clock.
type Clock interface {
	Now() time.Time
}
