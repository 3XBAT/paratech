package summaryexport

import (
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/summary"
)

func sectioned(t *testing.T, sections []summary.Section, points ...string) summary.Content {
	t.Helper()
	c, err := summary.NewSectionedContent(sections, points)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TASK-101: the copy and the saved file carry the headings of the sections.
func TestRenderSections(t *testing.T) {
	doc := Document{
		Title: "География",
		Content: sectioned(t, []summary.Section{
			{Heading: "Реки", Text: "Реки текут в море.\n\nУ устья бывает дельта.", Points: []string{"Исток", "Устье"}},
			{Heading: "Озёра", Text: "Озёра бывают пресные и солёные."},
		}, "Дельта — устье реки"),
	}
	tests := []struct {
		f    Format
		want string
	}{
		{FormatText, "География\n\n" +
			"Реки:\nРеки текут в море.\n\nУ устья бывает дельта.\n- Исток\n- Устье\n\nОзёра:\nОзёра бывают пресные и солёные.\n" +
			"\nКлючевые моменты:\n- Дельта — устье реки\n"},
		{FormatMarkdown, "# География\n\n" +
			"## Реки\n\nРеки текут в море.\n\nУ устья бывает дельта.\n\n- Исток\n- Устье\n\n## Озёра\n\nОзёра бывают пресные и солёные.\n" +
			"\n## Ключевые моменты\n\n- Дельта — устье реки\n"},
	}
	for _, tc := range tests {
		t.Run(string(tc.f), func(t *testing.T) {
			got, err := Render(doc, tc.f)
			if err != nil || got != tc.want {
				t.Fatalf("Render =\n%q, %v\nwant\n%q", got, err, tc.want)
			}
		})
	}
}

// Headings, texts and points of the sections are model output: in Markdown they form no markup.
func TestRenderSectionsEscapesModelText(t *testing.T) {
	doc := Document{Content: sectioned(t, []summary.Section{{
		Heading: "<b>Реки</b> [x](https://evil.example)",
		Text:    "# не заголовок\n    код\n<script>alert(1)</script>",
		Points:  []string{"**жирный**", "- вложенный"},
	}})}
	got, err := Render(doc, FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	want := "## \\<b\\>Реки\\</b\\> \\[x\\](https:\\//evil.example)\n\n" +
		"\\# не заголовок\nкод\n\\<script\\>alert(1)\\</script\\>\n\n- \\*\\*жирный\\*\\*\n- \\- вложенный\n"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
	for i, r := range got {
		if strings.ContainsRune("<>[]*", r) && (i == 0 || got[i-1] != '\\') {
			t.Fatalf("unescaped %q at %d", r, i)
		}
	}
}

func TestRenderSubjectSections(t *testing.T) {
	doc := SubjectDocument{
		Subject: "География",
		Lessons: []SubjectLesson{{
			Title: "Реки", Start: time.Date(2026, 10, 14, 10, 1, 0, 0, msk),
			Summary: sectioned(t, []summary.Section{
				{Heading: "Реки", Text: "Реки текут в море.", Points: []string{"Устье"}},
				{Heading: "Озёра", Text: "Озёра стоят."},
			}, "Вода"),
		}},
	}
	want := "### Кратко\n\n#### Реки\n\nРеки текут в море.\n\n- Устье\n\n#### Озёра\n\nОзёра стоят.\n" +
		"\n#### Ключевые моменты\n\n- Вода\n"
	if got := RenderSubject(doc); !strings.Contains(got, want) {
		t.Fatalf("got\n%s\nwant it to contain\n%s", got, want)
	}
}
