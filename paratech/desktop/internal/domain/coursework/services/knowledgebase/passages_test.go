package knowledgebase

import (
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func TestTerms(t *testing.T) {
	tests := []struct {
		q    string
		want []string
	}{
		{"Что такое производная?", []string{"производн"}},
		{"Что такое ряд Тейлора и где его применяют?", []string{"ряд", "тейло", "применя"}},
		{"ЁМКОСТЬ конденсатора", []string{"емкос", "конденсато"}},
		{"Что это такое?", nil},
		{"а и я", nil},
		{"Ряды, ряды и ещё раз ряды", []string{"ряды", "раз"}},
	}
	for _, tt := range tests {
		if got := Terms(tt.q); !slices.Equal(got, tt.want) {
			t.Errorf("Terms(%q) = %q, want %q", tt.q, got, tt.want)
		}
	}
	long := strings.Repeat("слово ", 3) + "альфа бета гамма дельта эпсилон дзета тета йота каппа лямбда мюон нюон кси"
	if n := len(Terms(long)); n != MaxTerms {
		t.Errorf("%d terms, want %d", n, MaxTerms)
	}
}

func lessonText(rec string, day int, d Document) LessonText {
	d.Recording = shared.RecordingID(rec)
	return LessonText{Lesson: Lesson{Recording: shared.RecordingID(rec), StartedAt: time.Date(2026, 10, day, 9, 0, 0, 0, time.UTC)}, Document: d}
}

func TestPassages(t *testing.T) {
	older := lessonText("r1", 10, Document{
		Summary: []string{"Производная и её смысл", "Интегралы"},
		Blocks: []Block{
			{Heading: "Определение", StartMs: 60_000, Paragraphs: []string{"Производная функции — предел отношения."}},
			{Heading: "Пример", StartMs: 120_000, Paragraphs: []string{"Производные элементарных функций."}},
		},
		// Ignored: the lecture text is the full text of this lesson.
		Segments: []Segment{{StartMs: 1, Text: "производная в транскрипте"}},
	})
	newer := lessonText("r2", 20, Document{
		Segments: []Segment{
			{StartMs: 0, Text: "Сегодня ряды."}, {StartMs: 1000, Text: "Производной"}, {StartMs: 2000, Text: "здесь"},
			{StartMs: 3000, Text: "нет"}, {StartMs: 4000, Text: "совсем"}, {StartMs: 5000, Text: "ничего"},
			{StartMs: 6000, Text: "А производная функции\u202e снова"},
		},
	})
	got := Passages("Что такое производная функции?", []LessonText{older, newer})

	type place struct {
		rec   string
		sec   Section
		index int
		start int64
	}
	var places []place
	for _, p := range got {
		places = append(places, place{string(p.Recording()), p.Section, p.Index, p.StartMs})
	}
	want := []place{
		// Two terms: the newer lesson first, then the older one in its order.
		{"r2", SectionTranscript, 6, 6000},
		{"r1", SectionLectureText, 0, 60_000},
		{"r1", SectionLectureText, 1, 120_000},
		// One term.
		{"r2", SectionTranscript, 0, 0},
		{"r1", SectionSummary, 0, 0},
	}
	if !slices.Equal(places, want) {
		t.Fatalf("places = %v, want %v", places, want)
	}
	if got[0].Text != "А производная функции снова" || got[1].Text != "Определение\nПроизводная функции — предел отношения." ||
		got[3].Text != "Сегодня ряды. Производной здесь нет совсем ничего" {
		t.Fatalf("texts %q / %q / %q", got[0].Text, got[1].Text, got[3].Text)
	}

	if p := Passages("Что это такое?", []LessonText{older}); p != nil {
		t.Fatalf("no terms: %v", p)
	}
	if p := Passages("Что такое ротор?", []LessonText{older, newer}); len(p) != 0 {
		t.Fatalf("no match: %v", p)
	}
}

func TestPassagesBounds(t *testing.T) {
	var blocks []Block
	for i := range MaxPassages + 5 {
		blocks = append(blocks, Block{StartMs: int64(i), Paragraphs: []string{"ряд номер"}})
	}
	if n := len(Passages("ряд", []LessonText{lessonText("r1", 1, Document{Blocks: blocks})})); n != MaxPassages {
		t.Fatalf("%d passages, want %d", n, MaxPassages)
	}

	// A long block is cut around its first match and fits.
	long := strings.Repeat("вода ", 2000) + "Ряд Тейлора приближает функцию." + strings.Repeat(" вода", 2000)
	got := Passages("ряд Тейлора", []LessonText{lessonText("r1", 1, Document{Blocks: []Block{{Paragraphs: []string{long}}}})})
	if len(got) != 1 || len(got[0].Text) > MaxPassageBytes || !strings.Contains(got[0].Text, "Ряд Тейлора приближает функцию.") {
		t.Fatalf("cut passage: %d bytes, %q…", len(got[0].Text), got[0].Text[:40])
	}
}
