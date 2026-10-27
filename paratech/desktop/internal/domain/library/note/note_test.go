package note

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var (
	t0 = time.Date(2026, time.October, 25, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func dayAnchor(t *testing.T, date string) Anchor {
	t.Helper()
	a, err := NewAnchor(AnchorSpec{Kind: AnchorDay, Date: mustDate(t, date)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func validSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{ID: "n1", Subject: "s1", Anchor: dayAnchor(t, "2026-09-12"), Title: "Лекция 1", Parts: []shared.MaterialID{"m1", "m2"}}
}

func newNote(t *testing.T) *Note {
	t.Helper()
	n, err := New(validSpec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func removedNote(t *testing.T) *Note {
	t.Helper()
	n := newNote(t)
	if _, err := n.Remove(t0); err != nil {
		t.Fatal(err)
	}
	return n
}

func manyParts(k int) []shared.MaterialID {
	out := make([]shared.MaterialID, k)
	for i := range out {
		out[i] = shared.MaterialID(fmt.Sprintf("m%d", i+1))
	}
	return out
}

func TestNew(t *testing.T) {
	n := newNote(t)
	if n.Version() != 0 || n.ID() != "n1" || n.Subject() != "s1" || n.Title() != "Лекция 1" || !n.Active() ||
		n.Status() != StatusActive || !slices.Equal(n.Parts(), []shared.MaterialID{"m1", "m2"}) ||
		n.Anchor().Kind() != AnchorDay || !n.CreatedAt().Equal(t0) || !n.UpdatedAt().Equal(t0) {
		t.Fatalf("New = %+v", n.State())
	}

	tests := []struct {
		name   string
		modify func(*Spec)
		err    error
	}{
		{"no id", func(s *Spec) { s.ID = "" }, ErrInvalidNote},
		{"bad id", func(s *Spec) { s.ID = "a/b" }, ErrInvalidNote},
		{"no subject", func(s *Spec) { s.Subject = "" }, ErrInvalidNote},
		{"no anchor", func(s *Spec) { s.Anchor = Anchor{} }, ErrInvalidAnchor},
		{"empty title", func(s *Spec) { s.Title = " \n\t " }, ErrEmptyTitle},
		{"no parts", func(s *Spec) { s.Parts = nil }, ErrNoParts},
		{"too many parts", func(s *Spec) { s.Parts = manyParts(MaxParts + 1) }, ErrTooManyParts},
		{"repeated part", func(s *Spec) { s.Parts = []shared.MaterialID{"m1", "m2", "m1"} }, ErrDuplicatePart},
		{"bad part", func(s *Spec) { s.Parts = []shared.MaterialID{"m1", ""} }, ErrInvalidNote},
		{"max parts", func(s *Spec) { s.Parts = manyParts(MaxParts) }, nil},
		{"one part", func(s *Spec) { s.Parts = []shared.MaterialID{"m1"} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSpec(t)
			tt.modify(&spec)
			_, err := New(spec, t0)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("New = %v, want %v", err, tt.err)
			}
		})
	}
	if _, err := New(validSpec(t), time.Time{}); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("New without a time = %v", err)
	}
}

func TestNewCleansTitleAndCopiesParts(t *testing.T) {
	spec := validSpec(t)
	spec.Title = "  Ряды\r\nТейлора \u200b" + strings.Repeat("ж", MaxTitleRunes)
	n, err := New(spec, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n.Title(), "Ряды Тейлора ж") || len([]rune(n.Title())) != MaxTitleRunes {
		t.Fatalf("title = %q (%d runes)", n.Title(), len([]rune(n.Title())))
	}
	spec.Parts[0] = "changed"
	parts := n.Parts()
	parts[1] = "changed"
	if !slices.Equal(n.Parts(), []shared.MaterialID{"m1", "m2"}) {
		t.Fatalf("parts share memory with the caller: %v", n.Parts())
	}
}

func TestRestore(t *testing.T) {
	good := newNote(t).State()
	good.Version = 3
	n, err := Restore(good)
	if err != nil || !reflect.DeepEqual(n.State(), good) || n.Version() != 3 {
		t.Fatalf("Restore = %+v, %v", n, err)
	}
	removed := good
	removed.Status = StatusRemoved
	if _, err := Restore(removed); err != nil {
		t.Fatalf("Restore(removed) = %v", err)
	}

	tests := []struct {
		name   string
		modify func(*State)
		err    error
	}{
		{"never stored", func(s *State) { s.Version = 0 }, ErrInvalidNote},
		{"unclean title", func(s *State) { s.Title = " Лекция  1" }, ErrInvalidNote},
		{"empty title", func(s *State) { s.Title = "" }, ErrEmptyTitle},
		{"unknown status", func(s *State) { s.Status = "archived" }, ErrInvalidNote},
		{"no anchor", func(s *State) { s.Anchor = Anchor{} }, ErrInvalidAnchor},
		{"no parts", func(s *State) { s.Parts = nil }, ErrNoParts},
		{"repeated part", func(s *State) { s.Parts = []shared.MaterialID{"m1", "m1"} }, ErrDuplicatePart},
		{"no subject", func(s *State) { s.Subject = "" }, ErrInvalidNote},
		{"no update time", func(s *State) { s.UpdatedAt = time.Time{} }, ErrInvalidNote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := good
			st.Parts = slices.Clone(good.Parts)
			tt.modify(&st)
			if _, err := Restore(st); !errors.Is(err, tt.err) {
				t.Fatalf("Restore = %v, want %v", err, tt.err)
			}
		})
	}
}

// Every pair of statuses: the table allows active→removed and removed→active and nothing else.
func TestTransitionTable(t *testing.T) {
	for _, from := range Statuses() {
		for _, to := range Statuses() {
			n := newNote(t)
			n.status = from
			err := n.transition(to, t1)
			allowed := from != to
			if allowed != (err == nil) || (!allowed && !errors.Is(err, ErrInvalidTransition)) {
				t.Errorf("%s → %s: %v", from, to, err)
			}
			if allowed && (n.Status() != to || !n.UpdatedAt().Equal(t1)) {
				t.Errorf("%s → %s: status %s, updated %v", from, to, n.Status(), n.UpdatedAt())
			}
		}
	}
	n := newNote(t)
	if err := n.transition("archived", t1); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("active → archived: %v", err)
	}
}

func TestRemoveAndRestore(t *testing.T) {
	n := newNote(t)
	before := n.State()
	if changed, err := n.Remove(t1); !changed || err != nil || n.Active() || !n.UpdatedAt().Equal(t1) {
		t.Fatalf("Remove = %v, %v; status %s", changed, err, n.Status())
	}
	if changed, err := n.Remove(t1.Add(time.Hour)); changed || err != nil || !n.UpdatedAt().Equal(t1) {
		t.Fatalf("Remove of a removed note = %v, %v", changed, err)
	}
	if changed, err := n.Restore(t1); !changed || err != nil || !n.Active() {
		t.Fatalf("Restore = %v, %v", changed, err)
	}
	if changed, err := n.Restore(t1.Add(time.Hour)); changed || err != nil {
		t.Fatalf("Restore of an active note = %v, %v", changed, err)
	}
	after := n.State()
	after.UpdatedAt = before.UpdatedAt
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Remove and Restore changed the note: %+v, want %+v", after, before)
	}
}

// A removed note refuses every change but Restore and keeps its state.
func TestRemovedNoteRefusesChanges(t *testing.T) {
	other := dayAnchor(t, "2026-09-19")
	changes := map[string]func(*Note) (bool, error){
		"AddParts":    func(n *Note) (bool, error) { return n.AddParts([]shared.MaterialID{"m3"}, t1) },
		"ReplacePart": func(n *Note) (bool, error) { return n.ReplacePart("m1", "m3", t1) },
		"Reorder":     func(n *Note) (bool, error) { return n.Reorder([]shared.MaterialID{"m2", "m1"}, t1) },
		"Reanchor":    func(n *Note) (bool, error) { return n.Reanchor(other, t1) },
		"Retitle":     func(n *Note) (bool, error) { return n.Retitle("Другое", t1) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			n := removedNote(t)
			before := n.State()
			if changed, err := change(n); changed || !errors.Is(err, ErrRemoved) {
				t.Fatalf("%s on a removed note = %v, %v", name, changed, err)
			}
			if !reflect.DeepEqual(n.State(), before) {
				t.Fatalf("%s changed a removed note", name)
			}
		})
	}
}

func TestAddParts(t *testing.T) {
	tests := []struct {
		name    string
		start   []shared.MaterialID
		add     []shared.MaterialID
		want    []shared.MaterialID
		changed bool
		err     error
	}{
		{"new ones in order", []shared.MaterialID{"m1"}, []shared.MaterialID{"m3", "m2"}, []shared.MaterialID{"m1", "m3", "m2"}, true, nil},
		{"present ones skipped", []shared.MaterialID{"m1", "m2"}, []shared.MaterialID{"m2", "m3", "m3"}, []shared.MaterialID{"m1", "m2", "m3"}, true, nil},
		{"nothing new", []shared.MaterialID{"m1", "m2"}, []shared.MaterialID{"m2", "m1"}, []shared.MaterialID{"m1", "m2"}, false, nil},
		{"empty", []shared.MaterialID{"m1"}, nil, []shared.MaterialID{"m1"}, false, ErrNoParts},
		{"bad id", []shared.MaterialID{"m1"}, []shared.MaterialID{"m2", "a b"}, []shared.MaterialID{"m1"}, false, ErrInvalidNote},
		{"up to the limit", manyParts(MaxParts - 1), []shared.MaterialID{"x"}, append(manyParts(MaxParts-1), "x"), true, nil},
		{"over the limit", manyParts(MaxParts - 1), []shared.MaterialID{"x", "y"}, manyParts(MaxParts - 1), false, ErrTooManyParts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSpec(t)
			spec.Parts = tt.start
			n, err := New(spec, t0)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := n.AddParts(tt.add, t1)
			if changed != tt.changed || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("AddParts = %v, %v; want %v, %v", changed, err, tt.changed, tt.err)
			}
			if !slices.Equal(n.Parts(), tt.want) {
				t.Fatalf("parts = %v, want %v", n.Parts(), tt.want)
			}
			if n.UpdatedAt().Equal(t1) != changed {
				t.Fatalf("updated at %v, changed %v", n.UpdatedAt(), changed)
			}
		})
	}
}

func TestReplacePart(t *testing.T) {
	tests := []struct {
		name      string
		old, next shared.MaterialID
		want      []shared.MaterialID
		changed   bool
		err       error
	}{
		{"in place", "m1", "m3", []shared.MaterialID{"m3", "m2"}, true, nil},
		{"by itself", "m2", "m2", []shared.MaterialID{"m1", "m2"}, false, nil},
		{"old is not a part", "m9", "m3", []shared.MaterialID{"m1", "m2"}, false, ErrPartNotFound},
		{"next is another part", "m1", "m2", []shared.MaterialID{"m1", "m2"}, false, ErrDuplicatePart},
		{"bad next", "m1", "", []shared.MaterialID{"m1", "m2"}, false, ErrInvalidNote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newNote(t)
			changed, err := n.ReplacePart(tt.old, tt.next, t1)
			if changed != tt.changed || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("ReplacePart = %v, %v; want %v, %v", changed, err, tt.changed, tt.err)
			}
			if !slices.Equal(n.Parts(), tt.want) || n.UpdatedAt().Equal(t1) != changed {
				t.Fatalf("parts = %v, updated %v", n.Parts(), n.UpdatedAt())
			}
		})
	}
}

func TestReorder(t *testing.T) {
	tests := []struct {
		name    string
		order   []shared.MaterialID
		want    []shared.MaterialID
		changed bool
		err     error
	}{
		{"swapped", []shared.MaterialID{"m2", "m1"}, []shared.MaterialID{"m2", "m1"}, true, nil},
		{"same order", []shared.MaterialID{"m1", "m2"}, []shared.MaterialID{"m1", "m2"}, false, nil},
		{"missing part", []shared.MaterialID{"m2"}, []shared.MaterialID{"m1", "m2"}, false, ErrInvalidOrder},
		{"extra part", []shared.MaterialID{"m2", "m1", "m3"}, []shared.MaterialID{"m1", "m2"}, false, ErrInvalidOrder},
		{"unknown part", []shared.MaterialID{"m2", "m3"}, []shared.MaterialID{"m1", "m2"}, false, ErrInvalidOrder},
		{"repeated part", []shared.MaterialID{"m2", "m2"}, []shared.MaterialID{"m1", "m2"}, false, ErrInvalidOrder},
		{"empty", nil, []shared.MaterialID{"m1", "m2"}, false, ErrInvalidOrder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newNote(t)
			changed, err := n.Reorder(tt.order, t1)
			if changed != tt.changed || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Reorder = %v, %v; want %v, %v", changed, err, tt.changed, tt.err)
			}
			if !slices.Equal(n.Parts(), tt.want) || n.UpdatedAt().Equal(t1) != changed {
				t.Fatalf("parts = %v, updated %v", n.Parts(), n.UpdatedAt())
			}
		})
	}
	n := newNote(t)
	order := []shared.MaterialID{"m2", "m1"}
	if _, err := n.Reorder(order, t1); err != nil {
		t.Fatal(err)
	}
	order[0] = "changed"
	if !slices.Equal(n.Parts(), []shared.MaterialID{"m2", "m1"}) {
		t.Fatalf("parts share memory with the order: %v", n.Parts())
	}
}

func TestReanchor(t *testing.T) {
	day := mustDate(t, "2026-09-12")
	rec, err := NewAnchor(AnchorSpec{Kind: AnchorRecording, Recording: "r1", Date: day, Title: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	undated, err := NewAnchor(AnchorSpec{Kind: AnchorUndated})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		anchor  Anchor
		changed bool
		err     error
	}{
		{"to a recording", rec, true, nil},
		{"to undated", undated, true, nil},
		{"the same day", dayAnchor(t, "2026-09-12"), false, nil},
		{"no anchor", Anchor{}, false, ErrInvalidAnchor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newNote(t)
			before := n.Anchor()
			changed, err := n.Reanchor(tt.anchor, t1)
			if changed != tt.changed || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Reanchor = %v, %v; want %v, %v", changed, err, tt.changed, tt.err)
			}
			want := before
			if changed {
				want = tt.anchor
			}
			if n.Anchor() != want || n.UpdatedAt().Equal(t1) != changed {
				t.Fatalf("anchor = %+v, updated %v", n.Anchor(), n.UpdatedAt())
			}
		})
	}

	// A deleted recording: the anchor falls back to its day and the note keeps everything else.
	n := newNote(t)
	if _, err := n.Reanchor(rec, t1); err != nil {
		t.Fatal(err)
	}
	if changed, err := n.Reanchor(n.Anchor().FallBackToDay(), t1); !changed || err != nil ||
		n.Anchor().Kind() != AnchorDay || n.Anchor().Date() != day || n.Anchor().Title() != "Физика" {
		t.Fatalf("fall back = %v, %v, %+v", changed, err, n.Anchor())
	}
}

func TestRetitle(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		want    string
		changed bool
		err     error
	}{
		{"new title", "  Ряды\nи пределы ", "Ряды и пределы", true, nil},
		{"same title", "Лекция  1", "Лекция 1", false, nil},
		{"cut", strings.Repeat("ф", MaxTitleRunes+5), strings.Repeat("ф", MaxTitleRunes), true, nil},
		{"empty", "\u200b \t", "Лекция 1", false, ErrEmptyTitle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newNote(t)
			changed, err := n.Retitle(tt.title, t1)
			if changed != tt.changed || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Retitle = %v, %v; want %v, %v", changed, err, tt.changed, tt.err)
			}
			if n.Title() != tt.want || n.UpdatedAt().Equal(t1) != changed {
				t.Fatalf("title = %q, updated %v", n.Title(), n.UpdatedAt())
			}
		})
	}
}

func TestPersisted(t *testing.T) {
	n := newNote(t)
	n.Persisted(1)
	if n.Version() != 1 || n.State().Version != 1 {
		t.Fatalf("version = %d", n.Version())
	}
}
