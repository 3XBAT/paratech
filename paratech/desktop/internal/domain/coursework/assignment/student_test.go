package assignment

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

func byStudent(t *testing.T, spec StudentSpec) *Assignment {
	t.Helper()
	a, err := NewByStudent(spec, t0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewByStudent(t *testing.T) {
	due := t0.Add(48 * time.Hour)
	a := byStudent(t, StudentSpec{
		ID: "a1", Lesson: "l1", Title: "  Решить номер 5  ", Note: "Стр. 10\nи 11", Due: due, SubjectName: "Матанализ",
	})

	st := a.State()
	if st.Version != 0 || st.ID != "a1" || st.Lesson != "l1" || st.Transcript != "" || st.Material != "" ||
		st.SourceIndex != -1 || st.PromptVersion != 0 || !st.UserEdited || st.Title != "Решить номер 5" ||
		st.Description != "Стр. 10\nи 11" || !st.Due.Equal(due) || st.UserSubject != "Матанализ" || st.UserTeacher != "" ||
		st.HintSubject != "" || st.HintTeacher != "" || !st.CreatedAt.Equal(t0) || !st.UpdatedAt.Equal(t0) {
		t.Fatalf("state = %+v", st)
	}
	if !a.ByStudent() || a.Superseded(0) {
		t.Fatalf("ByStudent = %v, Superseded = %v", a.ByStudent(), a.Superseded(0))
	}
	if _, ok := a.Origin(); ok {
		t.Fatal("a student assignment has an origin")
	}

	// Without a lesson, a subject, a note or a deadline.
	b := byStudent(t, StudentSpec{ID: "a2", Title: "Напомнить себе"})
	if st := b.State(); st.Lesson != "" || st.UserSubject != "" || st.Description != "" || !st.Due.IsZero() {
		t.Fatalf("bare state = %+v", st)
	}
}

func TestNewByStudentRefusals(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	cases := []struct {
		name string
		spec StudentSpec
		want error
	}{
		{"empty title", StudentSpec{ID: "a1", Title: "   "}, ErrInvalidTitle},
		{"title too long", StudentSpec{ID: "a1", Title: long(MaxTitleRunes + 1)}, ErrInvalidTitle},
		{"title of two lines", StudentSpec{ID: "a1", Title: "one\ntwo"}, ErrInvalidTitle},
		{"title with a line separator", StudentSpec{ID: "a1", Title: "one\u2028two"}, ErrInvalidTitle},
		{"title with a control", StudentSpec{ID: "a1", Title: "one\x07two"}, ErrInvalidTitle},
		{"title with a bidi override", StudentSpec{ID: "a1", Title: "fdp.\u202Eexe"}, ErrInvalidTitle},
		{"title with a bidi isolate", StudentSpec{ID: "a1", Title: "a\u2067b"}, ErrInvalidTitle},
		{"title not utf-8", StudentSpec{ID: "a1", Title: "a\xffb"}, ErrInvalidTitle},
		{"note too long", StudentSpec{ID: "a1", Title: "ok", Note: long(MaxNoteRunes + 1)}, ErrInvalidNote},
		{"note with a tab", StudentSpec{ID: "a1", Title: "ok", Note: "a\tb"}, ErrInvalidNote},
		{"note with a control", StudentSpec{ID: "a1", Title: "ok", Note: "a\x00b"}, ErrInvalidNote},
		{"note with a bidi embedding", StudentSpec{ID: "a1", Title: "ok", Note: "a\u202Ab"}, ErrInvalidNote},
		{"note with a bidi isolate", StudentSpec{ID: "a1", Title: "ok", Note: "a\u2069b"}, ErrInvalidNote},
		{"due too early", StudentSpec{ID: "a1", Title: "ok", Due: t0.AddDate(0, 0, -31).Add(-time.Minute)}, ErrInvalidDue},
		{"due too late", StudentSpec{ID: "a1", Title: "ok", Due: t0.AddDate(2, 0, 0).Add(time.Minute)}, ErrInvalidDue},
		{"bad id", StudentSpec{ID: "a 1", Title: "ok"}, ErrInvalidAssignment},
		{"bad lesson", StudentSpec{ID: "a1", Lesson: "l 1", Title: "ok"}, ErrInvalidAssignment},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewByStudent(c.spec, t0)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.want != ErrInvalidAssignment && !errors.Is(err, ErrInvalidDetails) {
				t.Fatalf("err = %v, want it to be ErrInvalidDetails too", err)
			}
		})
	}
}

func TestNewByStudentLimits(t *testing.T) {
	cases := []StudentSpec{
		{ID: "a1", Title: strings.Repeat("я", MaxTitleRunes), Note: strings.Repeat("я", MaxNoteRunes)},
		{ID: "a1", Title: "ok", Note: "строка\r\nстрока"},
		{ID: "a1", Title: "ok", Due: t0.AddDate(0, 0, -31)},
		{ID: "a1", Title: "ok", Due: t0.AddDate(2, 0, 0)},
	}
	for i, spec := range cases {
		if _, err := NewByStudent(spec, t0); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestStudentDue(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	cases := []struct {
		name, date, clock string
		want              time.Time
		err               error
	}{
		{"no date", "", "", time.Time{}, nil},
		{"end of the day", "2026-10-30", "", time.Date(2026, 10, 30, 23, 59, 0, 0, msk), nil},
		{"at nine", "2026-10-28", "09:00", time.Date(2026, 10, 28, 9, 0, 0, 0, msk), nil},
		{"midnight", "2026-10-28", "00:00", time.Date(2026, 10, 28, 0, 0, 0, 0, msk), nil},
		{"last minute", "2026-10-28", "23:59", time.Date(2026, 10, 28, 23, 59, 0, 0, msk), nil},
		{"time without a date", "", "09:00", time.Time{}, ErrInvalidTime},
		{"one-digit hour", "2026-10-28", "9:00", time.Time{}, ErrInvalidTime},
		{"hour 24", "2026-10-28", "24:00", time.Time{}, ErrInvalidTime},
		{"minute 60", "2026-10-28", "09:60", time.Time{}, ErrInvalidTime},
		{"seconds", "2026-10-28", "09:00:00", time.Time{}, ErrInvalidTime},
		{"letters", "2026-10-28", "ab:cd", time.Time{}, ErrInvalidTime},
		{"no such day", "2026-02-30", "", time.Time{}, ErrInvalidDue},
		{"short month", "2026-1-05", "", time.Time{}, ErrInvalidDue},
		{"another format", "30.10.2026", "", time.Time{}, ErrInvalidDue},
		{"a timestamp", "2026-10-30T10:00:00Z", "", time.Time{}, ErrInvalidDue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := StudentDue(c.date, c.clock, msk)
			if !errors.Is(err, c.err) || (c.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if !got.Equal(c.want) {
				t.Fatalf("due = %v, want %v", got, c.want)
			}
		})
	}
	if _, err := StudentDue("2026-10-28", "", nil); !errors.Is(err, ErrInvalidDue) {
		t.Fatalf("no zone: err = %v", err)
	}
}

// Extraction never touches an assignment the student added; Restore reads its row back.
func TestStudentAssignmentIsNotExtracted(t *testing.T) {
	a := byStudent(t, StudentSpec{ID: "a1", Title: "Свой текст", SubjectName: "Физика"})
	if _, err := a.Reextract(details(t, "Model text", "", time.Time{}), 2, t0); !errors.Is(err, ErrNotExtracted) {
		t.Fatalf("Reextract = %v", err)
	}
	if _, err := a.SetHints(values.NewLessonNames("Химия", "Иванов"), t0); !errors.Is(err, ErrNotExtracted) {
		t.Fatalf("SetHints = %v", err)
	}
	if a.Superseded(0) || a.Details().Title() != "Свой текст" || a.Hints() != (values.LessonNames{}) {
		t.Fatalf("changed: %+v", a.State())
	}

	st := a.State()
	st.Version = 1
	back, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !back.ByStudent() || back.State() != st || !back.SameByStudent(a) {
		t.Fatalf("restored = %+v", back.State())
	}
	if extracted(t).ByStudent() {
		t.Fatal("an extracted assignment is ByStudent")
	}
}

func TestSameByStudent(t *testing.T) {
	base := StudentSpec{ID: "a1", Lesson: "l1", Title: "Задание", Note: "n", Due: t0.Add(time.Hour), SubjectName: "Физика"}
	a := byStudent(t, base)
	cases := []struct {
		name string
		edit func(*StudentSpec)
		want bool
	}{
		{"same", func(*StudentSpec) {}, true},
		{"same due in another zone", func(s *StudentSpec) { s.Due = s.Due.In(time.FixedZone("X", 7200)) }, true},
		{"other title", func(s *StudentSpec) { s.Title = "Другое" }, false},
		{"other note", func(s *StudentSpec) { s.Note = "m" }, false},
		{"other due", func(s *StudentSpec) { s.Due = s.Due.Add(time.Minute) }, false},
		{"no due", func(s *StudentSpec) { s.Due = time.Time{} }, false},
		{"other lesson", func(s *StudentSpec) { s.Lesson = "" }, false},
		{"other subject", func(s *StudentSpec) { s.SubjectName = "Химия" }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := base
			c.edit(&spec)
			if got := a.SameByStudent(byStudent(t, spec)); got != c.want {
				t.Fatalf("SameByStudent = %v, want %v", got, c.want)
			}
		})
	}
	if a.SameByStudent(extracted(t)) || extracted(t).SameByStudent(extracted(t)) {
		t.Fatal("an extracted assignment matched")
	}
}
