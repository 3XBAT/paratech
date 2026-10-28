package wails

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/todayfeed"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// fakeToday is a TodayUseCase with fixed answers.
type fakeToday struct {
	today   todayfeed.Today
	posts   []todayfeed.PostView
	err     error
	subject shared.SubjectID
}

func (f *fakeToday) Today(context.Context) (todayfeed.Today, error) { return f.today, f.err }

func (f *fakeToday) Thread(_ context.Context, subject shared.SubjectID) ([]todayfeed.PostView, error) {
	f.subject = subject
	return f.posts, f.err
}

var todayT0 = time.Date(2026, 10, 21, 8, 52, 0, 0, time.UTC)

func samplePost() todayfeed.PostView {
	return todayfeed.PostView{
		Recording: "r1", Transcript: "t1",
		Lesson: todayfeed.LessonInfo{
			Lesson: "l1", Title: "Математический анализ", Subject: "Матанализ", SubjectID: "s1",
			SubjectName: "Математический анализ", Kind: "lecture", Start: todayT0.Add(-2 * time.Hour),
		},
		Headline: "Конспект готов · Найдено ДЗ до 24.10", SummaryReady: true, Homework: 1, EarliestDue: todayT0.Add(72 * time.Hour),
		Calendar: []todayfeed.CalendarAction{{ProposalKind: proposal.KindDeadlineEvent, Assignment: "a1", Due: todayT0.Add(72 * time.Hour)}},
		ShownAt:  todayT0,
	}
}

func TestTodayScreenMapping(t *testing.T) {
	p := samplePost()
	uc := &fakeToday{today: todayfeed.Today{
		Day:     todayT0.Truncate(24 * time.Hour),
		Lessons: []todayfeed.LessonRow{{Lesson: "l1", Title: "Математический анализ", Kind: "lecture", Start: todayT0, End: todayT0.Add(time.Hour), Post: &p}, {Lesson: "l2", Title: "Физика"}},
		Posts:   []todayfeed.PostView{p},
		Waiting: []todayfeed.WaitingItem{{Proposal: "p1", Kind: proposal.KindDeadlineEvent, Assignment: "a1", Title: "Задачи", Due: todayT0}},
		Burning: []today.Burning{{Deadline: today.Deadline{Assignment: "a1", Title: "Задачи", Due: todayT0}, DaysLeft: 2, Urgent: true}},
		Done:    []todayfeed.DoneItem{{Recording: "r1", Lesson: p.Lesson, SummaryReady: true, Homework: 1, At: todayT0}},
		Chips:   []today.Chip{{Kind: today.ChipFile, Text: "Конспект по файлу"}},
	}}
	got, err := NewTodayService(uc, discard()).Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Lessons) != 2 || got.Lessons[0].Post == nil || got.Lessons[1].Post != nil {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	post := got.Posts[0]
	if post.RecordingID != "r1" || post.Headline != p.Headline || post.Lesson.SubjectID != "s1" ||
		post.ShownAt != "2026-10-21T08:52:00Z" || len(post.Calendar) != 1 || post.Calendar[0].Kind != "create_deadline_event" ||
		post.Calendar[0].AssignmentID != "a1" {
		t.Errorf("post = %+v", post)
	}
	if w := got.Waiting[0]; w.ProposalID != "p1" || w.Kind != "create_deadline_event" || w.Due != "2026-10-21T08:52:00Z" || w.Recipient != "" {
		t.Errorf("waiting = %+v", w)
	}
	if b := got.Burning[0]; b.AssignmentID != "a1" || b.DaysLeft != 2 || !b.Urgent {
		t.Errorf("burning = %+v", b)
	}
	if d := got.Done[0]; d.RecordingID != "r1" || d.Lesson.Title != "Математический анализ" || d.Homework != 1 {
		t.Errorf("done = %+v", d)
	}
	if c := got.Chips[0]; c.Kind != "file_summary" || c.Text != "Конспект по файлу" {
		t.Errorf("chip = %+v", c)
	}
}

// TASK-315: a post counts its assignments marked «Выполнено», in «Сегодня» and in a subject thread.
func TestTodayPostHomeworkDone(t *testing.T) {
	p := samplePost()
	p.Homework, p.HomeworkDone = 3, 2
	uc := &fakeToday{today: todayfeed.Today{Posts: []todayfeed.PostView{p}}, posts: []todayfeed.PostView{p}}
	s := NewTodayService(uc, discard())
	got, err := s.Today(context.Background())
	if err != nil || got.Posts[0].Homework != 3 || got.Posts[0].HomeworkDone != 2 {
		t.Fatalf("Today posts = %+v, %v", got.Posts, err)
	}
	thread, err := s.SubjectPosts(context.Background(), "s1")
	if err != nil || thread[0].HomeworkDone != 2 {
		t.Fatalf("SubjectPosts = %+v, %v", thread, err)
	}
	raw, err := json.Marshal(thread[0])
	if err != nil || !strings.Contains(string(raw), `"homework":3,"homeworkDone":2,`) {
		t.Fatalf("post JSON = %s, %v", raw, err)
	}
}

// An empty day is empty lists for the page, never null.
func TestTodayScreenEmptyListsAreArrays(t *testing.T) {
	got, err := NewTodayService(&fakeToday{}, discard()).Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "null") {
		t.Fatalf("JSON has null: %s", raw)
	}
}

func TestTodayErrorsAreFixedTexts(t *testing.T) {
	uc := &fakeToday{err: errors.New("sqlite: disk I/O error at C:/Users/x/db")}
	s := NewTodayService(uc, discard())
	if _, err := s.Today(context.Background()); !errors.Is(err, ErrTodayUnavailable) {
		t.Fatalf("Today = %v, want ErrTodayUnavailable", err)
	}
	if _, err := s.SubjectPosts(context.Background(), "s1"); !errors.Is(err, ErrTodayUnavailable) {
		t.Fatalf("SubjectPosts = %v, want ErrTodayUnavailable", err)
	}
}

func TestSubjectPosts(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr error
		posts   int
	}{
		{"library subject", "s1", nil, 1},
		{"no subject", "", nil, 1},
		{"bad id", "../s1", ErrTodayInvalidID, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeToday{posts: []todayfeed.PostView{samplePost()}, subject: "untouched"}
			got, err := NewTodayService(uc, discard()).SubjectPosts(context.Background(), tc.id)
			if !errors.Is(err, tc.wantErr) || len(got) != tc.posts {
				t.Fatalf("SubjectPosts = %d posts, %v; want %d, %v", len(got), err, tc.posts, tc.wantErr)
			}
			if tc.wantErr == nil && uc.subject != shared.SubjectID(tc.id) {
				t.Fatalf("thread asked = %q, want %q", uc.subject, tc.id)
			}
			if tc.wantErr != nil && uc.subject != "untouched" {
				t.Fatal("a bad id reached the use case")
			}
		})
	}
}
