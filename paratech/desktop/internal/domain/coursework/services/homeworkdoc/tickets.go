package homeworkdoc

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/coursework/values"
)

// Fixed wording of a tickets file (TASK-133).
const (
	ticketsHeading   = "Ответы на билеты"
	ticketsBySubject = "Ответы на билеты по дисциплине «%s»"
	ticketPrefix     = "Билет "

	// NotInLectures replaces the answer of a ticket the student's lessons do not cover.
	NotInLectures = "Не найдено в ваших лекциях."
	// TicketNotAnswered replaces the answer of a ticket whose answer was refused.
	TicketNotAnswered = "Ответ не прошёл проверку — подготовьте этот билет сами."
	// UnsupportedMark follows a claim no citation of the lessons supports: it is never shown as «из ваших лекций».
	UnsupportedMark = " [не подтверждено вашими лекциями — проверьте]"
	// MissingLabel opens a part of the ticket the lessons do not answer.
	MissingLabel = "Нет в ваших лекциях: "
	// CoverageHeading is the heading of the closing coverage summary.
	CoverageHeading = "Покрытие лекциями"

	sourceOpen  = "[источник: лекция "
	sourceClose = "]"
	dateLayout  = "02.01.2006"

	coverageTotal      = "Всего билетов: "
	coverageCovered    = "Из ваших лекций: "
	coveragePartial    = "Частично: "
	coverageNotCovered = "Нет в ваших лекциях: "
	coverageFailed     = "Ответ не получен: "

	// maxLessonRunes bounds the lesson title in a source label, so a long title cannot blow up the file.
	maxLessonRunes = 150
	ticketsFile    = "Билеты"
)

// sectionName is the part of a lesson as a source label names it; "" for none.
func sectionName(s answer.Section) string {
	switch s {
	case answer.SectionSummary:
		return "конспект"
	case answer.SectionLectureText:
		return "текст лекции"
	case answer.SectionTranscript:
		return "расшифровка"
	}
	return ""
}

// TicketSource is the place in a lesson a citation opens: the lesson title as the answer kept it ("" for an ad-hoc
// recording), its start in the student's zone and the part. No recording ID is shown.
type TicketSource struct {
	Lesson  string
	Date    time.Time
	Section answer.Section
}

// TicketClaim is a claim of an answer with the places its citations open; none: no citation supports it.
type TicketClaim struct {
	Text    string
	Sources []TicketSource
}

// TicketEntry is one ticket of the file: its number and title, its coverage, the claims and the missing parts of
// its answer.
type TicketEntry struct {
	Number   int
	Title    string
	Coverage ticketset.Coverage
	Claims   []TicketClaim
	Missing  []string
}

// TicketsInput is everything a tickets file is built from: the title page fields as in Input and the tickets. Every
// text but the profile is untrusted (the list, the model): the document is checked with values.Document.Validate
// before rendering.
type TicketsInput struct {
	Student Student
	Subject string
	Year    int
	Tickets []TicketEntry
}

// BuildTickets returns the answers to the tickets: the title page, the table of contents, the mark GeneratedMark,
// each ticket under «Билет N. <title>» with its claims and their sources «[источник: лекция …]» (a claim without one
// is marked UnsupportedMark), the parts the lessons miss, NotInLectures for a ticket they do not cover,
// TicketNotAnswered for a refused one, and the closing coverage summary. Texts are plain text: one-line texts are
// cleaned of control and format characters; the renderer escapes them.
func BuildTickets(in TicketsInput) values.Document {
	st := in.Student
	t := values.TitlePage{
		University: values.CleanName(st.University),
		Faculty:    values.CleanName(st.Faculty),
		Heading:    ticketsHeading,
		Performer:  performer(values.CleanName(st.Group), values.CleanName(st.FullName)),
		Place:      place(values.CleanName(st.City), in.Year),
	}
	if subject := values.CleanName(in.Subject); subject != "" {
		t.Heading = strings.Replace(ticketsBySubject, "%s", subject, 1)
	}

	doc := values.Document{Title: t, TableOfContents: true}
	doc.Blocks = append(doc.Blocks, paragraph(GeneratedMark))
	var r ticketset.Report
	for _, e := range in.Tickets {
		heading := ticketPrefix + strconv.Itoa(e.Number) + "."
		if title := oneLine(e.Title); title != "" {
			heading += " " + title
		}
		doc.Blocks = append(doc.Blocks, values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{heading}})
		switch e.Coverage {
		case ticketset.CoverageCovered, ticketset.CoveragePartial:
			doc.Blocks = append(doc.Blocks, answerBlocks(e)...)
		case ticketset.CoverageFailed:
			doc.Blocks = append(doc.Blocks, paragraph(TicketNotAnswered))
		default:
			doc.Blocks = append(doc.Blocks, paragraph(NotInLectures))
		}
		switch e.Coverage {
		case ticketset.CoverageCovered:
			r.Covered++
		case ticketset.CoveragePartial:
			r.Partial++
		case ticketset.CoverageFailed:
			r.Failed++
		default:
			r.NotCovered++
		}
	}

	summary := []string{
		coverageTotal + strconv.Itoa(len(in.Tickets)), coverageCovered + strconv.Itoa(r.Covered),
		coveragePartial + strconv.Itoa(r.Partial), coverageNotCovered + strconv.Itoa(r.NotCovered),
	}
	if r.Failed > 0 {
		summary = append(summary, coverageFailed+strconv.Itoa(r.Failed))
	}
	doc.Blocks = append(doc.Blocks,
		values.Block{Kind: values.BlockHeading, Level: values.MinHeadingLevel, Lines: []string{CoverageHeading}},
		values.Block{Kind: values.BlockParagraph, Lines: summary})
	return doc
}

// answerBlocks are the claims of a covered or partial ticket, then its missing parts.
func answerBlocks(e TicketEntry) []values.Block {
	var out []values.Block
	for _, c := range e.Claims {
		text := oneLine(c.Text)
		if text == "" {
			continue
		}
		var labels []string
		for _, s := range c.Sources {
			if l := sourceLabel(s); !slices.Contains(labels, l) {
				labels = append(labels, l)
			}
		}
		if len(labels) == 0 {
			out = append(out, paragraph(text+UnsupportedMark))
			continue
		}
		out = append(out, paragraph(text+" "+strings.Join(labels, " ")))
	}
	for _, m := range e.Missing {
		if text := oneLine(m); text != "" {
			out = append(out, paragraph(MissingLabel+text))
		}
	}
	return out
}

// sourceLabel is «[источник: лекция «<название>», <дата>, <раздел>]», the parts that are known.
func sourceLabel(s TicketSource) string {
	var parts []string
	if lesson := oneLine(s.Lesson); lesson != "" {
		if utf8.RuneCountInString(lesson) > maxLessonRunes {
			lesson = strings.TrimSpace(string([]rune(lesson)[:maxLessonRunes])) + "…"
		}
		parts = append(parts, "«"+lesson+"»")
	}
	if !s.Date.IsZero() {
		parts = append(parts, s.Date.Format(dateLayout))
	}
	if name := sectionName(s.Section); name != "" {
		parts = append(parts, name)
	}
	return strings.TrimSpace(sourceOpen+strings.Join(parts, ", ")) + sourceClose
}

// TicketsFileName is the name of a tickets file: «Билеты <subject>», safe for Windows, with DocxExtension.
func TicketsFileName(subject string) string {
	return FileName(strings.TrimSpace(ticketsFile + " " + values.CleanName(subject)))
}

func paragraph(line string) values.Block {
	return values.Block{Kind: values.BlockParagraph, Lines: []string{line}}
}

// oneLine makes untrusted text one plain line without cutting it: control and format characters (bidi, zero-width)
// removed or turned into spaces, spaces collapsed.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r) || r == 0x2028 || r == 0x2029:
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}
