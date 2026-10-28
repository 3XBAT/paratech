package todayfeed

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// Limits of the reads.
const (
	MaxPosts      = 200                 // posts read for the screen or a thread
	MaxWaiting    = 20                  // proposals in «Ждёт вашего «Да»»
	ThreadHistory = 90 * 24 * time.Hour // posts of a subject thread (D6: threads are kept 90 days)
	saveAttempts  = 3                   // a post saved concurrently is reloaded and recorded again
)

// PosterDeps are the ports of the Poster.
type PosterDeps struct {
	Posts       PostRepository
	Transcripts TranscriptReader
	Summaries   SummaryReader
	Clock       Clock
	Log         *slog.Logger // optional
}

// ScreenDeps are the ports of the Screen.
type ScreenDeps struct {
	Posts       PostRepository
	Recordings  RecordingReader
	Lessons     LessonReader
	Homework    AssignmentReader
	Assignments Assignments
	Proposals   ProposalList
	Subjects    SubjectResolver
	Clock       Clock
	Log         *slog.Logger // optional
}

// LessonInfo is the lesson of a post as the knowledge base resolves it: the user's correction, the snapshot the
// recording kept, the live lesson. Subject is the string the lesson names; SubjectID is its library subject, which
// keys the agent thread ("" is the thread «Без предмета»), and SubjectName the name shown.
type LessonInfo struct {
	Lesson      shared.LessonID // "" for an ad-hoc recording
	Title       string
	Subject     string
	SubjectID   shared.SubjectID
	SubjectName string
	Kind        string    // lesson kind code; "" if unknown
	Start       time.Time // the scheduled start, else the recording start
}

// CalendarAction is the data of «Да, в календарь»: the existing create_deadline_event proposal of an assignment. The UI
// starts that proposal; it is validated and confirmed on the existing path, never here.
type CalendarAction struct {
	ProposalKind proposal.Kind
	Assignment   shared.AssignmentID
	Due          time.Time
}

// PostView is a post after a lesson: «Конспект готов · Найдено ДЗ до 24.10 · [Да, в календарь] [Открыть]».
type PostView struct {
	Recording    shared.RecordingID // «Открыть» opens the summary of this recording
	Transcript   shared.TranscriptID
	Lesson       LessonInfo
	Headline     string
	SummaryReady bool
	Homework     int              // assignments found, done ones too
	HomeworkDone int              // of them marked «Выполнено» (ADR-0033 §9): the mark of the subject history
	EarliestDue  time.Time        // zero: no open one has a deadline
	Calendar     []CalendarAction // open ones only
	ShownAt      time.Time        // when the post became worth showing
}

// LessonRow is a lesson of the day with its post, if the post-processing of its recording produced one.
type LessonRow struct {
	Lesson  shared.LessonID
	Title   string
	Subject string
	Kind    string // lesson kind code; "unknown" when the schedule does not say
	Teacher string
	Start   time.Time
	End     time.Time
	Post    *PostView // nil: none
}

// WaitingItem is a proposal waiting for the student's «Да»: a deadline for the calendar or an email.
type WaitingItem struct {
	Proposal   proposal.ID
	Kind       proposal.Kind
	Assignment shared.AssignmentID
	Title      string    // the assignment as the homework screen shows it
	Subject    string    // its subject
	Due        time.Time // the deadline of a calendar proposal; zero otherwise
	Recipient  string    // the configured recipient of an email; "" otherwise
	CreatedAt  time.Time
}

// DoneItem is a line of «Готово за день».
type DoneItem struct {
	Recording    shared.RecordingID
	Lesson       LessonInfo
	SummaryReady bool
	Homework     int
	At           time.Time
}

// Today is the data of «Сегодня».
type Today struct {
	Day     time.Time // start of the student's day
	Lessons []LessonRow
	Posts   []PostView // posts shown today, newest first
	Waiting []WaitingItem
	Burning []today.Burning
	Done    []DoneItem
	Chips   []today.Chip
}
