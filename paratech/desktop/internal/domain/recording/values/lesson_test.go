package values

import (
	"strings"
	"testing"
	"time"
)

func TestNewLessonSnapshot(t *testing.T) {
	start := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)

	type want struct {
		title, subject, kind, teacher string
		start, end                    time.Time
	}
	tests := []struct {
		name string
		spec LessonSnapshotSpec
		want want
	}{
		{name: "empty is zero", spec: LessonSnapshotSpec{}},
		{
			name: "all fields",
			spec: LessonSnapshotSpec{Title: "Матанализ", Subject: "Математика", Kind: "lecture", Teacher: "Иванов И. И.", Start: start, End: end},
			want: want{title: "Матанализ", subject: "Математика", kind: "lecture", teacher: "Иванов И. И.", start: start, end: end},
		},
		{
			name: "title only",
			spec: LessonSnapshotSpec{Title: "Тестовый урок"},
			want: want{title: "Тестовый урок"},
		},
		{
			name: "texts trimmed and made one line",
			spec: LessonSnapshotSpec{Title: "  Мат\nанализ\t ", Subject: "\x00Физ\u200bика\u202e", Teacher: " \r\n "},
			want: want{title: "Мат анализ", subject: "Физика"},
		},
		{
			name: "invalid UTF-8 dropped",
			spec: LessonSnapshotSpec{Title: "Урок\xff1"},
			want: want{title: "Урок1"},
		},
		{name: "kind lower-cased", spec: LessonSnapshotSpec{Kind: " Seminar "}, want: want{kind: "seminar"}},
		{name: "kind with a space dropped", spec: LessonSnapshotSpec{Kind: "lab work"}},
		{name: "kind not ASCII dropped", spec: LessonSnapshotSpec{Kind: "лекция"}},
		{name: "kind too long dropped", spec: LessonSnapshotSpec{Kind: strings.Repeat("a", MaxLessonKindBytes+1)}},
		{name: "kind code with dash", spec: LessonSnapshotSpec{Kind: "lab-2_x"}, want: want{kind: "lab-2_x"}},
		{name: "end before start dropped", spec: LessonSnapshotSpec{Start: end, End: start}, want: want{start: end}},
		{name: "end equal to start dropped", spec: LessonSnapshotSpec{Start: start, End: start}, want: want{start: start}},
		{name: "end without start kept", spec: LessonSnapshotSpec{End: end}, want: want{end: end}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewLessonSnapshot(tt.spec)
			got := want{s.Title(), s.Subject(), s.Kind(), s.Teacher(), s.ScheduledStart(), s.ScheduledEnd()}
			if got != tt.want {
				t.Fatalf("NewLessonSnapshot(%+v) = %+v; want %+v", tt.spec, got, tt.want)
			}
			if s.IsZero() != (tt.want == want{}) {
				t.Fatalf("IsZero = %v for %+v", s.IsZero(), got)
			}
			again := NewLessonSnapshot(LessonSnapshotSpec{
				Title: s.Title(), Subject: s.Subject(), Kind: s.Kind(), Teacher: s.Teacher(),
				Start: s.ScheduledStart(), End: s.ScheduledEnd(),
			})
			if again != s || !again.Equal(s) {
				t.Fatalf("not idempotent: %+v then %+v", s, again)
			}
		})
	}
}

func TestLessonSnapshotBounds(t *testing.T) {
	long := strings.Repeat("я", MaxLessonTitleBytes) // two bytes each
	s := NewLessonSnapshot(LessonSnapshotSpec{Title: long, Subject: long, Teacher: long})

	for name, c := range map[string]struct {
		got   string
		limit int
	}{
		"title":   {s.Title(), MaxLessonTitleBytes},
		"subject": {s.Subject(), MaxLessonSubjectBytes},
		"teacher": {s.Teacher(), MaxLessonTeacherBytes},
	} {
		if len(c.got) > c.limit || len(c.got) < c.limit-1 || strings.Trim(c.got, "я") != "" {
			t.Errorf("%s cut to %d bytes; want at most %d whole runes", name, len(c.got), c.limit)
		}
	}
}

func TestLessonSnapshotEqual(t *testing.T) {
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	a := NewLessonSnapshot(LessonSnapshotSpec{Title: "Урок", Start: at})
	b := NewLessonSnapshot(LessonSnapshotSpec{Title: "Урок", Start: at.In(time.FixedZone("MSK", 3*3600))})
	if !a.Equal(b) {
		t.Fatal("the same instant in another zone must be equal")
	}
	if a.Equal(NewLessonSnapshot(LessonSnapshotSpec{Title: "Урок 2", Start: at})) {
		t.Fatal("another title must differ")
	}
}
