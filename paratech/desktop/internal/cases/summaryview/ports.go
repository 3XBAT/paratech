package summaryview

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/summary"
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

// TranscriptFinder finds the transcript the summary was made from. Implemented by the sqlite adapter.
type TranscriptFinder interface {
	// LatestByRecording loads the newest transcript of the recording; none wraps shared.ErrNotFound.
	LatestByRecording(ctx context.Context, rec shared.RecordingID) (*transcript.Transcript, error)
}

// SummaryReader reads the summary of a transcript. Implemented by the sqlite adapter.
type SummaryReader interface {
	// Get loads a summary; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)
}

// LectureTextReader reads the lecture text of a transcript (TASK-090). Implemented by the sqlite adapter.
type LectureTextReader interface {
	// Get loads a lecture text; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id lecturetext.ID) (*lecturetext.LectureText, error)
}

// AssignmentLister lists the homework found in a transcript. Implemented by the sqlite adapter.
type AssignmentLister interface {
	// ListByTranscript returns the assignments extracted from t in the order of the extraction answer; none is an
	// empty list.
	ListByTranscript(ctx context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error)
}

// JobFinder finds the summarize, extract_homework and lecture_text jobs of a transcript. Implemented by the sqlite
// adapter.
type JobFinder interface {
	// LatestByKeyPrefix returns the newest job of type typ, in any status, whose idempotency key starts with prefix;
	// none wraps shared.ErrNotFound.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)
}

// TextWriter writes an exported summary to the path the user chose in the save dialog, atomically: on a failure
// nothing remains under that name. Implemented by the textfile adapter.
type TextWriter interface {
	WriteFile(ctx context.Context, path string, content []byte) error
}

// Clock supplies the current time; its location is the one dates are shown in (ARCH-24). The app wires the local
// clock.
type Clock interface {
	Now() time.Time
}
