package summaryexport

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

// ErrUnknownFormat is returned for a format other than FormatText and FormatMarkdown.
var ErrUnknownFormat = errors.New("unknown summary export format")

// Format is the file format of an exported summary.
type Format string

// Export formats.
const (
	FormatText     Format = "txt"
	FormatMarkdown Format = "md"
)

// Validate accepts the known formats.
func (f Format) Validate() error {
	switch f {
	case FormatText, FormatMarkdown:
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownFormat, string(f))
}

// Extension returns the file extension with its dot.
func (f Format) Extension() string { return "." + string(f) }

// Headings and layouts of the rendered summary (Russian UI).
const (
	keyPointsHeading = "Ключевые моменты"
	homeworkHeading  = "Домашние задания"
	dueText          = "до %s"
	dateLayout       = "02.01.2006, 15:04"
	headSeparator    = " · "
	fallbackFileName = "summary"
)

// Homework is an assignment found in the lesson, as the export lists it.
type Homework struct {
	Title       string
	Description string
	Due         time.Time // in the zone to show; zero: no deadline
}

// Document is what Render renders. Times are in the zone to show.
type Document struct {
	Title    string
	Start    time.Time     // zero: not shown
	Duration time.Duration // 0: unknown, not shown
	Content  summary.Content
	Homework []Homework
}

// Render renders d as plain text or Markdown: the title (a Markdown heading), a line with the date and length, the
// summary text, the key points and the homework as lists. Sections without items are left out.
func Render(d Document, f Format) (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	md := f == FormatMarkdown
	var b strings.Builder

	// One line whatever the caller passes: a line break would end the heading.
	if title := oneLine(d.Title); title != "" {
		if md {
			b.WriteString("# " + shared.EscapeMarkdown(title) + "\n\n")
		} else {
			b.WriteString(title + "\n")
		}
	}
	if head := headLine(d); head != "" {
		b.WriteString(line(head, md) + "\n\n")
	} else if !md && b.Len() > 0 {
		b.WriteString("\n")
	}

	writeText(&b, d.Content.Text(), md)

	if points := d.Content.KeyPoints(); len(points) > 0 {
		b.WriteString("\n" + heading(keyPointsHeading, md))
		for _, p := range points {
			b.WriteString("- " + line(p, md) + "\n")
		}
	}

	if len(d.Homework) > 0 {
		b.WriteString("\n" + heading(homeworkHeading, md))
		for _, h := range d.Homework {
			item := line(oneLine(h.Title), md)
			if !h.Due.IsZero() {
				item += " — " + line(fmt.Sprintf(dueText, h.Due.Format(dateLayout)), md)
			}
			b.WriteString("- " + item + "\n")
			if desc := oneLine(h.Description); desc != "" {
				b.WriteString("  " + line(desc, md) + "\n")
			}
		}
	}
	return b.String(), nil
}

// FileName returns a Windows-safe file name for base (for example "Конспект", the lesson title and the date) with the
// extension of f.
func FileName(base string, f Format) string {
	name := shared.SafeFileName(base)
	if name == "" {
		name = fallbackFileName
	}
	return name + f.Extension()
}

// FormatDuration renders a length as "1 ч 25 мин", "1 ч" or "45 мин", rounded to minutes and at least "1 мин";
// 0 and less is "".
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	m := max(int(d.Round(time.Minute)/time.Minute), 1)
	h, m := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d мин", m)
	case m == 0:
		return fmt.Sprintf("%d ч", h)
	}
	return fmt.Sprintf("%d ч %d мин", h, m)
}

func headLine(d Document) string {
	var parts []string
	if !d.Start.IsZero() {
		parts = append(parts, d.Start.Format(dateLayout))
	}
	if l := FormatDuration(d.Duration); l != "" {
		parts = append(parts, l)
	}
	return strings.Join(parts, headSeparator)
}

func heading(s string, md bool) string {
	if md {
		return "## " + s + "\n\n"
	}
	return s + ":\n"
}

// writeText writes the summary text line by line, keeping its paragraphs. In Markdown the indentation is dropped:
// four spaces would make a code block.
func writeText(b *strings.Builder, text string, md bool) {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if md {
			l = strings.TrimLeft(l, " \t")
		}
		b.WriteString(line(l, md) + "\n")
	}
}

func line(s string, md bool) string {
	if md {
		return shared.EscapeMarkdown(s)
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
