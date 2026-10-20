package knowledgebase

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSummaryTexts(t *testing.T) {
	cases := []struct {
		name      string
		sections  []SummarySection
		keyPoints []string
		want      []string
	}{
		{"none", nil, nil, []string{}},
		{"plain summary", []SummarySection{{Text: "Про реки."}}, []string{"Реки"}, []string{"Про реки.", "Реки"}},
		{"sections", []SummarySection{
			{Heading: "Реки", Text: "Реки текут.", Points: []string{"Устье", "Дельта"}},
			{Heading: "Озёра", Text: "Озёра стоят."},
		}, []string{"Вода"}, []string{"Реки\nРеки текут.\nУстье\nДельта", "Озёра\nОзёра стоят.", "Вода"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SummaryTexts(tc.sections, tc.keyPoints); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SummaryTexts = %q, want %q", got, tc.want)
			}
		})
	}
}

// TASK-101: a heading, a text or a point of a section is found once per section, at the index of the section; key
// points follow the sections.
func TestSearchSummarySections(t *testing.T) {
	d := Document{Recording: "r1", Summary: SummaryTexts([]SummarySection{
		{Heading: "Механика", Text: "Механика изучает движение.", Points: []string{"механика тел"}},
		{Heading: "Оптика", Text: "Свет.", Points: []string{"Законы механики не нужны"}},
		{Heading: "Механика жидкостей", Text: "Вода."},
	}, []string{"Главное: механика"})}
	s := mustSearch(t, "механик")
	s.Scan(d)

	var got []string
	for _, h := range s.Hits() {
		got = append(got, fmt.Sprintf("%s:%d:%s", h.Section, h.Index, h.Snippet.Match))
	}
	want := []string{"summary:0:Механик", "summary:1:механик", "summary:2:Механик"}
	if !reflect.DeepEqual(got, want) || !s.Truncated() {
		t.Fatalf("hits = %q (truncated %v), want %q and truncated by MaxSectionHits", got, s.Truncated(), want)
	}
}
