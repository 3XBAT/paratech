package homeworkdoc

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// Fixed wording of a finished paper (TASK-131).
const (
	// GeneratedMark opens every finished paper: the student reads it before handing it in and removes the line.
	GeneratedMark = "Сгенерировано — прочитайте перед сдачей."
	// VerifySource follows a source the model named that is not one of the student's materials.
	VerifySource      = " — проверить источник"
	paperBySubject    = "%s по дисциплине «%s»"
	paperTopicLabel   = "на тему: «%s»"
	defaultPaperTitle = "Работа"
)

// PaperSection is a written section of a paper.
type PaperSection struct {
	Title      string
	Paragraphs []string
}

// PaperSource is an entry of the bibliography; Verify marks one the model named («проверить источник»).
type PaperSource struct {
	Text   string
	Verify bool
}

// PaperInput is everything a finished paper is built from: the title page fields as in Input, the kind of the work
// («реферат»), the topic, the sections and the sources. Every text but the profile may be model output: the document
// is checked with values.Document.Validate before rendering.
type PaperInput struct {
	Student  Student
	Subject  string
	Teacher  string
	Kind     string
	Topic    string
	Year     int
	Sections []PaperSection
	Sources  []PaperSource
}

// BuildPaper returns a finished paper per GOST 7.32 (the zero values.Formatting): the title page, the table of
// contents, the mark GeneratedMark, every section under a level-1 heading on a new page, and the bibliography with
// each source once. One-line texts are cleaned (values.CleanName); a paragraph keeps its printable characters.
func BuildPaper(in PaperInput) values.Document {
	doc := paperStart(in)
	for _, s := range in.Sections {
		if title := values.CleanName(s.Title); title != "" {
			doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{title}})
		}
		for _, p := range s.Paragraphs {
			if line := strings.TrimSpace(printable(p)); line != "" {
				doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockParagraph, Lines: []string{line}})
			}
		}
	}
	doc.Blocks = append(doc.Blocks, bibliography(in.Sources)...)
	return doc
}

// paperStart is a finished paper up to its sections: the title page, the table of contents and GeneratedMark.
func paperStart(in PaperInput) values.Document {
	st := in.Student
	kind := capitalized(values.CleanName(in.Kind))
	if kind == "" {
		kind = defaultPaperTitle
	}
	t := values.TitlePage{
		University: values.CleanName(st.University),
		Faculty:    values.CleanName(st.Faculty),
		Heading:    kind,
		Performer:  performer(values.CleanName(st.Group), values.CleanName(st.FullName)),
		Place:      place(values.CleanName(st.City), in.Year),
	}
	if subject := values.CleanName(in.Subject); subject != "" {
		t.Heading = strings.Replace(strings.Replace(paperBySubject, "%s", kind, 1), "%s", subject, 1)
	}
	if topic := values.CleanName(in.Topic); topic != "" {
		t.Topic = strings.Replace(paperTopicLabel, "%s", topic, 1)
	}
	if teacher := values.CleanName(in.Teacher); teacher != "" {
		t.Checker = checkerLabel + teacher
	}

	doc := values.Document{Title: t, TableOfContents: true, SectionsOnNewPage: true}
	doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockParagraph, Lines: []string{GeneratedMark}})
	return doc
}

// bibliography is the block of the sources, each once; none without a source.
func bibliography(sources []PaperSource) []values.Block {
	var entries []string
	seen := map[string]bool{}
	for _, s := range sources {
		text := values.CleanName(s.Text)
		if text == "" {
			continue
		}
		if s.Verify {
			text += VerifySource
		}
		if !seen[text] {
			seen[text] = true
			entries = append(entries, text)
		}
	}
	if len(entries) == 0 {
		return nil
	}
	return []values.Block{{Kind: values.BlockBibliography, Lines: entries}}
}

// PaperFileName is the name of the file of a paper: its kind and topic, safe for Windows, with DocxExtension.
func PaperFileName(kind, topic string) string {
	name := strings.TrimSpace(capitalized(values.CleanName(kind)) + " " + values.CleanName(topic))
	if name == "" {
		name = defaultPaperTitle
	}
	return FileName(name)
}

func capitalized(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
