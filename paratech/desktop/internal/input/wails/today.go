package wails

import (
	"context"
	"log/slog"

	"paratech/desktop/internal/cases/todayfeed"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/shared"
)

// todayError is an error of «Сегодня»: a fixed Russian text the page shows as it is.
type todayError string

func (e todayError) Error() string { return string(e) }

// Errors of «Сегодня». Causes are logged, never shown.
const (
	ErrTodayInvalidID   = todayError("Неверный идентификатор предмета: обновите страницу.")
	ErrTodayUnavailable = todayError("Не удалось прочитать данные «Сегодня». Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opTodayScreen = "ui: today screen"
	opTodayThread = "ui: posts of a subject thread"
)

// TodayUseCase is «Сегодня» (ARCH-42); satisfied by todayfeed.Screen.
type TodayUseCase interface {
	Today(ctx context.Context) (todayfeed.Today, error)
	Thread(ctx context.Context, subject shared.SubjectID) ([]todayfeed.PostView, error)
}

// TodayPostLesson is the lesson of a post. SubjectID is the library subject that keys its agent thread ("" is the
// thread «Без предмета»); SubjectName is the name to show. Kind is a lesson kind code, "" if unknown.
type TodayPostLesson struct {
	LessonID    string `json:"lessonId"`
	Title       string `json:"title"`
	Subject     string `json:"subject"`
	SubjectID   string `json:"subjectId"`
	SubjectName string `json:"subjectName"`
	Kind        string `json:"kind"`
	Start       string `json:"start"`
}

// TodayCalendarAction is the data of «Да, в календарь»: the page starts the existing proposal of Kind
// ("create_deadline_event") for the assignment; it is confirmed and executed only on the existing path.
type TodayCalendarAction struct {
	Kind         string `json:"kind"`
	AssignmentID string `json:"assignmentId"`
	Due          string `json:"due"`
}

// TodayPost is a post after a lesson: «Конспект готов · Найдено ДЗ до 24.10 · [Да, в календарь] [Открыть]».
// «Открыть» opens the summary of RecordingID when SummaryReady.
type TodayPost struct {
	RecordingID  string                `json:"recordingId"`
	TranscriptID string                `json:"transcriptId"`
	Lesson       TodayPostLesson       `json:"lesson"`
	Headline     string                `json:"headline"`
	SummaryReady bool                  `json:"summaryReady"`
	Homework     int                   `json:"homework"`
	EarliestDue  string                `json:"earliestDue"`
	Calendar     []TodayCalendarAction `json:"calendar"`
	ShownAt      string                `json:"shownAt"`
}

// TodayLesson is a lesson of the day; Post is null until its recording has a post.
type TodayLesson struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Subject string     `json:"subject"`
	Kind    string     `json:"kind"`
	Teacher string     `json:"teacher"`
	Start   string     `json:"start"`
	End     string     `json:"end"`
	Post    *TodayPost `json:"post"`
}

// TodayWaiting is a proposal of «Ждёт вашего «Да»». Kind is the proposal kind ("create_deadline_event",
// "send_homework_email", "export_homework"); Due only for a deadline, Recipient only for an email.
type TodayWaiting struct {
	ProposalID   string `json:"proposalId"`
	Kind         string `json:"kind"`
	AssignmentID string `json:"assignmentId"`
	Title        string `json:"title"`
	Subject      string `json:"subject"`
	Due          string `json:"due"`
	Recipient    string `json:"recipient"`
	CreatedAt    string `json:"createdAt"`
}

// TodayDeadline is a line of «Горит на 7 дней»; DaysLeft counts calendar days from today, Urgent marks the accent.
type TodayDeadline struct {
	AssignmentID string `json:"assignmentId"`
	Title        string `json:"title"`
	Subject      string `json:"subject"`
	Due          string `json:"due"`
	DaysLeft     int    `json:"daysLeft"`
	Urgent       bool   `json:"urgent"`
}

// TodayDone is a line of «Готово за день».
type TodayDone struct {
	RecordingID  string          `json:"recordingId"`
	Lesson       TodayPostLesson `json:"lesson"`
	SummaryReady bool            `json:"summaryReady"`
	Homework     int             `json:"homework"`
	At           string          `json:"at"`
}

// TodayChip is a hint under «Спросить paratech»; a click puts Text into the field.
type TodayChip struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// TodayScreen is the data of «Сегодня». Day is the start of the student's day.
type TodayScreen struct {
	Day     string          `json:"day"`
	Lessons []TodayLesson   `json:"lessons"`
	Posts   []TodayPost     `json:"posts"`
	Waiting []TodayWaiting  `json:"waiting"`
	Burning []TodayDeadline `json:"burning"`
	Done    []TodayDone     `json:"done"`
	Chips   []TodayChip     `json:"chips"`
}

// TodayService is bound to «Сегодня» (TASK-125; the page is TASK-126). It only reads local data.
type TodayService struct {
	uc  TodayUseCase
	log *slog.Logger
}

// NewTodayService returns the binding of «Сегодня».
func NewTodayService(uc TodayUseCase, log *slog.Logger) *TodayService {
	return &TodayService{uc: uc, log: log}
}

// Today returns the data of the screen.
func (s *TodayService) Today(ctx context.Context) (TodayScreen, error) {
	v, err := s.uc.Today(ctx)
	if err != nil {
		s.log.Error(opTodayScreen, "err", err)
		return TodayScreen{}, ErrTodayUnavailable
	}
	out := TodayScreen{
		Day: timeText(v.Day), Lessons: make([]TodayLesson, 0, len(v.Lessons)), Posts: todayPosts(v.Posts),
		Waiting: make([]TodayWaiting, 0, len(v.Waiting)), Burning: make([]TodayDeadline, 0, len(v.Burning)),
		Done: make([]TodayDone, 0, len(v.Done)), Chips: make([]TodayChip, 0, len(v.Chips)),
	}
	for _, l := range v.Lessons {
		row := TodayLesson{
			ID: l.Lesson.String(), Title: l.Title, Subject: l.Subject, Kind: l.Kind, Teacher: l.Teacher,
			Start: timeText(l.Start), End: timeText(l.End),
		}
		if l.Post != nil {
			p := todayPost(*l.Post)
			row.Post = &p
		}
		out.Lessons = append(out.Lessons, row)
	}
	for _, w := range v.Waiting {
		out.Waiting = append(out.Waiting, TodayWaiting{
			ProposalID: w.Proposal.String(), Kind: string(w.Kind), AssignmentID: w.Assignment.String(), Title: w.Title,
			Subject: w.Subject, Due: timeText(w.Due), Recipient: w.Recipient, CreatedAt: timeText(w.CreatedAt),
		})
	}
	for _, b := range v.Burning {
		out.Burning = append(out.Burning, todayDeadline(b))
	}
	for _, d := range v.Done {
		out.Done = append(out.Done, TodayDone{
			RecordingID: d.Recording.String(), Lesson: todayLesson(d.Lesson), SummaryReady: d.SummaryReady,
			Homework: d.Homework, At: timeText(d.At),
		})
	}
	for _, c := range v.Chips {
		out.Chips = append(out.Chips, TodayChip{Kind: string(c.Kind), Text: c.Text})
	}
	return out, nil
}

// SubjectPosts returns the posts of the agent thread of a library subject, newest first; "" is the thread «Без
// предмета».
func (s *TodayService) SubjectPosts(ctx context.Context, subjectID string) ([]TodayPost, error) {
	id := shared.SubjectID(subjectID)
	if id != "" && id.Validate() != nil {
		return nil, ErrTodayInvalidID
	}
	list, err := s.uc.Thread(ctx, id)
	if err != nil {
		s.log.Error(opTodayThread, "err", err)
		return nil, ErrTodayUnavailable
	}
	return todayPosts(list), nil
}

func todayPosts(list []todayfeed.PostView) []TodayPost {
	out := make([]TodayPost, 0, len(list))
	for _, p := range list {
		out = append(out, todayPost(p))
	}
	return out
}

func todayPost(p todayfeed.PostView) TodayPost {
	out := TodayPost{
		RecordingID: p.Recording.String(), TranscriptID: p.Transcript.String(), Lesson: todayLesson(p.Lesson),
		Headline: p.Headline, SummaryReady: p.SummaryReady, Homework: p.Homework, EarliestDue: timeText(p.EarliestDue),
		Calendar: make([]TodayCalendarAction, 0, len(p.Calendar)), ShownAt: timeText(p.ShownAt),
	}
	for _, c := range p.Calendar {
		out.Calendar = append(out.Calendar, TodayCalendarAction{
			Kind: string(c.ProposalKind), AssignmentID: c.Assignment.String(), Due: timeText(c.Due),
		})
	}
	return out
}

func todayLesson(l todayfeed.LessonInfo) TodayPostLesson {
	return TodayPostLesson{
		LessonID: l.Lesson.String(), Title: l.Title, Subject: l.Subject, SubjectID: l.SubjectID.String(),
		SubjectName: l.SubjectName, Kind: l.Kind, Start: timeText(l.Start),
	}
}

func todayDeadline(b today.Burning) TodayDeadline {
	return TodayDeadline{
		AssignmentID: b.Assignment.String(), Title: b.Title, Subject: b.Subject, Due: timeText(b.Due),
		DaysLeft: b.DaysLeft, Urgent: b.Urgent,
	}
}
