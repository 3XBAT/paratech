package knowledgebase

import (
	"context"
	"reflect"
	"strings"
	"testing"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

func (e *env) addSectionedSummary(t *testing.T, rec shared.RecordingID, sections []summary.Section, points ...string) {
	t.Helper()
	r := e.recs.ready[rec]
	c, err := summary.NewSectionedContent(sections, points)
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: r.Transcript, PromptVersion: 5, Content: c}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.summaries.rows[s.ID()] = s
	r.Summary = true
	e.recs.ready[rec] = r
}

// TASK-101: the search finds the headings, texts and points of the sections, one hit per section, and the key points
// after them; the export writes the sections with their headings.
func TestSearchAndExportSections(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Лекция", subject: "Физика", kind: "lecture"})
	e.addTranscript(t, "r1", " раз")
	e.addSectionedSummary(t, "r1", []summary.Section{
		{Heading: "Кванты", Text: "Энергия порциями."},
		{Heading: "Поля", Text: "Поле непрерывно.", Points: []string{"Квантование поля"}},
		{Heading: "Итоги", Text: "Всё."},
	}, "Кванты дискретны")

	res, err := e.svc.Search(ctx, "квант")
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, h := range res.Hits {
		if h.Section == kb.SectionSummary {
			got = append(got, h.Index)
		}
	}
	// Section 0 by its heading, section 1 by its point, key point 0 after the 3 sections.
	if want := []int{0, 1, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summary hits = %v, want %v", got, want)
	}

	if err := e.svc.ExportSubject(ctx, "Физика", "x.md"); err != nil {
		t.Fatal(err)
	}
	want := "### Кратко\n\n#### Кванты\n\nЭнергия порциями.\n\n#### Поля\n\nПоле непрерывно.\n\n- Квантование поля\n\n" +
		"#### Итоги\n\nВсё.\n\n#### Ключевые моменты\n\n- Кванты дискретны\n"
	if !strings.Contains(e.writer.content, want) {
		t.Fatalf("export:\n%s\nwant it to contain\n%s", e.writer.content, want)
	}
}
