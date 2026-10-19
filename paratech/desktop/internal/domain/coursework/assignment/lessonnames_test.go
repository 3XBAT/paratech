package assignment

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

func TestHintsOfAnExtraction(t *testing.T) {
	a, err := NewExtracted(Spec{
		ID: "a1", Origin: Origin{Transcript: "t1"}, PromptVersion: 2, Details: details(t, "Essay", "", time.Time{}),
		Hints: values.NewLessonNames("Физика\r\n", " Петров "),
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if h := a.Hints(); h.Subject() != "Физика" || h.Teacher() != "Петров" || !a.Correction().IsZero() {
		t.Fatalf("hints %+v, correction %+v", h, a.Correction())
	}
}

func TestSetHints(t *testing.T) {
	later := t0.Add(time.Hour)
	cases := []struct {
		name        string
		edited      bool
		hints       values.LessonNames
		wantChanged bool
	}{
		{"new hints", false, values.NewLessonNames("Физика", ""), true},
		{"same hints", false, values.LessonNames{}, false},
		{"user-edited details still take new hints", true, values.NewLessonNames("", "Петров"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := extracted(t)
			if tc.edited {
				if err := a.Edit(details(t, "Mine", "", time.Time{}), t0); err != nil {
					t.Fatal(err)
				}
			}
			before := a.Details()
			changed, err := a.SetHints(tc.hints, later)
			if err != nil || changed != tc.wantChanged {
				t.Fatalf("SetHints = %v, %v; want %v", changed, err, tc.wantChanged)
			}
			if a.Hints() != tc.hints || !a.Details().Equal(before) || a.UserEdited() != tc.edited {
				t.Fatalf("hints %+v, details changed %v, edited %v", a.Hints(), !a.Details().Equal(before), a.UserEdited())
			}
			if changed != a.UpdatedAt().Equal(later) {
				t.Fatalf("updated at %v", a.UpdatedAt())
			}
		})
	}

	st := extracted(t).State()
	st.Version, st.SourceIndex, st.PromptVersion = 1, -1, 0
	manual, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manual.SetHints(values.NewLessonNames("x", ""), later); !errors.Is(err, ErrNotExtracted) {
		t.Fatalf("SetHints of a manual assignment = %v, want ErrNotExtracted", err)
	}
}

func TestCorrect(t *testing.T) {
	a := extracted(t)
	later := t0.Add(time.Hour)
	if !a.Correct(values.NewLessonNames("Геометрия", "Сидоров"), later) || a.Correction().Subject() != "Геометрия" ||
		!a.UpdatedAt().Equal(later) {
		t.Fatalf("correction %+v at %v", a.Correction(), a.UpdatedAt())
	}
	if a.UserEdited() {
		t.Fatal("a correction of the lesson is not an edit of the details")
	}
	if a.Correct(values.NewLessonNames("Геометрия", "Сидоров"), later.Add(time.Hour)) {
		t.Fatal("the same correction changed the assignment")
	}
	if !a.Correct(values.LessonNames{}, later) || !a.Correction().IsZero() {
		t.Fatalf("clearing left %+v", a.Correction())
	}
}

func TestRestoreKeepsTheLessonNames(t *testing.T) {
	a := extracted(t)
	if _, err := a.SetHints(values.NewLessonNames("Физика", "Петров"), t0); err != nil {
		t.Fatal(err)
	}
	a.Correct(values.NewLessonNames("", "Сидоров"), t0)
	st := a.State()
	st.Version = 1

	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hints() != a.Hints() || got.Correction() != a.Correction() {
		t.Fatalf("restored hints %+v, correction %+v", got.Hints(), got.Correction())
	}

	// Stored text is cleaned like new text.
	st.HintSubject, st.UserTeacher = "Физика‮\nx", "  Иванов  "
	got, err = Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hints().Subject() != "Физика x" || got.Correction().Teacher() != "Иванов" {
		t.Fatalf("restored %q, %q", got.Hints().Subject(), got.Correction().Teacher())
	}
}
