package knowledgebase

import (
	"log/slog"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
)

// SearchHit is one place a query was found and the lesson it is in.
type SearchHit struct {
	kb.Hit
	Lesson kb.Lesson
}

// SearchResult is what a search found, newest lessons first. Truncated: the caps of kb.Search left hits out.
type SearchResult struct {
	Hits      []SearchHit
	Truncated bool
}

// Deps are the ports the Service needs. Log is optional; without Library lessons are grouped by their free subject
// string only (as before ADR-0024).
type Deps struct {
	Library      SubjectResolver
	Lessons      LessonStore
	Recordings   RecordingRepository
	Schedule     LessonReader
	Transcripts  TranscriptReader
	Artifacts    ArtifactReader
	Summaries    SummaryReader
	LectureTexts LectureTextReader
	Writer       TextWriter
	Clock        Clock
	Log          *slog.Logger
}
