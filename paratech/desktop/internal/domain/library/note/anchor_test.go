package note

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in   string
		want string
		err  error
	}{
		{"2026-09-12", "2026-09-12", nil},
		{"2028-02-29", "2028-02-29", nil},
		{"2026-02-29", "", ErrInvalidDate},
		{"2026-9-12", "", ErrInvalidDate},
		{"12.09.2026", "", ErrInvalidDate},
		{"1999-12-31", "", ErrInvalidDate},
		{"2101-01-01", "", ErrInvalidDate},
		{"", "", ErrInvalidDate},
	}
	for _, tt := range tests {
		d, err := ParseDate(tt.in)
		if d.String() != tt.want || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
			t.Errorf("ParseDate(%q) = %q, %v; want %q, %v", tt.in, d, err, tt.want, tt.err)
		}
	}
	if d, err := NewDate(2026, time.September, 12); err != nil || d.Year() != 2026 || d.Month() != time.September || d.Day() != 12 {
		t.Fatalf("NewDate = %v, %v", d, err)
	}
	if !(Date{}).IsZero() || (Date{}).String() != "" {
		t.Fatal("zero date")
	}
}

func TestNewAnchor(t *testing.T) {
	day := mustDate(t, "2026-09-12")
	tests := []struct {
		name string
		spec AnchorSpec
		err  error
	}{
		{"recording", AnchorSpec{Kind: AnchorRecording, Recording: "r1", Date: day}, nil},
		{"lesson", AnchorSpec{Kind: AnchorLesson, Lesson: "l1", Date: day, LessonKind: "lecture", Title: "Матанализ"}, nil},
		{"day", AnchorSpec{Kind: AnchorDay, Date: day}, nil},
		{"undated", AnchorSpec{Kind: AnchorUndated, Title: "Лекция без даты"}, nil},
		{"unknown kind", AnchorSpec{Kind: "week", Date: day}, ErrInvalidAnchor},
		{"no kind", AnchorSpec{Date: day}, ErrInvalidAnchor},
		{"recording without id", AnchorSpec{Kind: AnchorRecording, Date: day}, ErrInvalidAnchor},
		{"recording with a lesson", AnchorSpec{Kind: AnchorRecording, Recording: "r1", Lesson: "l1", Date: day}, ErrInvalidAnchor},
		{"recording with a bad id", AnchorSpec{Kind: AnchorRecording, Recording: "a/b", Date: day}, ErrInvalidAnchor},
		{"recording without a date", AnchorSpec{Kind: AnchorRecording, Recording: "r1"}, ErrInvalidAnchor},
		{"lesson without id", AnchorSpec{Kind: AnchorLesson, Date: day}, ErrInvalidAnchor},
		{"lesson with a recording", AnchorSpec{Kind: AnchorLesson, Lesson: "l1", Recording: "r1", Date: day}, ErrInvalidAnchor},
		{"lesson with a bad id", AnchorSpec{Kind: AnchorLesson, Lesson: "a b", Date: day}, ErrInvalidAnchor},
		{"lesson without a date", AnchorSpec{Kind: AnchorLesson, Lesson: "l1"}, ErrInvalidAnchor},
		{"day without a date", AnchorSpec{Kind: AnchorDay}, ErrInvalidAnchor},
		{"day with a recording", AnchorSpec{Kind: AnchorDay, Recording: "r1", Date: day}, ErrInvalidAnchor},
		{"day with a lesson", AnchorSpec{Kind: AnchorDay, Lesson: "l1", Date: day}, ErrInvalidAnchor},
		{"undated with a date", AnchorSpec{Kind: AnchorUndated, Date: day}, ErrInvalidAnchor},
		{"undated with a lesson", AnchorSpec{Kind: AnchorUndated, Lesson: "l1"}, ErrInvalidAnchor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAnchor(tt.spec)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("NewAnchor = %v, want %v", err, tt.err)
			}
			if err != nil && !a.IsZero() {
				t.Fatalf("a refused anchor is not zero: %+v", a)
			}
			if err == nil && (a.Kind() != tt.spec.Kind || a.Recording() != tt.spec.Recording || a.Lesson() != tt.spec.Lesson ||
				a.Date() != tt.spec.Date) {
				t.Fatalf("anchor = %+v, from %+v", a, tt.spec)
			}
		})
	}
}

func TestAnchorSnapshotIsCleaned(t *testing.T) {
	a, err := NewAnchor(AnchorSpec{
		Kind: AnchorDay, Date: mustDate(t, "2026-09-12"), LessonKind: " Seminar ",
		Title: "  Ряды\nи\u202eпределы\t" + strings.Repeat("я", MaxSnapshotTitleRunes),
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.LessonKind() != "seminar" {
		t.Errorf("lesson kind = %q", a.LessonKind())
	}
	if !strings.HasPrefix(a.Title(), "Ряды ипределы я") || len([]rune(a.Title())) != MaxSnapshotTitleRunes {
		t.Errorf("title = %q (%d runes)", a.Title(), len([]rune(a.Title())))
	}
	for _, bad := range []string{"лекция", "lec ture", strings.Repeat("x", MaxLessonKindBytes+1), "a;b"} {
		a, err := NewAnchor(AnchorSpec{Kind: AnchorUndated, LessonKind: bad})
		if err != nil || a.LessonKind() != "" {
			t.Errorf("lesson kind %q kept as %q, %v", bad, a.LessonKind(), err)
		}
	}
}

func TestFallBackToDay(t *testing.T) {
	day := mustDate(t, "2026-09-12")
	want, err := NewAnchor(AnchorSpec{Kind: AnchorDay, Date: day, LessonKind: "lecture", Title: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []AnchorSpec{
		{Kind: AnchorRecording, Recording: "r1", Date: day, LessonKind: "lecture", Title: "Физика"},
		{Kind: AnchorLesson, Lesson: "l1", Date: day, LessonKind: "lecture", Title: "Физика"},
		{Kind: AnchorDay, Date: day, LessonKind: "lecture", Title: "Физика"},
	} {
		a, err := NewAnchor(spec)
		if err != nil {
			t.Fatal(err)
		}
		got := a.FallBackToDay()
		if got != want || got.validate() != nil {
			t.Errorf("%s: FallBackToDay = %+v, want %+v", spec.Kind, got, want)
		}
	}
	undated, err := NewAnchor(AnchorSpec{Kind: AnchorUndated, Title: "Без даты"})
	if err != nil {
		t.Fatal(err)
	}
	if got := undated.FallBackToDay(); got != undated {
		t.Errorf("undated FallBackToDay = %+v", got)
	}
}
