package todayfeed

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/shared"
)

func TestNewScreenNeedsEveryPort(t *testing.T) {
	if _, err := NewScreen(ScreenDeps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewScreen without ports: %v, want ErrInvalidConfig", err)
	}
}

// record runs the events of a transcript at time at through the Poster, as the job hook does.
func record(t *testing.T, w *world, at time.Time, tr shared.TranscriptID, events ...post.Event) {
	t.Helper()
	w.clock.Set(at)
	p := w.poster()
	for _, e := range events {
		if err := p.Record(context.Background(), e, tr); err != nil {
			t.Fatal(err)
		}
	}
	w.clock.Set(tNow)
}

// day builds the state of the mockup: a math lecture recorded this morning with a summary and one assignment, a
// physics lecture later today, yesterday's lecture with a summary and a deadline proposal waiting.
func day(t *testing.T) *world {
	w := newWorld(t)
	w.addLesson("l-math", "Математический анализ", "Матанализ", at(21, 10, 30))
	w.addLesson("l-phys", "Физика", "Физика", at(21, 14, 0))
	w.addLesson("l-tomorrow", "История", "История", at(22, 8, 50))

	w.addRecording("r-math", "l-math", "Матанализ", at(21, 10, 30), "t-math")
	w.summaries["t-math"] = true
	w.addHomework("a-math", "t-math", at(24, 23, 59))
	record(t, w, at(21, 11, 52), "t-math", post.EventSummary, post.EventHomework)

	w.addRecording("r-yday", "", "", at(20, 15, 40), "t-yday")
	w.summaries["t-yday"] = true
	record(t, w, at(20, 17, 0), "t-yday", post.EventSummary)

	w.subjects["Матанализ"] = subjectmatch.Match{ID: "subj-math", Name: "Математический анализ"}
	w.deadlines = []today.Deadline{
		{Assignment: "a-math", Title: "Задачи 1–7 из § 5", Subject: "Математический анализ", Due: at(24, 23, 59)},
		{Assignment: "a-essay", Title: "Реферат", Subject: "История России", Due: at(23, 23, 59)},
		{Assignment: "a-far", Title: "Курсовая", Due: at(30, 23, 59)},
	}
	w.labels["a-math"] = [2]string{"Задачи 1–7 из § 5", "Математический анализ"}
	w.addProposal("p-math", "a-math", at(24, 23, 59))
	w.addProposal("p-gone", "a-gone", at(25, 23, 59))
	return w
}

func TestTodayScreen(t *testing.T) {
	w := day(t)
	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Day.Equal(at(21, 0, 0)) {
		t.Errorf("Day = %v", got.Day)
	}

	// Posts: only today's; the post names the lesson and its library subject.
	if len(got.Posts) != 1 {
		t.Fatalf("posts = %+v, want the math post only", got.Posts)
	}
	p := got.Posts[0]
	wantCalendar := []CalendarAction{{ProposalKind: proposal.KindDeadlineEvent, Assignment: "a-math", Due: at(24, 23, 59)}}
	if p.Recording != "r-math" || p.Headline != "Конспект готов · Найдено ДЗ до 24.10" || !p.SummaryReady ||
		p.Homework != 1 || !reflect.DeepEqual(p.Calendar, wantCalendar) || !p.ShownAt.Equal(at(21, 11, 52)) {
		t.Errorf("post = %+v", p)
	}
	if l := p.Lesson; l.Lesson != "l-math" || l.Subject != "Матанализ" || l.SubjectID != "subj-math" ||
		l.SubjectName != "Математический анализ" || l.Kind != "lecture" || !l.Start.Equal(at(21, 10, 30)) {
		t.Errorf("post lesson = %+v", l)
	}

	// Lessons of the day, with the post of the recorded one.
	if len(got.Lessons) != 2 || got.Lessons[0].Lesson != "l-math" || got.Lessons[1].Lesson != "l-phys" {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	if got.Lessons[0].Post == nil || got.Lessons[0].Post.Recording != "r-math" || got.Lessons[1].Post != nil {
		t.Errorf("lesson posts = %+v, %+v", got.Lessons[0].Post, got.Lessons[1].Post)
	}
	if l := got.Lessons[0]; l.Title != "Математический анализ" || l.Teacher != "Петров А. В." || l.Kind != "lecture" ||
		!l.End.Equal(at(21, 11, 50)) {
		t.Errorf("lesson row = %+v", l)
	}

	// «Готово за день».
	if len(got.Done) != 1 || got.Done[0].Recording != "r-math" || got.Done[0].Homework != 1 || !got.Done[0].SummaryReady {
		t.Errorf("done = %+v", got.Done)
	}

	// «Ждёт вашего «Да»»: both validated proposals; a gone assignment only loses its labels.
	if len(got.Waiting) != 2 {
		t.Fatalf("waiting = %+v", got.Waiting)
	}
	byID := map[proposal.ID]WaitingItem{}
	for _, it := range got.Waiting {
		byID[it.Proposal] = it
	}
	if it := byID["p-math"]; it.Kind != proposal.KindDeadlineEvent || it.Title != "Задачи 1–7 из § 5" ||
		it.Subject != "Математический анализ" || !it.Due.Equal(at(24, 23, 59)) {
		t.Errorf("waiting math = %+v", it)
	}
	if it := byID["p-gone"]; it.Title != "" || it.Assignment != "a-gone" {
		t.Errorf("waiting gone = %+v", it)
	}

	// «Горит на 7 дней».
	var burning []shared.AssignmentID
	for _, b := range got.Burning {
		burning = append(burning, b.Assignment)
	}
	if !reflect.DeepEqual(burning, []shared.AssignmentID{"a-essay", "a-math"}) {
		t.Errorf("burning = %v", burning)
	}

	// Chips by the rules.
	var chips []string
	for _, c := range got.Chips {
		chips = append(chips, c.Text)
	}
	want := []string{"Что горит на неделе?", "Объясни пару: Математический анализ", "Повторить вчерашнюю пару", "Конспект по файлу"}
	if !reflect.DeepEqual(chips, want) {
		t.Errorf("chips = %v, want %v", chips, want)
	}
}

// A lesson without speech: the summary job fails, the homework job too (TASK-110). Even a homework event with
// nothing found must not show a post, a «Готово» line or a lesson status.
func TestTodayShowsNoPostForALessonWithoutSpeech(t *testing.T) {
	w := newWorld(t)
	w.addLesson("l1", "Физика", "Физика", at(21, 8, 50))
	w.addRecording("r1", "l1", "Физика", at(21, 8, 50), "t1")
	record(t, w, at(21, 10, 15), "t1", post.EventSummary, post.EventHomework) // no summary stored

	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 0 || len(got.Done) != 0 || got.Lessons[0].Post != nil {
		t.Fatalf("today = %+v, want no post", got)
	}
	if len(w.posts.rows) != 1 || !w.posts.rows["r1"].SummaryAt.IsZero() {
		t.Fatalf("stored post = %+v, want the homework check only", w.posts.rows)
	}
	if !reflect.DeepEqual(got.Chips, []today.Chip{{Kind: today.ChipFile, Text: "Конспект по файлу"}}) {
		t.Fatalf("chips = %+v", got.Chips)
	}
}

func TestTodayEmpty(t *testing.T) {
	got, err := newWorld(t).screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Posts == nil || got.Done == nil || got.Lessons == nil || got.Waiting == nil || got.Burning == nil ||
		len(got.Posts)+len(got.Done)+len(got.Lessons)+len(got.Waiting)+len(got.Burning) != 0 || len(got.Chips) != 1 {
		t.Fatalf("today = %#v, want empty lists and the file chip", got)
	}
}

func TestTodayHidesPostsOfGoneRecordings(t *testing.T) {
	w := day(t)
	delete(w.recordings, "r-math")
	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 0 || got.Lessons[0].Post != nil {
		t.Fatalf("today = %+v, want no post of a deleted recording", got)
	}
}

func TestTodayPassesStorageErrors(t *testing.T) {
	w := day(t)
	w.failReads = errDisk
	if _, err := w.screen().Today(context.Background()); !errors.Is(err, errDisk) {
		t.Fatalf("Today = %v, want the storage error", err)
	}
}

func TestThread(t *testing.T) {
	w := day(t)
	s := w.screen()
	ctx := context.Background()
	cases := []struct {
		name    string
		subject shared.SubjectID
		want    []shared.RecordingID
	}{
		{"library subject", "subj-math", []shared.RecordingID{"r-math"}},
		{"no subject", "", []shared.RecordingID{"r-yday"}},
		{"other subject", "subj-phys", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Thread(ctx, tc.subject)
			if err != nil {
				t.Fatal(err)
			}
			var ids []shared.RecordingID
			for _, v := range got {
				ids = append(ids, v.Recording)
			}
			if !reflect.DeepEqual(ids, tc.want) || got == nil {
				t.Fatalf("thread = %v, want %v", ids, tc.want)
			}
		})
	}
	if _, err := s.Thread(ctx, "bad id"); err == nil {
		t.Fatal("a bad subject id was accepted")
	}

	// Posts older than the thread history are not listed.
	w.clock.Set(tNow.Add(ThreadHistory + 48*time.Hour))
	if got, err := s.Thread(ctx, "subj-math"); err != nil || len(got) != 0 {
		t.Fatalf("old thread = %+v, %v; want none", got, err)
	}
}
