package homeworkdoc

import (
	"slices"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

func labInput(manual string) LabInput {
	return LabInput{
		Paper: PaperInput{
			Student: Student{FullName: "Иванов Иван", Group: "ИВТ-21"}, Subject: "Физика", Kind: "отчёт по лабораторной",
			Topic: "Маятник", Year: 2026, Sources: []PaperSource{{Text: "Методичка.docx"}},
			Sections: []PaperSection{{Title: "не читается", Paragraphs: []string{"не читается"}}},
		},
		Sections: []LabSection{
			{Title: "Цель работы", Paragraphs: []LabParagraph{{Text: "Изучить маятник."}}},
			{Title: "Результаты", Paragraphs: []LabParagraph{
				{Text: "Данные сведены в таблицу."}, {Text: "Вставьте таблицу измерений", Placeholder: true},
				{Text: "Вставьте график T(l)", Placeholder: true},
			}},
			{Title: "Выводы", Paragraphs: []LabParagraph{{Text: "Если период совпадёт с расчётным, формула верна."}}, Conclusions: true},
		},
		Manual: manual,
	}
}

func TestBuildLabReport(t *testing.T) {
	doc := BuildLabReport(labInput(""))
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.Title.Heading != "Отчёт по лабораторной по дисциплине «Физика»" || !doc.TableOfContents || doc.Blocks[0].Lines[0] != GeneratedMark {
		t.Fatalf("start = %+v / %+v", doc.Title, doc.Blocks[0])
	}
	var kinds []values.BlockKind
	var texts []string
	for _, b := range doc.Blocks {
		kinds = append(kinds, b.Kind)
		texts = append(texts, b.Lines...)
	}
	wantKinds := []values.BlockKind{
		values.BlockParagraph,
		values.BlockHeading, values.BlockParagraph,
		values.BlockHeading, values.BlockParagraph, values.BlockPlaceholder, values.BlockPlaceholder,
		values.BlockHeading, values.BlockParagraph, values.BlockPlaceholder,
		values.BlockBibliography, values.BlockHeading, values.BlockNumberedList,
	}
	if !slices.Equal(kinds, wantKinds) {
		t.Fatalf("kinds = %v", kinds)
	}
	if doc.Blocks[5].Lines[0] != "Вставьте таблицу измерений" || doc.Blocks[9].Lines[0] != ConclusionsNote {
		t.Fatalf("placeholders = %+v %+v", doc.Blocks[5], doc.Blocks[9])
	}
	if slices.Contains(texts, "не читается") {
		t.Fatal("the paper sections were read")
	}
	todo := doc.Blocks[len(doc.Blocks)-1]
	if doc.Blocks[len(doc.Blocks)-2].Lines[0] != TodoHeading || !slices.Equal(todo.Lines, []string{
		"«Результаты»: Вставьте таблицу измерений", "«Результаты»: Вставьте график T(l)", "«Выводы»: проверьте выводы по своим данным",
	}) {
		t.Fatalf("todo = %+v", todo)
	}

	// A manual that was not read: a visible note at the start and the last item to do.
	doc = BuildLabReport(labInput(ManualNotRead))
	if doc.Blocks[1].Kind != values.BlockPlaceholder || doc.Blocks[1].Lines[0] != ManualNotRead ||
		doc.Blocks[len(doc.Blocks)-1].Lines[3] != ManualNotRead {
		t.Fatalf("manual note = %+v", doc.Blocks[1])
	}

	// Nothing to do: no block.
	in := labInput("")
	in.Sections = in.Sections[:1]
	doc = BuildLabReport(in)
	if last := doc.Blocks[len(doc.Blocks)-1]; last.Kind != values.BlockBibliography {
		t.Fatalf("last = %+v", last)
	}
}
