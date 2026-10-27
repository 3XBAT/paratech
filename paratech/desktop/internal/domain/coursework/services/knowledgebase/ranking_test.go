package knowledgebase

import (
	"fmt"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// aiLesson is a lesson whose every AI section matches "тейлор" up to MaxSectionHits: 1 title, 3 summary, 3 lecture
// text and 3 transcript hits.
func aiLesson(key string, notes ...NoteFragment) Document {
	d := Document{Key: key, Recording: shared.RecordingID("r-" + key), Title: "Тейлор", Notes: notes}
	for i := range MaxSectionHits + 1 {
		d.Summary = append(d.Summary, fmt.Sprintf("ряд тейлора %d", i))
		d.Blocks = append(d.Blocks, Block{Heading: fmt.Sprintf("Тейлор %d", i), StartMs: int64(i) * 1000})
		d.Segments = append(d.Segments, Segment{StartMs: int64(i) * 1000, Text: "тейлор"})
	}
	return d
}

func TestSearchRanksNotesFirst(t *testing.T) {
	note := NoteFragment{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 1}, Text: "мой тейлор"}
	tests := []struct {
		name string
		docs []Document
		want []string // key and section of the hits, in order
	}{
		{
			name: "equal match in a lesson: the note before the summary",
			docs: []Document{{Key: "a", Summary: []string{"тейлор"}, Notes: []NoteFragment{note}}},
			want: []string{"a ownNote", "a summary"},
		},
		{
			name: "across lessons the scan order stays: a note of a later lesson after the summary of an earlier one",
			docs: []Document{{Key: "a", Summary: []string{"тейлор"}}, {Key: "b", Summary: []string{"тейлор"}, Notes: []NoteFragment{note}}},
			want: []string{"a summary", "b ownNote", "b summary"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustSearch(t, "тейлор")
			for _, d := range tt.docs {
				s.Scan(d)
			}
			var got []string
			for _, h := range s.Hits() {
				got = append(got, h.Key+" "+string(h.Section))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
		})
	}
}

// The hit cap cuts the AI sections at the tail of a lesson, never the note scanned first in it.
func TestSearchCapKeepsNotes(t *testing.T) {
	note := NoteFragment{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 2}, Text: "тейлор от руки"}
	s := mustSearch(t, "тейлор")
	for i := range 4 {
		s.Scan(aiLesson(fmt.Sprintf("l%d", i)))
	}
	s.Scan(aiLesson("l4", note)) // 40 hits before it: 1 note and 10 AI hits do not fit
	s.Scan(aiLesson("l5"))

	hits := s.Hits()
	if len(hits) != MaxHits || !s.Truncated() {
		t.Fatalf("%d hits, truncated %v; want %d, true", len(hits), s.Truncated(), MaxHits)
	}
	h := hits[40]
	if h.Key != "l4" || h.Section != SectionOwnNote || h.Note != "n1" || h.Locator != note.Locator {
		t.Fatalf("hit 40 = %+v, want the note of l4", h)
	}
	if last := hits[MaxHits-1]; last.Key != "l4" || last.Section != SectionTranscript || last.Index != 1 {
		t.Fatalf("last hit = %+v, want the second transcript hit of l4 (its third is cut)", last)
	}
}

func TestPassagesRanksNotesFirst(t *testing.T) {
	note := NoteFragment{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 1}, Text: "мой ряд Тейлора"}

	// More equal AI passages in the lesson than MaxPassages: the note is first, the cap cuts the AI tail.
	many := make([]string, MaxPassages+3)
	for i := range many {
		many[i] = fmt.Sprintf("ряд Тейлора %d", i)
	}
	full := lessonText("r1", 20, Document{Summary: many, Notes: []NoteFragment{note}})
	got := Passages("ряд Тейлора", []LessonText{full})
	if len(got) != MaxPassages || got[0].Section != SectionOwnNote || got[0].Note != "n1" ||
		got[1].Section != SectionSummary || got[1].Index != 0 || got[MaxPassages-1].Index != MaxPassages-2 {
		t.Fatalf("passages %+v", got)
	}

	// Across lessons: the newer lesson first at an equal score; a better match still wins over a note.
	newer := lessonText("r2", 21, Document{Summary: []string{"ряд Тейлора"}})
	older := lessonText("r3", 10, Document{Summary: []string{"ряд Тейлора"}, Notes: []NoteFragment{note}})
	better := lessonText("r4", 5, Document{Summary: []string{"ряд Тейлора и Маклорена"}, Notes: []NoteFragment{{Text: "Тейлор"}}})
	got = Passages("ряд Тейлора Маклорена", []LessonText{older, better, newer})
	var places []string
	for _, p := range got {
		places = append(places, fmt.Sprintf("%s %s", p.Recording(), p.Section))
	}
	want := []string{"r4 summary", "r2 summary", "r3 ownNote", "r3 summary", "r4 ownNote"}
	if !slices.Equal(places, want) {
		t.Fatalf("places = %v, want %v", places, want)
	}
}
