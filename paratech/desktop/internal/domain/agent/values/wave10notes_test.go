package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNotesResult(t *testing.T) {
	n1, n2, s1 := mustRef(t, KindNote, "n1"), mustRef(t, KindNote, "n2"), mustRef(t, KindSubject, "s1")
	res, seen, err := NotesResult("c1", []NoteRow{
		{ID: n1, Subject: s1, Anchor: NoteAtLesson, Date: day(t, "2026-10-24"), Parts: 3, NoTextParts: 2},
		{ID: n2, Subject: s1, Anchor: NoteUndated, Parts: 1, NoTextParts: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"count":2,"notes":[{"id":"n1","subject":"s1","anchor":"lesson","date":"2026-10-24","parts":3,"parts_without_text":2},` +
		`{"id":"n2","subject":"s1","anchor":"undated","parts":1,"parts_without_text":1}],"truncated":false}`
	if res.ResultJSON != want {
		t.Fatalf("result = %s", res.ResultJSON)
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %v", seen)
	}

	bad := []NoteRow{
		{ID: mustRef(t, KindAssignment, "a1"), Anchor: NoteUndated},
		{ID: n1, Anchor: "week", Date: day(t, "2026-10-24")},
		{ID: n1, Anchor: NoteAtDay},
		{ID: n1, Anchor: NoteUndated, Date: day(t, "2026-10-24")},
	}
	for i, r := range bad {
		if _, _, err := NotesResult("c1", []NoteRow{r}); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("bad row %d = %v", i, err)
		}
	}
}

func TestNotesResultIsBounded(t *testing.T) {
	rows := make([]NoteRow, 0, MaxReadItems+5)
	for i := range MaxReadItems + 5 {
		rows = append(rows, NoteRow{ID: mustRef(t, KindNote, fmt.Sprintf("n%d", i)), Anchor: NoteAtDay, Date: day(t, "2026-10-24"), Parts: 1})
	}
	res, seen, err := NotesResult("c1", rows)
	if err != nil || len(seen) != MaxReadItems || !strings.Contains(res.ResultJSON, `"truncated":true`) ||
		!strings.Contains(res.ResultJSON, fmt.Sprintf(`"count":%d`, MaxReadItems+5)) {
		t.Fatalf("result = %s, seen %d, %v", res.ResultJSON, len(seen), err)
	}
}

func TestNoteResult(t *testing.T) {
	n1 := mustRef(t, KindNote, "n1")
	tests := []struct {
		name string
		row  NoteWriteRow
		want string
		err  error
	}{
		{"add to a new note", NoteWriteRow{Note: n1, Changed: true, Created: true, Parts: 1}, `{"changed":true,"created":true,"done":true,"id":"n1","parts":1}`, nil},
		{"rename", NoteWriteRow{Note: n1, Changed: true}, `{"changed":true,"done":true,"id":"n1"}`, nil},
		{"nothing changed", NoteWriteRow{Note: n1}, `{"changed":false,"done":true,"id":"n1"}`, nil},
		{"a made note changed something", NoteWriteRow{Note: n1, Created: true}, "", ErrInvalidResult},
		{"a note id only", NoteWriteRow{Note: mustRef(t, KindMaterial, "m1"), Changed: true}, "", ErrInvalidResult},
	}
	for _, tt := range tests {
		res, seen, err := NoteResult("c1", tt.row)
		if !errors.Is(err, tt.err) || (tt.err == nil && (err != nil || res.ResultJSON != tt.want || len(seen) != 1)) {
			t.Errorf("%s: NoteResult = %s, %v, %v", tt.name, res.ResultJSON, seen, err)
		}
	}
}
