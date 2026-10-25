package homeworkdoc

import (
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
)

// Fixed wording of a lab report (TASK-107). The app writes it, never the model.
const (
	// TodoHeading heads the final block: every gap the student fills, by section.
	TodoHeading = "Что доделать самому"
	// ConclusionsNote stands under the conclusions: the model wrote them without the student's data.
	ConclusionsNote = "Выводы зависят от ваших данных: проверьте и поправьте их, когда вставите свои результаты."
	// ManualNotRead and ManualNoText tell the student that the attached lab manual was not read.
	ManualNotRead = "Методичка в PDF пока не читается: отчёт составлен по тексту задания. Сверьте структуру и ход работы с методичкой."
	ManualNoText  = "В файле методички нет текста, который удалось прочитать: отчёт составлен по тексту задания. Сверьте структуру и ход работы с методичкой."
	todoCheck     = "проверьте выводы по своим данным"
)

// LabParagraph is a paragraph of a lab report section: text, or a placeholder whose Text is its prompt («Вставьте …»).
type LabParagraph struct {
	Text        string
	Placeholder bool
}

// LabSection is a written section of a lab report; Conclusions marks the conclusions, which depend on the student's data.
type LabSection struct {
	Title       string
	Paragraphs  []LabParagraph
	Conclusions bool
}

// LabInput is everything a lab report is built from: the title page fields, kind, topic and sources of Paper (its
// Sections are not read), the sections, and Manual, the note about an attached manual that was not read ("" none).
type LabInput struct {
	Paper    PaperInput
	Sections []LabSection
	Manual   string
}

// BuildLabReport returns a lab report as a finished paper (BuildPaper): after GeneratedMark the note about the manual,
// then the sections with each placeholder as a highlighted values.BlockPlaceholder and ConclusionsNote under the
// conclusions, the bibliography, and last TodoHeading listing every placeholder by section, the conclusions and the
// manual. The texts may be model output: the document is checked with values.Document.Validate before rendering.
func BuildLabReport(in LabInput) values.Document {
	doc := paperStart(in.Paper)
	manual := values.CleanName(in.Manual)
	if manual != "" {
		doc.Blocks = append(doc.Blocks, placeholder(manual))
	}
	var todo []string
	for _, s := range in.Sections {
		title := values.CleanName(s.Title)
		if title != "" {
			doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{title}})
		}
		for _, p := range s.Paragraphs {
			line := strings.TrimSpace(printable(p.Text))
			switch {
			case line == "":
			case p.Placeholder:
				doc.Blocks = append(doc.Blocks, placeholder(line))
				todo = append(todo, todoLine(title, line))
			default:
				doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockParagraph, Lines: []string{line}})
			}
		}
		if s.Conclusions {
			doc.Blocks = append(doc.Blocks, placeholder(ConclusionsNote))
			todo = append(todo, todoLine(title, todoCheck))
		}
	}
	doc.Blocks = append(doc.Blocks, bibliography(in.Paper.Sources)...)
	if manual != "" {
		todo = append(todo, manual)
	}
	if len(todo) > 0 {
		doc.Blocks = append(doc.Blocks,
			values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{TodoHeading}},
			values.Block{Kind: values.BlockNumberedList, Lines: todo})
	}
	return doc
}

func placeholder(prompt string) values.Block {
	return values.Block{Kind: values.BlockPlaceholder, Lines: []string{prompt}}
}

// todoLine is an item of «Что доделать самому»: the section, then what to do.
func todoLine(section, what string) string {
	if section == "" {
		return what
	}
	return "«" + section + "»: " + what
}
