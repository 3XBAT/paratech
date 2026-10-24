package homeworkdoc

import (
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

func TestBuildPaper(t *testing.T) {
	doc := BuildPaper(PaperInput{
		Student: Student{FullName: "Иванов Иван", Group: "ИВТ-21", University: "Университет", City: "Москва"},
		Subject: "История", Teacher: "Петров П. П.", Kind: "реферат", Topic: "Древняя Русь\nи соседи", Year: 2026,
		Sections: []PaperSection{
			{Title: "Введение", Paragraphs: []string{"Первый.", "  ", "Второй\x07."}},
			{Title: "Заключение", Paragraphs: []string{"Итог."}},
		},
		Sources: []PaperSource{{Text: "Методичка.docx"}, {Text: "Иванов И. История", Verify: true}, {Text: "Методичка.docx"}},
	})
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if !doc.TableOfContents || !doc.SectionsOnNewPage || doc.Format != (values.Formatting{}) {
		t.Fatalf("layout = %+v", doc)
	}
	if doc.Title.Heading != "Реферат по дисциплине «История»" || doc.Title.Topic != "на тему: «Древняя Русь и соседи»" ||
		doc.Title.Checker != "Проверил: Петров П. П." || doc.Title.Place != "Москва 2026" {
		t.Fatalf("title = %+v", doc.Title)
	}
	if b := doc.Blocks[0]; b.Kind != values.BlockParagraph || b.Lines[0] != GeneratedMark {
		t.Fatalf("first block = %+v", b)
	}
	var headings, paragraphs []string
	for _, b := range doc.Blocks {
		switch b.Kind {
		case values.BlockHeading:
			headings = append(headings, b.Lines[0])
		case values.BlockParagraph:
			paragraphs = append(paragraphs, b.Lines[0])
		}
	}
	if !slices.Equal(headings, []string{"Введение", "Заключение"}) ||
		!slices.Equal(paragraphs, []string{GeneratedMark, "Первый.", "Второй.", "Итог."}) {
		t.Fatalf("headings %v, paragraphs %v", headings, paragraphs)
	}
	last := doc.Blocks[len(doc.Blocks)-1]
	if last.Kind != values.BlockBibliography ||
		!slices.Equal(last.Lines, []string{"Методичка.docx", "Иванов И. История" + VerifySource}) {
		t.Fatalf("bibliography = %+v", last)
	}

	plain := BuildPaper(PaperInput{Sections: []PaperSection{{Title: "Раздел", Paragraphs: []string{"Текст."}}}})
	if plain.Title.Heading != "Работа" || plain.Blocks[len(plain.Blocks)-1].Kind == values.BlockBibliography {
		t.Fatalf("plain paper = %+v", plain)
	}
	if got := PaperFileName("реферат", "Русь: <история>"); !strings.HasPrefix(got, "Реферат Русь") || !strings.HasSuffix(got, DocxExtension) {
		t.Fatalf("file name = %q", got)
	}
}
