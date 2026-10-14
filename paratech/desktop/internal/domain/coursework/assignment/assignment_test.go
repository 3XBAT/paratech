package assignment

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

func details(t *testing.T, title, description string, due time.Time) Details {
	t.Helper()
	d, err := NewDetails(title, description, due)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func extracted(t *testing.T) *Assignment {
	t.Helper()
	a, err := NewExtracted(Spec{
		ID: "a1", Lesson: "l1", Origin: Origin{Transcript: "t1", Index: 0}, PromptVersion: 1,
		Details: details(t, "Read chapter 4", "Pages 10-20", t0.Add(72*time.Hour)),
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewDetails(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	cases := []struct {
		name        string
		title, desc string
		due         time.Time
		wantErr     bool
	}{
		{"valid", "Essay", "Two pages\nabout rivers\twith a table", t0, false},
		{"no deadline", "Essay", "", time.Time{}, false},
		{"trimmed", "  Essay  ", "  x  ", time.Time{}, false},
		{"deadline in another zone", "Essay", "", t0.In(msk), false},
		{"title at the limit", strings.Repeat("я", MaxTitleRunes), "", time.Time{}, false},
		{"empty title", "", "x", time.Time{}, true},
		{"blank title", "   ", "x", time.Time{}, true},
		{"title too long", strings.Repeat("a", MaxTitleRunes+1), "", time.Time{}, true},
		{"title with a newline", "a\nb", "", time.Time{}, true},
		{"title with a line separator", "a b", "", time.Time{}, true},
		{"title invalid utf8", "\xff", "", time.Time{}, true},
		{"description too long", "Essay", strings.Repeat("a", MaxDescriptionRunes+1), time.Time{}, true},
		{"description with a control character", "Essay", "a\x00b", time.Time{}, true},
		{"description invalid utf8", "Essay", "\xff", time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDetails(tc.title, tc.desc, tc.due)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDetails) || !d.IsZero() {
					t.Fatalf("NewDetails = %+v, %v; want ErrInvalidDetails and the zero value", d, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Title() != strings.TrimSpace(tc.title) || d.Description() != strings.TrimSpace(tc.desc) || !d.Due().Equal(tc.due) {
				t.Fatalf("details = %q %q %v", d.Title(), d.Description(), d.Due())
			}
			if !tc.due.IsZero() && d.Due().Location() != time.UTC {
				t.Fatalf("due location = %v, want UTC", d.Due().Location())
			}
		})
	}
}

func TestNewExtracted(t *testing.T) {
	d := details(t, "Essay", "", time.Time{})
	cases := []struct {
		name    string
		spec    Spec
		now     time.Time
		wantErr bool
	}{
		{"valid", Spec{ID: "a1", Origin: Origin{Transcript: "t1"}, PromptVersion: 1, Details: d}, t0, false},
		{"with lesson", Spec{ID: "a1", Lesson: "l1", Origin: Origin{Transcript: "t1", Index: 3}, PromptVersion: 2, Details: d}, t0, false},
		{"no id", Spec{Origin: Origin{Transcript: "t1"}, PromptVersion: 1, Details: d}, t0, true},
		{"bad id", Spec{ID: "a/1", Origin: Origin{Transcript: "t1"}, PromptVersion: 1, Details: d}, t0, true},
		{"bad lesson", Spec{ID: "a1", Lesson: "l 1", Origin: Origin{Transcript: "t1"}, PromptVersion: 1, Details: d}, t0, true},
		{"no transcript", Spec{ID: "a1", PromptVersion: 1, Details: d}, t0, true},
		{"negative index", Spec{ID: "a1", Origin: Origin{Transcript: "t1", Index: -1}, PromptVersion: 1, Details: d}, t0, true},
		{"no prompt version", Spec{ID: "a1", Origin: Origin{Transcript: "t1"}, Details: d}, t0, true},
		{"no details", Spec{ID: "a1", Origin: Origin{Transcript: "t1"}, PromptVersion: 1}, t0, true},
		{"no time", Spec{ID: "a1", Origin: Origin{Transcript: "t1"}, PromptVersion: 1, Details: d}, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewExtracted(tc.spec, tc.now)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidAssignment) || a != nil {
					t.Fatalf("NewExtracted = %v, %v; want ErrInvalidAssignment", a, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			o, ok := a.Origin()
			if a.Version() != 0 || a.UserEdited() || !ok || o != tc.spec.Origin || a.LessonID() != tc.spec.Lesson ||
				a.PromptVersion() != tc.spec.PromptVersion || !a.Details().Equal(d) || !a.CreatedAt().Equal(tc.now) {
				t.Fatalf("new assignment = %+v", a.State())
			}
		})
	}
}

func TestReextractUpdatesAnUneditedAssignment(t *testing.T) {
	a := extracted(t)
	later := t0.Add(time.Hour)

	changed, err := a.Reextract(a.Details(), 1, later)
	if err != nil || changed {
		t.Fatalf("Reextract with the same details = %v, %v; want unchanged", changed, err)
	}

	newer := details(t, "Read chapter 5", "", time.Time{})
	changed, err = a.Reextract(newer, 2, later)
	if err != nil || !changed {
		t.Fatalf("Reextract = %v, %v; want changed", changed, err)
	}
	if !a.Details().Equal(newer) || a.PromptVersion() != 2 || !a.UpdatedAt().Equal(later) || a.UserEdited() {
		t.Fatalf("after Reextract: %+v", a.State())
	}

	// A new prompt version alone is a change: the stored version must tell which prompts produced the values.
	changed, err = a.Reextract(newer, 3, later)
	if err != nil || !changed || a.PromptVersion() != 3 {
		t.Fatalf("Reextract with a new prompt version = %v, %v", changed, err)
	}
}

// The invariant of AC-3 of TASK-042: user edits are never overwritten by reprocessing.
func TestReextractKeepsAUserEditedAssignment(t *testing.T) {
	a := extracted(t)
	mine := details(t, "My title", "My notes", t0.Add(24*time.Hour))
	if err := a.Edit(mine, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	changed, err := a.Reextract(details(t, "Model title", "", time.Time{}), 2, t0.Add(time.Hour))
	if err != nil || changed {
		t.Fatalf("Reextract of an edited assignment = %v, %v; want unchanged", changed, err)
	}
	if !a.Details().Equal(mine) || a.PromptVersion() != 1 || !a.UpdatedAt().Equal(t0.Add(time.Minute)) {
		t.Fatalf("edited assignment changed: %+v", a.State())
	}
}

func TestReextractRefusals(t *testing.T) {
	d := details(t, "x", "", time.Time{})
	notExtracted, err := Restore(State{
		Version: 1, ID: "a2", SourceIndex: -1, Title: "x", CreatedAt: t0, UpdatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		a       *Assignment
		d       Details
		version int
		want    error
	}{
		{"not extracted", notExtracted, d, 1, ErrNotExtracted},
		{"zero details", extracted(t), Details{}, 1, ErrInvalidDetails},
		{"no prompt version", extracted(t), d, 0, ErrInvalidAssignment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.a.State()
			changed, err := tc.a.Reextract(tc.d, tc.version, t0.Add(time.Hour))
			if !errors.Is(err, tc.want) || changed {
				t.Fatalf("Reextract = %v, %v; want %v", changed, err, tc.want)
			}
			if tc.a.State() != before {
				t.Fatal("a refused Reextract changed the assignment")
			}
		})
	}
}

func TestEdit(t *testing.T) {
	a := extracted(t)
	if err := a.Edit(Details{}, t0); !errors.Is(err, ErrInvalidDetails) || a.UserEdited() {
		t.Fatalf("Edit with zero details = %v, edited %v", err, a.UserEdited())
	}

	// Clearing the deadline is an edit too.
	d := details(t, "Read chapter 4", "Pages 10-20", time.Time{})
	if err := a.Edit(d, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !a.UserEdited() || !a.Details().Due().IsZero() || !a.UpdatedAt().Equal(t0.Add(time.Minute)) {
		t.Fatalf("after Edit: %+v", a.State())
	}

	// Confirming unchanged details still protects them.
	b := extracted(t)
	if err := b.Edit(b.Details(), t0.Add(time.Minute)); err != nil || !b.UserEdited() {
		t.Fatalf("Edit with unchanged details = %v, edited %v", err, b.UserEdited())
	}
}

func TestRestore(t *testing.T) {
	good := extracted(t)
	good.Persisted(3)
	if err := good.Edit(good.Details(), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	st := good.State()

	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.State() != st {
		t.Fatalf("Restore(State()) = %+v, want %+v", got.State(), st)
	}

	// The transcript was deleted: the row keeps its index, but the origin is gone.
	orphan := st
	orphan.Transcript = ""
	o, err := Restore(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Origin(); ok {
		t.Fatal("an assignment without a transcript reports an origin")
	}

	broken := map[string]func(*State){
		"version 0":                  func(s *State) { s.Version = 0 },
		"bad id":                     func(s *State) { s.ID = "" },
		"bad transcript":             func(s *State) { s.Transcript = "t 1" },
		"bad lesson":                 func(s *State) { s.Lesson = "l\n" },
		"empty title":                func(s *State) { s.Title = "" },
		"source below -1":            func(s *State) { s.SourceIndex = -2 },
		"extracted without version":  func(s *State) { s.PromptVersion = 0 },
		"negative prompt version":    func(s *State) { s.SourceIndex, s.PromptVersion = -1, -1 },
		"no creation time":           func(s *State) { s.CreatedAt = time.Time{} },
		"no update time":             func(s *State) { s.UpdatedAt = time.Time{} },
		"description with a control": func(s *State) { s.Description = "\x07" },
	}
	for name, mutate := range broken {
		t.Run(name, func(t *testing.T) {
			s := st
			mutate(&s)
			if a, err := Restore(s); !errors.Is(err, ErrInvalidAssignment) || a != nil {
				t.Fatalf("Restore = %v, %v; want ErrInvalidAssignment", a, err)
			}
		})
	}
}

func TestPersisted(t *testing.T) {
	a := extracted(t)
	a.Persisted(5)
	if a.Version() != 5 || a.State().Version != 5 {
		t.Fatalf("version = %d", a.Version())
	}
}
