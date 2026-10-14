package summary

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

func content(t *testing.T, text string, points ...string) Content {
	t.Helper()
	c, err := NewContent(text, points)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newSummary(t *testing.T) *Summary {
	t.Helper()
	s, err := New(Spec{Transcript: "t1", Lesson: "l1", PromptVersion: 1, Content: content(t, "About rivers.", "Rivers flow")}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewContent(t *testing.T) {
	many := make([]string, MaxKeyPoints+1)
	for i := range many {
		many[i] = "p"
	}
	cases := []struct {
		name    string
		text    string
		points  []string
		wantErr bool
	}{
		{"valid", "Line one.\nLine two.\tTabbed.", []string{"a", "b"}, false},
		{"no key points", "Text", nil, false},
		{"text at the limit", strings.Repeat("ж", MaxTextRunes), nil, false},
		{"max key points", "Text", many[:MaxKeyPoints], false},
		{"empty text", "", nil, true},
		{"blank text", " \n ", nil, true},
		{"text too long", strings.Repeat("a", MaxTextRunes+1), nil, true},
		{"text with a control", "a\x1bb", nil, true},
		{"text invalid utf8", "\xff", nil, true},
		{"too many key points", "Text", many, true},
		{"empty key point", "Text", []string{" "}, true},
		{"key point with a newline", "Text", []string{"a\nb"}, true},
		{"key point too long", "Text", []string{strings.Repeat("a", MaxKeyPointRunes+1)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewContent(tc.text, tc.points)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidContent) || !c.IsZero() {
					t.Fatalf("NewContent = %+v, %v; want ErrInvalidContent", c, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Text() != strings.TrimSpace(tc.text) || len(c.KeyPoints()) != len(tc.points) {
				t.Fatalf("content = %q %q", c.Text(), c.KeyPoints())
			}
		})
	}
}

func TestContentKeyPointsAreCopied(t *testing.T) {
	points := []string{"a"}
	c := content(t, "Text", points...)
	points[0] = "changed"
	got := c.KeyPoints()
	got[0] = "changed too"
	if c.KeyPoints()[0] != "a" {
		t.Fatal("content shares its key points with the caller")
	}
}

func TestNew(t *testing.T) {
	c := content(t, "Text")
	cases := []struct {
		name    string
		spec    Spec
		now     time.Time
		wantErr bool
	}{
		{"valid", Spec{Transcript: "t1", PromptVersion: 1, Content: c}, t0, false},
		{"with lesson", Spec{Transcript: "t1", Lesson: "l1", PromptVersion: 1, Content: c}, t0, false},
		{"no transcript", Spec{PromptVersion: 1, Content: c}, t0, true},
		{"bad transcript", Spec{Transcript: "t/1", PromptVersion: 1, Content: c}, t0, true},
		{"bad lesson", Spec{Transcript: "t1", Lesson: "l 1", PromptVersion: 1, Content: c}, t0, true},
		{"no prompt version", Spec{Transcript: "t1", Content: c}, t0, true},
		{"no content", Spec{Transcript: "t1", PromptVersion: 1}, t0, true},
		{"no time", Spec{Transcript: "t1", PromptVersion: 1, Content: c}, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(tc.spec, tc.now)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidSummary) || s != nil {
					t.Fatalf("New = %v, %v; want ErrInvalidSummary", s, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.ID() != IDOf(tc.spec.Transcript) || s.TranscriptID() != tc.spec.Transcript || s.LessonID() != tc.spec.Lesson ||
				s.Version() != 0 || !s.Content().Equal(c) || s.PromptVersion() != 1 {
				t.Fatalf("new summary = %+v", s.State())
			}
		})
	}
}

func TestReplace(t *testing.T) {
	s := newSummary(t)
	later := t0.Add(time.Hour)

	if changed, err := s.Replace(s.Content(), 1, later); err != nil || changed {
		t.Fatalf("Replace with the same content = %v, %v; want unchanged", changed, err)
	}
	if !s.UpdatedAt().Equal(t0) {
		t.Fatal("an unchanged Replace moved the update time")
	}

	newer := content(t, "About lakes.")
	if changed, err := s.Replace(newer, 1, later); err != nil || !changed {
		t.Fatalf("Replace = %v, %v; want changed", changed, err)
	}
	if !s.Content().Equal(newer) || !s.UpdatedAt().Equal(later) {
		t.Fatalf("after Replace: %+v", s.State())
	}
	if changed, err := s.Replace(newer, 2, later); err != nil || !changed || s.PromptVersion() != 2 {
		t.Fatalf("Replace with a new prompt version = %v, %v", changed, err)
	}

	before := s.State()
	for name, call := range map[string]func() (bool, error){
		"zero content":      func() (bool, error) { return s.Replace(Content{}, 1, later) },
		"no prompt version": func() (bool, error) { return s.Replace(newer, 0, later) },
	} {
		if changed, err := call(); !errors.Is(err, ErrInvalidSummary) || changed {
			t.Errorf("%s: Replace = %v, %v; want ErrInvalidSummary", name, changed, err)
		}
	}
	if !reflect.DeepEqual(s.State(), before) {
		t.Fatal("a refused Replace changed the summary")
	}
}

func TestRestore(t *testing.T) {
	s := newSummary(t)
	s.Persisted(2)
	st := s.State()

	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State(), st) {
		t.Fatalf("Restore(State()) = %+v, want %+v", got.State(), st)
	}

	orphan := st
	orphan.Transcript = ""
	if _, err := Restore(orphan); err != nil {
		t.Fatalf("Restore without a transcript: %v", err)
	}

	broken := map[string]func(*State){
		"version 0":            func(s *State) { s.Version = 0 },
		"bad id":               func(s *State) { s.ID = "" },
		"id is not transcript": func(s *State) { s.ID = "other" },
		"bad lesson":           func(s *State) { s.Lesson = "l 1" },
		"empty text":           func(s *State) { s.Text = "" },
		"bad key point":        func(s *State) { s.KeyPoints = []string{""} },
		"no prompt version":    func(s *State) { s.PromptVersion = 0 },
		"no creation time":     func(s *State) { s.CreatedAt = time.Time{} },
	}
	for name, mutate := range broken {
		t.Run(name, func(t *testing.T) {
			b := st
			b.KeyPoints = append([]string(nil), st.KeyPoints...)
			mutate(&b)
			if r, err := Restore(b); !errors.Is(err, ErrInvalidSummary) || r != nil {
				t.Fatalf("Restore = %v, %v; want ErrInvalidSummary", r, err)
			}
		})
	}
}
