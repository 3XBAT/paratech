package subjectview

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/library/services/overview"
	"paratech/desktop/internal/domain/shared"
)

// The window of the schedule the schedule line is read from: last week and the next two, so alternating weeks show.
const (
	ScheduleBack  = 7 * 24 * time.Hour
	ScheduleAhead = 14 * 24 * time.Hour
)

// Deps are the ports of the Service; Log is optional.
type Deps struct {
	Subjects    SubjectReader
	Materials   MaterialList
	Resolver    SubjectResolver
	Assignments Assignments
	Schedule    LessonReader
	Knowledge   KnowledgeBase
	Summaries   SummaryReader
	Failures    Failures
	Visits      Visits
	Clock       Clock
	Log         *slog.Logger
}

// Overview is the data of the page of a subject. Name is the subject's name (typed text, shown as data). OpenedAt is
// the previous opening the «новое» marks are relative to; zero when the page was never opened, and then nothing is
// marked.
type Overview struct {
	Subject   shared.SubjectID
	Name      string
	OpenedAt  time.Time
	Deadlines []overview.Deadline // «Ближайшее», soonest first
	Schedule  []overview.Slot     // the schedule line; empty without an imported schedule
	Lessons   []Lesson            // the latest recorded lessons, newest first
	Fresh     []FreshMaterial     // the files marked «новое», newest first
}

// Lesson is a recorded lesson of the subject. Title is "" for an ad-hoc recording (the page names it by its date);
// Kind is a lesson kind code, "" if unknown. SummaryNew marks a summary that appeared after the previous opening.
type Lesson struct {
	Recording  shared.RecordingID
	Title      string
	Kind       string
	StartedAt  time.Time
	Duration   time.Duration // 0: unknown
	Status     overview.Status
	SummaryNew bool
}

// FreshMaterial is a file of the subject added or changed after the previous opening, with its folder (the page marks
// the folder too).
type FreshMaterial struct {
	Material shared.MaterialID
	Folder   shared.FolderID
}
