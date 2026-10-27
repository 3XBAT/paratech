package subjectview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/today"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/overview"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

func TestNewRequiresEveryPort(t *testing.T) {
	e := newEnv(t)
	full := Deps{
		Subjects: e.subjects, Materials: e.materials, Resolver: e.resolver, Assignments: e.assignments,
		Schedule: e.schedule, Knowledge: e.knowledge, Summaries: e.summaries, Failures: e.failures, Visits: e.visits,
		Clock: fakeClock{now},
	}
	drops := []func(*Deps){
		func(d *Deps) { d.Subjects = nil }, func(d *Deps) { d.Materials = nil }, func(d *Deps) { d.Resolver = nil },
		func(d *Deps) { d.Assignments = nil }, func(d *Deps) { d.Schedule = nil }, func(d *Deps) { d.Knowledge = nil },
		func(d *Deps) { d.Summaries = nil }, func(d *Deps) { d.Failures = nil }, func(d *Deps) { d.Visits = nil },
		func(d *Deps) { d.Clock = nil },
	}
	for i, drop := range drops {
		d := full
		drop(&d)
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("drop %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

func TestOverviewDeadlines(t *testing.T) {
	e := newEnv(t)
	due := func(days int) time.Time { return now.AddDate(0, 0, days) }
	e.assignments.list = []today.Deadline{
		{Assignment: "a-later", Title: "Контрольная", Subject: "Линейная алгебра", Due: due(7)},
		{Assignment: "a-alias", Title: "Задачи 3, 5, 8", Subject: "Линал", Due: due(6)}, // an alias of s1
		{Assignment: "a-other", Title: "Лабораторная", Subject: "Физика", Due: due(1)},
		{Assignment: "a-unknown", Title: "Эссе", Subject: "Философия", Due: due(1)},
		{Assignment: "a-none", Title: "Без предмета", Due: due(1)},
		{Assignment: "a-passed", Title: "Прошло", Subject: "Линал", Due: due(-1)},
		{Assignment: "a-undated", Title: "Без срока", Subject: "Линал"},
	}
	for i := range 6 {
		e.assignments.list = append(e.assignments.list, today.Deadline{
			Assignment: shared.AssignmentID(fmt.Sprintf("a-far-%d", i)), Subject: "Линал", Due: due(30 + i),
		})
	}

	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []shared.AssignmentID
	for _, d := range got.Deadlines {
		ids = append(ids, d.Assignment)
	}
	want := []shared.AssignmentID{"a-alias", "a-later", "a-far-0", "a-far-1", "a-far-2"}
	if !slices.Equal(ids, want) {
		t.Fatalf("deadlines = %v, want %v", ids, want)
	}
	if d := got.Deadlines[0]; d.Title != "Задачи 3, 5, 8" || d.DaysLeft != 6 {
		t.Fatalf("first deadline = %+v", d)
	}
	if e.resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want one for all strings", e.resolver.calls)
	}
}

func TestOverviewSchedule(t *testing.T) {
	e := newEnv(t)
	tue := time.Date(2026, 10, 20, 10, 30, 0, 0, msk) // Tuesday of this week
	mon := time.Date(2026, 10, 26, 12, 10, 0, 0, msk) // Monday of the next week
	e.addLesson(t, "l1", "Линейная алгебра", lesson.KindLecture, tue)
	e.addLesson(t, "l2", "Линейная алгебра", lesson.KindLecture, tue.AddDate(0, 0, 7))
	e.addLesson(t, "l3", "Линал", lesson.KindSeminar, mon)
	e.addLesson(t, "l4", "Физика", lesson.KindLecture, mon)
	e.addLesson(t, "l5", "Линейная алгебра", lesson.KindLab, tue.AddDate(0, 0, 30)) // outside the window

	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := []overview.Slot{
		{Kind: "seminar", Weekday: time.Monday, StartMinute: 12*60 + 10, EndMinute: 13*60 + 40},
		{Kind: "lecture", Weekday: time.Tuesday, StartMinute: 10*60 + 30, EndMinute: 12 * 60},
	}
	if !slices.Equal(got.Schedule, want) {
		t.Fatalf("schedule = %+v, want %+v", got.Schedule, want)
	}
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, msk)
	if !e.schedule.from.Equal(day.Add(-ScheduleBack)) || !e.schedule.to.Equal(day.Add(ScheduleAhead)) {
		t.Fatalf("schedule window = [%v, %v)", e.schedule.from, e.schedule.to)
	}
}

func TestOverviewNoScheduleNoDeadlines(t *testing.T) {
	e := newEnv(t)
	got, err := e.svc.Overview(context.Background(), "s2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Физика" || got.Subject != "s2" || len(got.Deadlines) != 0 || len(got.Schedule) != 0 ||
		len(got.Lessons) != 0 || len(got.Fresh) != 0 || !got.OpenedAt.IsZero() {
		t.Fatalf("overview = %+v, want an empty page", got)
	}
	if got.Deadlines == nil || got.Schedule == nil || got.Lessons == nil || got.Fresh == nil {
		t.Fatalf("overview = %+v: lists must be empty, not nil", got)
	}
	if e.resolver.calls != 0 {
		t.Fatalf("resolver called %d times without strings", e.resolver.calls)
	}
}

func TestOverviewLessons(t *testing.T) {
	e := newEnv(t)
	opened := now.Add(-24 * time.Hour)
	e.visits.at["s1"] = opened
	start := func(days int) time.Time { return now.AddDate(0, 0, -days).UTC() }
	e.knowledge.byKey[kb.LibraryKey("s1")] = []kb.Lesson{
		{Recording: "r-new", Title: "Матрицы операторов", Kind: "lecture", StartedAt: start(1), Duration: 90 * time.Minute,
			Ready: kb.Readiness{Transcript: "t-new", Summary: true}},
		{Recording: "r-old", Title: "Смена базиса", Kind: "lecture", StartedAt: start(8),
			Ready: kb.Readiness{Transcript: "t-old", Summary: true}},
		{Recording: "r-text", StartedAt: start(9), Ready: kb.Readiness{Transcript: "t-text"}},
		{Recording: "r-fail", StartedAt: start(10), Ready: kb.Readiness{Transcript: "t-fail"}},
		{Recording: "r-raw", StartedAt: start(11)},
		{Recording: "r-lost", StartedAt: start(12), Ready: kb.Readiness{Transcript: "t-lost", Summary: true}},
	}
	e.addSummary(t, "t-new", now.Add(-time.Hour))
	e.addSummary(t, "t-old", opened.Add(-time.Hour))
	e.failures.ids = []shared.RecordingID{"r-fail", "r-new", "r-elsewhere"}

	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		rec    shared.RecordingID
		status overview.Status
		fresh  bool
	}
	var rows []row
	for _, l := range got.Lessons {
		rows = append(rows, row{l.Recording, l.Status, l.SummaryNew})
	}
	want := []row{
		{"r-new", overview.StatusSummaryReady, true}, // a failed later step keeps the summary
		{"r-old", overview.StatusSummaryReady, false},
		{"r-text", overview.StatusTranscribed, false},
		{"r-fail", overview.StatusFailed, false},
		{"r-raw", overview.StatusRecorded, false},
		{"r-lost", overview.StatusSummaryReady, false}, // the summary row is gone: no mark
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("lessons = %v, want %v", rows, want)
	}
	first := got.Lessons[0]
	if first.Title != "Матрицы операторов" || first.Kind != "lecture" || first.Duration != 90*time.Minute ||
		first.StartedAt.Location() != msk || !got.OpenedAt.Equal(opened) {
		t.Fatalf("first lesson = %+v, opened %v", first, got.OpenedAt)
	}
}

func TestOverviewLessonsAtMostMax(t *testing.T) {
	e := newEnv(t)
	var list []kb.Lesson
	for i := range overview.MaxLessons + 3 {
		list = append(list, kb.Lesson{Recording: shared.RecordingID(fmt.Sprintf("r%02d", i)), StartedAt: now.Add(-time.Duration(i) * time.Hour)})
	}
	e.knowledge.byKey[kb.LibraryKey("s1")] = list
	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lessons) != overview.MaxLessons || got.Lessons[0].Recording != "r00" {
		t.Fatalf("lessons = %d, first %s; want the newest %d", len(got.Lessons), got.Lessons[0].Recording, overview.MaxLessons)
	}
}

func TestOverviewFreshMarks(t *testing.T) {
	opened := now.Add(-24 * time.Hour)
	tests := []struct {
		name   string
		opened time.Time
		want   []FreshMaterial
	}{
		{name: "never opened marks nothing", want: []FreshMaterial{}},
		{name: "after the last opening", opened: opened, want: []FreshMaterial{{Material: "m-new", Folder: "f-tests"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if !tt.opened.IsZero() {
				e.visits.at["s1"] = tt.opened
			}
			e.addMaterial(t, "m-new", "s1", "f-tests", material.StatusActive, now.Add(-time.Hour))
			e.addMaterial(t, "m-old", "s1", "f-lect", material.StatusActive, opened.Add(-time.Hour))
			e.addMaterial(t, "m-removed", "s1", "f-lect", material.StatusRemoved, now.Add(-time.Hour))
			e.addMaterial(t, "m-other", "s2", "f-x", material.StatusActive, now.Add(-time.Hour))
			e.knowledge.byKey[kb.LibraryKey("s1")] = []kb.Lesson{
				{Recording: "r1", StartedAt: now, Ready: kb.Readiness{Transcript: "t1", Summary: true}},
			}
			e.addSummary(t, "t1", now.Add(-time.Hour))

			got, err := e.svc.Overview(context.Background(), "s1")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Fresh, tt.want) {
				t.Fatalf("fresh = %v, want %v", got.Fresh, tt.want)
			}
			if got.Lessons[0].SummaryNew != !tt.opened.IsZero() {
				t.Fatalf("summary new = %v with opened %v", got.Lessons[0].SummaryNew, tt.opened)
			}
		})
	}
}

func TestOverviewDoesNotMarkOpened(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Overview(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.visits.at["s1"]; ok {
		t.Fatal("Overview recorded an opening")
	}
}

func TestOverviewUnreadableVisitsOnlyCostMarks(t *testing.T) {
	e := newEnv(t)
	e.visits.readErr = errDisk
	e.addMaterial(t, "m1", "s1", "f1", material.StatusActive, now)
	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Overview = %v, want the page without marks", err)
	}
	if len(got.Fresh) != 0 || !got.OpenedAt.IsZero() {
		t.Fatalf("overview = %+v, want no marks", got)
	}
}

func TestOverviewUnreadableSummaryOnlyCostsItsMark(t *testing.T) {
	e := newEnv(t)
	e.visits.at["s1"] = now.Add(-time.Hour)
	e.knowledge.byKey[kb.LibraryKey("s1")] = []kb.Lesson{
		{Recording: "r1", StartedAt: now, Ready: kb.Readiness{Transcript: "t1", Summary: true}},
	}
	e.summaries.err = errDisk
	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if l := got.Lessons[0]; l.Status != overview.StatusSummaryReady || l.SummaryNew {
		t.Fatalf("lesson = %+v", l)
	}

	// A cancelled read is not swallowed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.svc.Overview(ctx, "s1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Overview = %v, want context.Canceled", err)
	}
}

func TestOverviewErrors(t *testing.T) {
	tests := []struct {
		name  string
		id    shared.SubjectID
		fault func(e *env)
		want  error
	}{
		{name: "malformed id", id: "", want: ErrInvalidRequest},
		{name: "unknown subject", id: "s9", want: ErrSubjectNotFound},
		{name: "subject store", id: "s1", fault: func(e *env) { e.subjects.err = errDisk }, want: ErrUnavailable},
		{name: "assignments", id: "s1", fault: func(e *env) { e.assignments.err = errDisk }, want: ErrUnavailable},
		{name: "schedule", id: "s1", fault: func(e *env) { e.schedule.err = errDisk }, want: ErrUnavailable},
		{name: "resolver", id: "s1", fault: func(e *env) {
			e.resolver.err = errDisk
			e.assignments.list = []today.Deadline{{Assignment: "a1", Subject: "Линал", Due: now.Add(time.Hour)}}
		}, want: ErrUnavailable},
		{name: "knowledge base", id: "s1", fault: func(e *env) { e.knowledge.err = errDisk }, want: ErrUnavailable},
		{name: "failures", id: "s1", fault: func(e *env) { e.failures.err = errDisk }, want: ErrUnavailable},
		{name: "materials", id: "s1", fault: func(e *env) { e.materials.err = errDisk }, want: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.fault != nil {
				tt.fault(e)
			}
			_, err := e.svc.Overview(context.Background(), tt.id)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Overview = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestMarkOpened(t *testing.T) {
	e := newEnv(t)
	if err := e.svc.MarkOpened(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if at := e.visits.at["s1"]; !at.Equal(now) {
		t.Fatalf("opened at %v, want %v", at, now)
	}

	// The next overview marks only what appeared after this opening.
	e.addMaterial(t, "m-before", "s1", "f1", material.StatusActive, now.Add(-time.Minute))
	e.addMaterial(t, "m-after", "s1", "f1", material.StatusActive, now.Add(time.Minute))
	e.svc.clock = fakeClock{now.Add(time.Hour)}
	got, err := e.svc.Overview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []FreshMaterial{{Material: "m-after", Folder: "f1"}}; !slices.Equal(got.Fresh, want) {
		t.Fatalf("fresh = %v, want %v", got.Fresh, want)
	}

	// Opening again is idempotent in effect: the time moves on, nothing else changes.
	if err := e.svc.MarkOpened(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if at := e.visits.at["s1"]; !at.Equal(now.Add(time.Hour)) {
		t.Fatalf("opened at %v after the second opening", at)
	}
}

func TestMarkOpenedErrors(t *testing.T) {
	tests := []struct {
		name  string
		id    shared.SubjectID
		fault func(e *env)
		want  error
	}{
		{name: "malformed id", id: "", want: ErrInvalidRequest},
		{name: "unknown subject", id: "s9", want: ErrSubjectNotFound},
		{name: "store", id: "s1", fault: func(e *env) { e.visits.saveErr = errDisk }, want: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.fault != nil {
				tt.fault(e)
			}
			if err := e.svc.MarkOpened(context.Background(), tt.id); !errors.Is(err, tt.want) {
				t.Fatalf("MarkOpened = %v, want %v", err, tt.want)
			}
			if len(e.visits.at) != 0 {
				t.Fatalf("visits = %v, want none recorded", e.visits.at)
			}
		})
	}
}
