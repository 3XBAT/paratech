package ongoing

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)

func newLesson(t *testing.T, id shared.LessonID, start time.Time, d time.Duration, deleted bool) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: "Math", Start: start, End: start.Add(d), TimeZone: "Europe/Moscow",
	}}, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		if err := l.MarkDeleted(t0.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestCurrent(t *testing.T) {
	now := t0.Add(30 * time.Minute)
	tests := []struct {
		name    string
		lessons func(t *testing.T) []*lesson.Lesson
		want    shared.LessonID // "" = none
	}{
		{name: "no lessons", lessons: func(*testing.T) []*lesson.Lesson { return nil }},
		{name: "one in progress", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{newLesson(t, "a", t0, 90*time.Minute, false)}
		}, want: "a"},
		{name: "ended and future are not", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				newLesson(t, "ended", t0.Add(-time.Hour), time.Hour, false),        // ends at t0
				newLesson(t, "future", now.Add(time.Nanosecond), time.Hour, false), // starts after now
				newLesson(t, "ends-now", t0, 30*time.Minute, false),                // [start, end): ends at now
			}
		}},
		{name: "starts right now", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{newLesson(t, "now", now, time.Hour, false)}
		}, want: "now"},
		{name: "deleted is not", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{newLesson(t, "gone", t0, 90*time.Minute, true), nil}
		}},
		{name: "overlap picks the later start", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				newLesson(t, "long", t0.Add(-time.Hour), 3*time.Hour, false),
				newLesson(t, "later", t0, 90*time.Minute, false),
				newLesson(t, "earlier", t0.Add(-30*time.Minute), 2*time.Hour, false),
			}
		}, want: "later"},
		{name: "equal starts pick the smaller id", lessons: func(t *testing.T) []*lesson.Lesson {
			return []*lesson.Lesson{
				newLesson(t, "b", t0, 90*time.Minute, false),
				newLesson(t, "a", t0, time.Hour, false),
				newLesson(t, "c", t0, 2*time.Hour, false),
			}
		}, want: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Current(tt.lessons(t), now)
			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("Current = %s, want none", got.ID())
			case tt.want != "" && (got == nil || got.ID() != tt.want):
				t.Fatalf("Current = %v, want %s", got, tt.want)
			}
		})
	}
}
