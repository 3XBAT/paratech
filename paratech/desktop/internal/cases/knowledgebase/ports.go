package knowledgebase

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// LessonStore lists the lessons of the knowledge base. Implemented by the sqlite adapter.
type LessonStore interface {
	// ListLessons returns the completed recordings that are neither silent nor marked for deletion; a row that cannot
	// be read is left out and its id goes to unreadable.
	ListLessons(ctx context.Context) (recs []*recording.Recording, unreadable []shared.RecordingID, err error)

	// ListReadiness returns per recording of ListLessons what is stored for its newest transcript.
	ListReadiness(ctx context.Context) (map[shared.RecordingID]kb.Readiness, error)
}

// RecordingRepository loads and saves a recording for the user's correction of its lesson. Implemented by the sqlite
// adapter.
type RecordingRepository interface {
	// Get loads a recording; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)

	// Save writes it under its version; a stale one wraps shared.ErrVersionConflict.
	Save(ctx context.Context, r *recording.Recording) error
}

// LessonReader names the live lesson of a recording that kept no snapshot. Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson; a missing or removed one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// TranscriptReader reads a transcript row, for the file of its text. Implemented by the sqlite adapter.
type TranscriptReader interface {
	// Get loads a transcript; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error)
}

// ArtifactReader reads the transcript document through the Storage Manager (hard rule 5). Implemented by the
// filestore adapter.
type ArtifactReader interface {
	// Open returns a reader of the artifact; the caller closes it. A missing one yields shared.ErrNotFound.
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
}

// SummaryReader reads the summary of a transcript. Implemented by the sqlite adapter.
type SummaryReader interface {
	// Get loads a summary; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)
}

// LectureTextReader reads the lecture text of a transcript. Implemented by the sqlite adapter.
type LectureTextReader interface {
	// Get loads a lecture text; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id lecturetext.ID) (*lecturetext.LectureText, error)
}

// TextWriter writes an exported subject to the path the user chose in the save dialog, atomically: on a failure
// nothing remains under that name. Implemented by the textfile adapter.
type TextWriter interface {
	WriteFile(ctx context.Context, path string, content []byte) error
}

// Clock supplies the current time; its location is the one exported dates are shown in (ARCH-24). The app wires the
// local clock.
type Clock interface {
	Now() time.Time
}
