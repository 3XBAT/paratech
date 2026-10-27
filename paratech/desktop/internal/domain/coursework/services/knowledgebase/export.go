package knowledgebase

import (
	"strconv"
	"strings"

	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/shared"
)

// Headings and texts of the student's notes in an exported subject (Russian UI). The source line is paratech's own
// markup; everything of the student is escaped.
const (
	ownNotesHeading  = "Мой конспект"
	ownNotesSource   = "_Источник: ваш конспект, текст без изменений._"
	ownNoteEmpty     = "В этой части конспекта нет текста."
	undatedText      = "Без даты"
	noteDateLayout   = "02.01.2006"
	exportSeparator  = " · "
	locatorPageText  = "стр."
	locatorSlideText = "слайд"
)

// ExportNote is one note of an exported lecture: its title and the fragments of its parts that have text.
type ExportNote struct {
	Title     string
	Fragments []NoteFragment
}

// ExportLecture is one lecture of an exported subject. Lesson is what summaryexport renders of a recorded lesson; of
// a note-only lecture only its title, kind and start (local midnight, zero when undated) are used.
type ExportLecture struct {
	Lesson   summaryexport.SubjectLesson
	Recorded bool
	Notes    []ExportNote
}

// RenderSubject renders a whole subject as one Markdown file: as summaryexport.RenderSubject renders it, with the
// note-only lectures among the recorded ones in the order given and, per lecture with notes, a block «Мой конспект»
// marked as the student's text after the summary and the lecture text. A note-only lecture has no summary nor lecture
// text block. Every text of the student is escaped line by line (shared.EscapeMarkdown), so it forms no heading, link
// or other markup. A subject without notes renders exactly as summaryexport.RenderSubject.
func RenderSubject(subject string, lectures []ExportLecture) string {
	var b strings.Builder
	b.WriteString(summaryexport.RenderSubject(summaryexport.SubjectDocument{Subject: subject}))

	// A recorded lesson is rendered alone and its subject heading cut: the subject heading of an empty name.
	head := summaryexport.RenderSubject(summaryexport.SubjectDocument{})
	for _, l := range lectures {
		if l.Recorded {
			one := summaryexport.RenderSubject(summaryexport.SubjectDocument{Lessons: []summaryexport.SubjectLesson{l.Lesson}})
			b.WriteString(strings.TrimPrefix(one, head))
		} else {
			b.WriteString("\n## " + shared.EscapeMarkdown(noteLectureHeading(l.Lesson)) + "\n")
		}
		if len(l.Notes) > 0 {
			writeNotes(&b, l.Notes)
		}
	}
	return b.String()
}

// noteLectureHeading is «12.09.2026 · лекция · Название», «Без даты · Название» when undated.
func noteLectureHeading(l summaryexport.SubjectLesson) string {
	parts := []string{undatedText}
	if !l.Start.IsZero() {
		parts[0] = l.Start.Format(noteDateLayout)
	}
	if kind := kindText(l.Kind); kind != "" {
		parts = append(parts, kind)
	}
	if title := oneLine(l.Title); title != "" {
		parts = append(parts, title)
	}
	return strings.Join(parts, exportSeparator)
}

// writeNotes writes the block «Мой конспект»: the source line, then each note under its title and its fragments
// separated by blank lines; a page or slide is named on a line of its own.
func writeNotes(b *strings.Builder, notes []ExportNote) {
	b.WriteString("\n### " + ownNotesHeading + "\n\n" + ownNotesSource + "\n")
	for _, n := range notes {
		b.WriteString("\n#### " + shared.EscapeMarkdown(oneLine(n.Title)) + "\n\n")
		if len(n.Fragments) == 0 {
			b.WriteString(ownNoteEmpty + "\n")
		}
		for i, f := range n.Fragments {
			if i > 0 {
				b.WriteString("\n")
			}
			if place := locatorText(f.Locator); place != "" {
				b.WriteString(shared.EscapeMarkdown(place) + "\n\n")
			}
			writeLines(b, f.Text)
		}
	}
}

// locatorText names a page or a slide («стр. 3»); sections and fragments are not named (a section starts with its
// heading line).
func locatorText(l Locator) string {
	switch l.Kind {
	case "page":
		return locatorPageText + " " + strconv.Itoa(l.N)
	case "slide":
		return locatorSlideText + " " + strconv.Itoa(l.N)
	}
	return ""
}

// writeLines writes untrusted text line by line, escaped, keeping its paragraphs. The indentation is dropped: four
// spaces would make a code block.
func writeLines(b *strings.Builder, text string) {
	for _, line := range strings.Split(cleanText(text), "\n") {
		line = strings.TrimLeft(strings.TrimRight(line, "\r \t"), " \t")
		b.WriteString(shared.EscapeMarkdown(line) + "\n")
	}
}

// kindText names a lesson kind code as an export does; "" for an unknown one.
func kindText(kind string) string {
	switch kind {
	case "lecture":
		return "лекция"
	case "seminar":
		return "семинар"
	case "practice":
		return "практика"
	case "lab":
		return "лабораторная"
	}
	return ""
}

func oneLine(s string) string { return strings.Join(strings.Fields(cleanText(s)), " ") }
