package knowledgebase

import (
	"slices"
	"testing"
	"time"
)

func TestPassagesOwnNotes(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	recorded := lessonText("r1", 20, Document{
		Summary:  []string{"Ряд Тейлора"},
		Segments: []Segment{{StartMs: 5000, Text: "ряд Тейлора в расшифровке"}},
		Notes: []NoteFragment{
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 3}, Text: "Мой ряд Тейлора"},
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 4}, Text: "про интегралы"},
		},
	})
	recorded.Lesson.Key = RecordingKey("r1")
	// Two note-only lectures: no recording, told apart by their keys.
	onlyNotes := LessonText{Lesson: Lesson{Key: DayKey("s1", day(15)), StartedAt: day(15)}, Document: Document{Notes: []NoteFragment{
		{Note: "n2", Material: "m2", Locator: Locator{Kind: "section", N: 2}, Text: "Ряды\u202e Тейлора\nРяд Тейлора сходится."},
	}}}
	undated := LessonText{Lesson: Lesson{Key: NoteKey("n3"), Notes: []Note{{ID: "n3", CreatedAt: day(1)}}}, Document: Document{Notes: []NoteFragment{
		{Note: "n3", Material: "m3", Locator: Locator{Kind: "fragment", N: 1}, Text: "ряд тейлора без даты"},
	}}}

	got := Passages("ряд Тейлора", []LessonText{undated, onlyNotes, recorded})
	type place struct {
		key  string
		sec  Section
		idx  int
		note string
		loc  Locator
	}
	var places []place
	for _, p := range got {
		places = append(places, place{p.Lesson.Key, p.Section, p.Index, string(p.Note), p.Locator})
	}
	want := []place{
		// Equal score: the newer lesson first; within it the summary, the transcript, then the student's note
		// (the order of TASK-152 stays: ADR-0027 Open 6 is open).
		{RecordingKey("r1"), SectionSummary, 0, "", Locator{}},
		{RecordingKey("r1"), SectionTranscript, 0, "", Locator{}},
		{RecordingKey("r1"), SectionOwnNote, 0, "n1", Locator{Kind: "page", N: 3}},
		{DayKey("s1", day(15)), SectionOwnNote, 0, "n2", Locator{Kind: "section", N: 2}},
		{NoteKey("n3"), SectionOwnNote, 0, "n3", Locator{Kind: "fragment", N: 1}},
	}
	if !slices.Equal(places, want) {
		t.Fatalf("places =\n%v\nwant\n%v", places, want)
	}
	if got[2].Material != "m1" || got[2].Recording() != "r1" || got[2].Heading != "" || got[2].StartMs != 0 {
		t.Fatalf("note passage of a recorded lesson %+v", got[2])
	}
	// A section keeps its heading line, cleaned; the text is cleaned as any lesson text.
	if got[3].Heading != "Ряды Тейлора" || got[3].Text != "Ряды Тейлора\nРяд Тейлора сходится." || got[3].Recording() != "" {
		t.Fatalf("section passage %+v", got[3])
	}
}

// Lessons are told apart by key: two note-only lectures without a recording both count; a lesson given twice once.
func TestPassagesKeys(t *testing.T) {
	a := LessonText{Lesson: Lesson{Key: LessonKey("l1"), StartedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
		Document: Document{Notes: []NoteFragment{{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 1}, Text: "ротор поля"}}}}
	b := LessonText{Lesson: Lesson{Key: LessonKey("l2"), StartedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		Document: Document{Notes: []NoteFragment{{Note: "n2", Material: "m2", Locator: Locator{Kind: "page", N: 1}, Text: "ротор вектора"}}}}
	got := Passages("ротор", []LessonText{a, b, a})
	if len(got) != 2 || got[0].Note != "n1" || got[1].Note != "n2" {
		t.Fatalf("passages %+v", got)
	}
}
