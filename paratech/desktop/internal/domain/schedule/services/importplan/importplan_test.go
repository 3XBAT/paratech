package importplan

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func at(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

func content(title string, start time.Time) lesson.Content {
	return lesson.Content{Title: title, Start: start, End: start.Add(time.Hour), TimeZone: "UTC"}
}

func row(line int, key string, c lesson.Content) Row { return Row{Line: line, Key: key, Content: c} }

func stored(t *testing.T, id, uid string, c lesson.Content, deleted bool) *lesson.Lesson {
	t.Helper()
	s := lesson.State{
		Version: 2, ID: shared.LessonID(id), Content: c, SourceUID: uid, ContentRevision: 1,
		CreatedAt: t0, UpdatedAt: t0,
	}
	if deleted {
		s.DeletedAt = t0
	}
	l, err := lesson.Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSource(t *testing.T) {
	for _, s := range []Source{SourceCSV, SourceICS, SourceXLSX} {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	if err := Source("pdf").Validate(); !errors.Is(err, ErrUnknownSource) {
		t.Errorf("unknown source: %v", err)
	}
	if _, err := Build("pdf", Parsed{}, nil); !errors.Is(err, ErrUnknownSource) {
		t.Errorf("Build with unknown source: %v", err)
	}
	if SourceCSV.UID("id:1") != "csv:id:1" || !SourceCSV.Owns("csv:id:1") || SourceCSV.Owns("csvx:1") ||
		SourceCSV.Owns("ics:1") || SourceCSV.Owns("") {
		t.Error("namespace rules broken")
	}
}

func TestBuildErrors(t *testing.T) {
	good := content("Math", at(0))
	bad := good
	bad.End = bad.Start

	tests := []struct {
		name   string
		parsed Parsed
		want   []RowError
	}{
		{"empty file", Parsed{}, []RowError{{Line: 0, Reason: "the file contains no lessons"}}},
		{
			"parser errors are kept and sorted",
			Parsed{Rows: []Row{row(2, "a", good)}, Errors: []RowError{{Line: 5, Field: "date", Reason: "x"}, {Line: 3, Field: "start", Reason: "y"}}},
			[]RowError{{Line: 3, Field: "start", Reason: "y"}, {Line: 5, Field: "date", Reason: "x"}},
		},
		{"invalid content", Parsed{Rows: []Row{row(2, "a", good), row(3, "b", bad)}}, []RowError{{Line: 3, Reason: "end is not after start"}}},
		{"empty key", Parsed{Rows: []Row{row(2, " ", good)}}, []RowError{{Line: 2, Field: "uid", Reason: "no lesson identity"}}},
		{"long key", Parsed{Rows: []Row{row(2, strings.Repeat("k", lesson.MaxSourceUIDBytes), good)}},
			[]RowError{{Line: 2, Field: "uid", Reason: "source uid longer than 512 bytes"}}},
		{"duplicate key", Parsed{Rows: []Row{row(2, "a", good), row(4, "a", content("Other", at(5)))}},
			[]RowError{{Line: 4, Reason: "the same lesson as line 2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := []*lesson.Lesson{stored(t, "l1", "csv:a", good, false)}
			p, err := Build(SourceCSV, tt.parsed, current)
			if err != nil {
				t.Fatal(err)
			}
			if p.Valid() || len(p.Errors) != len(tt.want) {
				t.Fatalf("errors = %+v, want %+v", p.Errors, tt.want)
			}
			for i := range tt.want {
				if p.Errors[i] != tt.want[i] {
					t.Errorf("error %d = %+v, want %+v", i, p.Errors[i], tt.want[i])
				}
			}
			if p.HasChanges() || len(p.Unchanged)+len(p.Conflicts) > 0 || p.Fingerprint != "" {
				t.Errorf("a plan with errors has a diff: %+v", p)
			}
		})
	}

	t.Run("too many rows", func(t *testing.T) {
		rows := make([]Row, MaxRows+1)
		for i := range rows {
			rows[i] = row(i+2, "k"+string(rune('a'+i%26))+strings.Repeat("x", i/26), content("M", at(i*2)))
		}
		p, err := Build(SourceCSV, Parsed{Rows: rows}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.Valid() || p.Errors[0].Line != 0 {
			t.Fatalf("errors = %+v, want a file error", p.Errors)
		}
	})
}

func TestBuildDiff(t *testing.T) {
	math, phys := content("Math", at(0)), content("Physics", at(2))
	movedPhys := content("Physics", at(3))
	current := []*lesson.Lesson{
		stored(t, "l-math", "csv:math", math, false),
		stored(t, "l-phys", "csv:phys", phys, false),
		stored(t, "l-chem", "csv:chem", content("Chem", at(6)), false),        // gone from the file
		stored(t, "l-old", "csv:old", content("Old", at(8)), true),            // deleted already: ignored
		stored(t, "l-manual", "", content("Manual", at(10)), false),           // manual: never removed
		stored(t, "l-ics", "ics:x", content("From ICS", at(12)), false),       // other source: never removed
		stored(t, "l-csvx", "csvx:math", content("Lookalike", at(14)), false), // other prefix
	}
	parsed := Parsed{Rows: []Row{
		row(2, "math", math),
		row(3, "phys", movedPhys),
		row(4, "bio", content("Bio", at(20))),
		row(5, "old", content("Old", at(8))), // re-import of a deleted occurrence: a new lesson
	}}

	p, err := Build(SourceCSV, parsed, current)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Valid() || !p.HasChanges() || len(p.Fingerprint) != 64 {
		t.Fatalf("plan not valid: %+v", p)
	}

	if len(p.Added) != 2 || p.Added[0].UID != "csv:bio" || p.Added[0].Line != 4 || p.Added[1].UID != "csv:old" {
		t.Errorf("added = %+v", p.Added)
	}
	if len(p.Changed) != 1 || p.Changed[0].Lesson != "l-phys" || p.Changed[0].Version != 2 ||
		!p.Changed[0].Before.Equal(phys) || !p.Changed[0].After.Equal(movedPhys) {
		t.Errorf("changed = %+v", p.Changed)
	}
	if len(p.Unchanged) != 1 || p.Unchanged[0].Lesson != "l-math" {
		t.Errorf("unchanged = %+v", p.Unchanged)
	}
	if len(p.Removed) != 1 || p.Removed[0].Lesson != "l-chem" || p.Removed[0].UID != "csv:chem" {
		t.Errorf("removed = %+v", p.Removed)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("conflicts = %+v", p.Conflicts)
	}

	again, err := Build(SourceCSV, parsed, current)
	if err != nil || again.Fingerprint != p.Fingerprint {
		t.Fatalf("fingerprint not deterministic: %v", err)
	}
}

func TestBuildReimportIsNoOp(t *testing.T) {
	math := content("Math", at(0))
	p, err := Build(SourceCSV, Parsed{Rows: []Row{row(2, "math", math)}}, []*lesson.Lesson{stored(t, "l1", "csv:math", math, false)})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Valid() || p.HasChanges() || len(p.Unchanged) != 1 {
		t.Fatalf("plan = %+v, want only unchanged", p)
	}
}

func TestBuildConflicts(t *testing.T) {
	manual := stored(t, "l-manual", "", content("Manual", at(0)), false)
	ics := stored(t, "l-ics", "ics:1", content("ICS", at(10)), false)
	replaced := stored(t, "l-replaced", "csv:r", content("Replaced", at(20)), false)
	removed := stored(t, "l-removed", "csv:gone", content("Removed", at(30)), false)
	current := []*lesson.Lesson{manual, ics, replaced, removed}

	half := 30 * time.Minute
	rows := []Row{
		row(2, "a", content("A", at(0).Add(half))),  // overlaps the manual lesson
		row(3, "b", content("B", at(10))),           // overlaps the ICS lesson
		row(4, "r", content("R", at(20).Add(half))), // replaces its own lesson: no conflict with the old time
		row(5, "c", content("C", at(30))),           // the old lesson there is removed: no conflict
		row(6, "d", content("D", at(40))),
		row(7, "e", content("E", at(40).Add(half))), // overlaps row 6
		row(8, "f", content("F", at(41))),           // touches row 6 and overlaps row 7
	}
	p, err := Build(SourceCSV, Parsed{Rows: rows}, current)
	if err != nil {
		t.Fatal(err)
	}
	want := []Conflict{
		{Line: 2, UID: "csv:a", WithLesson: "l-manual"},
		{Line: 3, UID: "csv:b", WithLesson: "l-ics"},
		{Line: 6, UID: "csv:d", WithLine: 7},
		{Line: 7, UID: "csv:e", WithLine: 8},
	}
	if len(p.Conflicts) != len(want) {
		t.Fatalf("conflicts = %+v, want %d", p.Conflicts, len(want))
	}
	for i, w := range want {
		g := p.Conflicts[i]
		if g.Line != w.Line || g.UID != w.UID || g.WithLine != w.WithLine || g.WithLesson != w.WithLesson {
			t.Errorf("conflict %d = %+v, want %+v", i, g, w)
		}
	}
	if !p.Conflicts[0].WithContent.Equal(manual.Content()) || !p.Conflicts[0].Content.Equal(rows[0].Content) {
		t.Errorf("conflict contents not kept: %+v", p.Conflicts[0])
	}
	if !p.Valid() {
		t.Fatal("conflicts must not make the plan invalid")
	}

	// A new overlapping manual lesson changes the plan, so a preview made before it does not match.
	other, err := Build(SourceCSV, Parsed{Rows: rows}, append(current, stored(t, "l-new", "", content("New", at(40)), false)))
	if err != nil {
		t.Fatal(err)
	}
	if other.Fingerprint == p.Fingerprint {
		t.Error("a new conflict did not change the fingerprint")
	}
}

func TestBuildConflictsTruncated(t *testing.T) {
	rows := make([]Row, 50)
	for i := range rows {
		rows[i] = row(i+2, "k"+strings.Repeat("x", i), content("Same", at(0)))
	}
	p, err := Build(SourceCSV, Parsed{Rows: rows}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != MaxConflicts || !p.ConflictsTruncated {
		t.Fatalf("%d conflicts, truncated %v; want %d and true", len(p.Conflicts), p.ConflictsTruncated, MaxConflicts)
	}
}

func TestFingerprintChangesWithVersion(t *testing.T) {
	math := content("Math", at(0))
	parsed := Parsed{Rows: []Row{row(2, "math", math)}}
	a, _ := Build(SourceCSV, parsed, []*lesson.Lesson{stored(t, "l1", "csv:math", math, false)})

	l, err := lesson.Restore(lesson.State{
		Version: 3, ID: "l1", Content: math, SourceUID: "csv:math", ContentRevision: 1, CreatedAt: t0, UpdatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Build(SourceCSV, parsed, []*lesson.Lesson{l})
	if a.Fingerprint == b.Fingerprint {
		t.Error("a lesson saved meanwhile did not change the fingerprint")
	}
}
