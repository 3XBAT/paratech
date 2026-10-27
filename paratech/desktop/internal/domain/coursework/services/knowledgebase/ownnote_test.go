package knowledgebase

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/summary"
)

// The student's notes are a section of their own scanned first in a lesson (ADR-0027 «Ranking»), with the note,
// material and locator of each hit; the caps of ADR-0019 count it as a section.
func TestSearchOwnNotes(t *testing.T) {
	d := Document{
		Key: "recording:r1", Recording: "r1", Title: "Ряды",
		Segments: []Segment{{StartMs: 1000, Text: " ряды Тейлора"}},
		Notes: []NoteFragment{
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "section", N: 1}, Text: "Ряды\nРяд Тейлора сходится"},
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "section", N: 2}, Text: "Другое"},
			{Note: "n2", Material: "m2", Locator: Locator{Kind: "page", N: 3}, Text: "тейлор на стр. 3"},
		},
	}
	s := mustSearch(t, "тейлор")
	s.Scan(d)
	var got []string
	for _, h := range s.Hits() {
		got = append(got, fmt.Sprintf("%s %s %d %s %s %s/%d", h.Key, h.Section, h.Index, h.Note, h.Material, h.Locator.Kind, h.Locator.N))
	}
	want := []string{
		"recording:r1 ownNote 0 n1 m1 section/1",
		"recording:r1 ownNote 2 n2 m2 page/3",
		"recording:r1 transcript 0   /0",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hits =\n %v\nwant\n %v", got, want)
	}

	// A note-only lecture: no recording; more than MaxSectionHits fragments are cut.
	frags := make([]NoteFragment, 5)
	for i := range frags {
		frags[i] = NoteFragment{Note: "n3", Material: "m3", Locator: Locator{Kind: "fragment", N: i + 1}, Text: "тейлор"}
	}
	s = mustSearch(t, "тейлор")
	s.Scan(Document{Key: "note:n3", Notes: frags})
	hits := s.Hits()
	if len(hits) != MaxSectionHits || !s.Truncated() || hits[0].Recording != "" || hits[0].Key != "note:n3" || hits[2].Locator.N != 3 {
		t.Fatalf("note-only hits = %+v, truncated %v", hits, s.Truncated())
	}
}

func TestRenderSubjectWithNotes(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	c, err := summary.NewContent("Ряды и их сходимость.", nil)
	if err != nil {
		t.Fatal(err)
	}
	recorded := summaryexport.SubjectLesson{
		Title: "Ряды", Kind: "lecture", Start: time.Date(2026, 9, 12, 10, 0, 0, 0, msk), Duration: time.Hour, Summary: c,
	}

	// Without notes the file is exactly summaryexport's.
	plain := summaryexport.RenderSubject(summaryexport.SubjectDocument{Subject: "Матанализ", Lessons: []summaryexport.SubjectLesson{recorded, recorded}})
	if got := RenderSubject("Матанализ", []ExportLecture{{Lesson: recorded, Recorded: true}, {Lesson: recorded, Recorded: true}}); got != plain {
		t.Fatalf("without notes:\n%s\nwant\n%s", got, plain)
	}

	got := RenderSubject("Матанализ", []ExportLecture{
		{Lesson: recorded, Recorded: true, Notes: []ExportNote{{Title: "Мои ряды", Fragments: []NoteFragment{
			{Locator: Locator{Kind: "section", N: 1}, Text: "## Ряды\n    1. [ссылка](https://example.invalid)\n<script>x</script>"},
			{Locator: Locator{Kind: "page", N: 3}, Text: "===\n- пункт\n\u202eтекст"},
		}}}},
		{Lesson: summaryexport.SubjectLesson{Title: "Пара 5 сентября", Kind: "seminar", Start: time.Date(2026, 9, 5, 0, 0, 0, 0, msk)},
			Notes: []ExportNote{{Title: "# Фото", Fragments: nil}}},
		{Lesson: summaryexport.SubjectLesson{Title: "Без даты лекция"}, Notes: []ExportNote{{Title: "Старое", Fragments: []NoteFragment{
			{Locator: Locator{Kind: "fragment", N: 1}, Text: "Просто текст"},
		}}}},
	})
	recordedPart := strings.TrimPrefix(
		summaryexport.RenderSubject(summaryexport.SubjectDocument{Lessons: []summaryexport.SubjectLesson{recorded}}),
		summaryexport.RenderSubject(summaryexport.SubjectDocument{}))
	want := "# Матанализ\n" + recordedPart +
		"\n### Мой конспект\n\n" + ownNotesSource + "\n" +
		"\n#### Мои ряды\n\n" +
		"\\#\\# Ряды\n" +
		"1\\. \\[ссылка\\](https:\\//example.invalid)\n" +
		"\\<script\\>x\\</script\\>\n" +
		"\nстр. 3\n\n" +
		"\\===\n" +
		"\\- пункт\n" +
		"текст\n" +
		"\n## 05\\.09.2026 · семинар · Пара 5 сентября\n" +
		"\n### Мой конспект\n\n" + ownNotesSource + "\n" +
		"\n#### \\# Фото\n\nВ этом конспекте нет текста.\n" +
		"\n## Без даты · Без даты лекция\n" +
		"\n### Мой конспект\n\n" + ownNotesSource + "\n" +
		"\n#### Старое\n\nПросто текст\n"
	if got != want {
		t.Fatalf("RenderSubject =\n%s\nwant\n%s", got, want)
	}
	if n := strings.Count(got, ownNoteEmpty); n != 1 {
		t.Fatalf("empty-note line %d times, want once for the one empty note", n)
	}
}
