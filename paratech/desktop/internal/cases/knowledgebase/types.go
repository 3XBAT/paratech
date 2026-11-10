package knowledgebase

import (
	"log/slog"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// FolderLesson is a recorded lesson of a library subject shown as a link row in one of its system folders
// (ADR-0034 amendment 2026-11-10). Nothing of it is stored.
type FolderLesson struct {
	Folder shared.FolderID // subject.SystemFolderID of the subject's system folder
	System subject.System  // SystemLectures | SystemSeminars | SystemLabs
	Lesson kb.Lesson       // a recorded lesson, as Lessons returns it (no notes)
}

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
// string only (as before ADR-0024). Notes, NoteTexts and Subjects come together or not at all: without them the base
// has no notes of the student (as before ADR-0027).
type Deps struct {
	Library      SubjectResolver
	Notes        NoteStore
	NoteTexts    NoteTextReader
	Subjects     SubjectLister
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
