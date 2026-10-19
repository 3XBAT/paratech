package homeworkdoc

import (
	"strconv"
	"strings"
	"unicode"

	"paratech/desktop/internal/domain/coursework/values"
)

// Student is what the title page names of the student: the fields of the student profile, plain text.
type Student struct {
	FullName   string
	Group      string
	University string
	Faculty    string // faculty or department
	City       string
}

// Input is everything a homework document is built from. Subject and Teacher are those the assignment shows
// (values.LessonLabel); Year is the current year in the student's zone.
type Input struct {
	Student         Student
	Subject         string
	Teacher         string
	AssignmentTitle string
	DraftTitle      string
	DraftBody       string
	Year            int
}

// Fixed wording of the title page (Russian UI).
const (
	headingPlain     = "Домашнее задание"
	headingBySubject = "Домашнее задание по дисциплине «%s»"
	performerLabel   = "Выполнил: "
	performerGroup   = "студент группы "
	checkerLabel     = "Проверил: "
)

// Build returns the document: the title page, a heading with the draft title and the blocks of the draft body. Every
// one-line text is cleaned (values.CleanName), so a field never breaks a line; a field that is empty after that is
// left out with its label. A body line that is not valid text keeps its printable characters only.
func Build(in Input) values.Document {
	st := in.Student
	t := values.TitlePage{
		University: values.CleanName(st.University),
		Faculty:    values.CleanName(st.Faculty),
		Heading:    headingPlain,
		Topic:      values.CleanName(in.AssignmentTitle),
		Performer:  performer(values.CleanName(st.Group), values.CleanName(st.FullName)),
		Place:      place(values.CleanName(st.City), in.Year),
	}
	if subject := values.CleanName(in.Subject); subject != "" {
		t.Heading = strings.Replace(headingBySubject, "%s", subject, 1)
	}
	if teacher := values.CleanName(in.Teacher); teacher != "" {
		t.Checker = checkerLabel + teacher
	}

	doc := values.Document{Title: t}
	title := values.CleanName(in.DraftTitle)
	if title != "" {
		doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{title}})
	}
	body := blocks(in.DraftBody)
	// A body that starts with the draft title as its own heading does not repeat it.
	if title != "" && len(body) > 0 && body[0].Kind == values.BlockHeading && body[0].Lines[0] == title {
		body = body[1:]
	}
	doc.Blocks = append(doc.Blocks, body...)
	return doc
}

func performer(group, name string) string {
	switch {
	case group != "" && name != "":
		return performerLabel + performerGroup + group + " " + name
	case group != "":
		return performerLabel + performerGroup + group
	case name != "":
		return performerLabel + name
	}
	return ""
}

func place(city string, year int) string {
	if year <= 0 {
		return city
	}
	if city == "" {
		return strconv.Itoa(year)
	}
	return city + " " + strconv.Itoa(year)
}

// blocks splits a draft body into headings and paragraphs: a line "# text" (up to values.MaxHeadingLevel hashes, a
// space, then text) is a heading, a blank line ends a paragraph, other lines are lines of the current paragraph.
func blocks(body string) []values.Block {
	var out []values.Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, values.Block{Kind: values.BlockParagraph, Lines: para})
			para = nil
		}
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRightFunc(printable(raw), unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if level, text, ok := heading(line); ok {
			flush()
			out = append(out, values.Block{Kind: values.BlockHeading, Level: level, Lines: []string{text}})
			continue
		}
		para = append(para, line)
	}
	flush()
	return out
}

// heading reads a Markdown ATX heading of level 1 to values.MaxHeadingLevel; a deeper one stays text.
func heading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if level < values.MinHeadingLevel || level > values.MaxHeadingLevel {
		return 0, "", false
	}
	rest := trimmed[level:]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return 0, "", false
	}
	text := values.CleanName(strings.TrimRight(strings.TrimSpace(rest), "#"))
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

// printable keeps tabs and the printable characters of a body line: control and bidi characters are dropped.
func printable(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, values.StripBidi(s))
}
