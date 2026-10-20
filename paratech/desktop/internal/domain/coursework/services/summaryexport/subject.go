package summaryexport

import (
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/summary"
)

// Headings and texts of an exported subject (Russian UI).
const (
	subjectSummaryHeading = "Кратко"
	subjectLectureHeading = "Текст лекции"
	subjectNoSummary      = "Конспекта пока нет."
	subjectNoLectureText  = "Текста лекции пока нет."
	subjectAdHocTitle     = "Запись"
	subjectFileFallback   = "База знаний"
)

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

// SubjectLesson is one lesson of an exported subject. Times are in the zone to show.
type SubjectLesson struct {
	Title    string // "" for an ad-hoc recording
	Kind     string // lesson kind code; "" or unknown: not shown
	Start    time.Time
	Duration time.Duration       // 0: unknown, not shown
	Summary  summary.Content     // zero: no summary
	Blocks   []lecturetext.Block // nil: no lecture text
}

// SubjectDocument is what RenderSubject renders: one subject with its lessons in the order to write them.
type SubjectDocument struct {
	Subject string
	Lessons []SubjectLesson
}

// RenderSubject renders a whole subject as one Markdown file (TASK-091): the subject as the first heading, then per
// lesson a heading «19.10.2026, 10:00 · лекция · Название», its summary (its sections as headings, TASK-101) with the
// key points and its lecture text with the time of each block. Transcripts are not included: the lecture text is the full text of the lesson. Every text
// is escaped (shared.EscapeMarkdown), so model output forms no markup.
func RenderSubject(d SubjectDocument) string {
	var b strings.Builder
	b.WriteString("# " + line(oneLine(d.Subject), true) + "\n")

	for _, l := range d.Lessons {
		b.WriteString("\n## " + line(lessonHeading(l), true) + "\n\n")
		if length := FormatDuration(l.Duration); length != "" {
			b.WriteString(line(length, true) + "\n\n")
		}

		b.WriteString("### " + subjectSummaryHeading + "\n\n")
		if l.Summary.IsZero() {
			b.WriteString(subjectNoSummary + "\n")
		} else {
			writeSummary(&b, l.Summary, true, "#### ")
			if points := l.Summary.KeyPoints(); len(points) > 0 {
				b.WriteString("\n#### " + keyPointsHeading + "\n\n")
				for _, p := range points {
					b.WriteString("- " + line(oneLine(p), true) + "\n")
				}
			}
		}

		b.WriteString("\n### " + subjectLectureHeading + "\n\n")
		if len(l.Blocks) == 0 {
			b.WriteString(subjectNoLectureText + "\n")
		}
		for i, blk := range l.Blocks {
			if i > 0 {
				b.WriteString("\n")
			}
			heading := "[" + BlockTime(blk.StartMs()) + "] " + oneLine(blk.Heading())
			b.WriteString("#### " + line(heading, true) + "\n\n")
			for j, p := range blk.Paragraphs() {
				if j > 0 {
					b.WriteString("\n")
				}
				writeText(&b, p, true)
			}
		}
	}
	return b.String()
}

// SubjectFileName returns the Markdown file name of an exported subject: the subject, safe for Windows.
func SubjectFileName(subject string) string {
	name := FileName(subject, FormatMarkdown)
	if name == fallbackFileName+FormatMarkdown.Extension() {
		name = FileName(subjectFileFallback, FormatMarkdown)
	}
	return name
}

// lessonHeading is «<date, time> · <kind> · <title>»; an ad-hoc recording is titled «Запись».
func lessonHeading(l SubjectLesson) string {
	var parts []string
	if !l.Start.IsZero() {
		parts = append(parts, l.Start.Format(dateLayout))
	}
	if kind := kindText(l.Kind); kind != "" {
		parts = append(parts, kind)
	}
	title := oneLine(l.Title)
	if title == "" {
		title = subjectAdHocTitle
	}
	return strings.Join(append(parts, title), headSeparator)
}
