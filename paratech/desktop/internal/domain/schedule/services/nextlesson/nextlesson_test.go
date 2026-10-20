package nextlesson

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

func mk(t *testing.T, id shared.LessonID, title, subject string, start time.Time, deleted bool) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Subject: subject, Start: start, End: start.Add(90 * time.Minute), TimeZone: "UTC",
	}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		if err := l.MarkDeleted(t0); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestSubjectKey(t *testing.T) {
	cases := []struct{ subject, title, want string }{
		{"Тестовая  Алгебра", "", "тестовая алгебра"},
		{"Тёмная материя", "", "темная материя"},
		{"", "Тестовая алгебра, лекция", "тестовая алгебра"},
		{"Алгебра", "Физика", "алгебра"},
		{"", "", ""},
		{"  ", "", ""},
	}
	for _, tc := range cases {
		if got := SubjectKey(tc.subject, tc.title); got != tc.want {
			t.Errorf("SubjectKey(%q, %q) = %q, want %q", tc.subject, tc.title, got, tc.want)
		}
	}
}

func TestNext(t *testing.T) {
	day := 24 * time.Hour
	cur := Current{ID: "self", Subject: "Тестовая алгебра", Start: t0}
	cases := []struct {
		name    string
		lessons func(t *testing.T) []*lesson.Lesson
		cur     Current
		want    shared.LessonID
	}{
		{"earliest of the same subject", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				mk(t, "late", "Алгебра", "Тестовая алгебра", t0.Add(14*day), false),
				mk(t, "next", "Алгебра (сем.)", "тестовая АЛГЕБРА", t0.Add(7*day), false),
				mk(t, "other", "Физика", "Физика", t0.Add(day), false),
			}
		}, cur, "next"},
		{"subject derived from the title", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "next", "Тестовая алгебра, семинар", "", t0.Add(2*day), false)}
		}, cur, "next"},
		{"itself, earlier and same start are skipped", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				mk(t, "self", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), false),
				mk(t, "before", "Тестовая алгебра", "Тестовая алгебра", t0.Add(-day), false),
				mk(t, "parallel", "Тестовая алгебра", "Тестовая алгебра", t0, false),
			}
		}, cur, ""},
		{"deleted lesson is skipped", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				mk(t, "gone", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), true),
				mk(t, "next", "Тестовая алгебра", "Тестовая алгебра", t0.Add(3*day), false),
			}
		}, cur, "next"},
		{"beyond the window", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "far", "Тестовая алгебра", "Тестовая алгебра", t0.Add(Window), false)}
		}, cur, ""},
		{"just inside the window", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "near", "Тестовая алгебра", "Тестовая алгебра", t0.Add(Window-time.Minute), false)}
		}, cur, "near"},
		{"equal starts pick the smaller id", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				mk(t, "b", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), false),
				mk(t, "a", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), false),
			}
		}, cur, "a"},
		{"unknown subject finds nothing", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "x", "Лекция", "", t0.Add(day), false), nil}
		}, Current{Start: t0}, ""},
		{"no start finds nothing", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "x", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), false)}
		}, Current{Subject: "Тестовая алгебра"}, ""},
		{"ad-hoc recording without a lesson id", func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{mk(t, "x", "Тестовая алгебра", "Тестовая алгебра", t0.Add(day), false)}
		}, Current{Title: "Тестовая алгебра", Start: t0}, "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Next(tc.lessons(t), tc.cur)
			var id shared.LessonID
			if got != nil {
				id = got.ID()
			}
			if id != tc.want {
				t.Fatalf("Next = %q, want %q", id, tc.want)
			}
		})
	}
}
