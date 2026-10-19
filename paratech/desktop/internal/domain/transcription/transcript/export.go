package transcript

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// ExportFormat is the file format of a transcript the user saves or copies (TASK-079).
type ExportFormat string

// Export formats.
const (
	FormatText     ExportFormat = "txt"
	FormatMarkdown ExportFormat = "md"
)

// MaxExportBytes bounds a rendered transcript: Markdown escaping at most doubles the text of a document.
const MaxExportBytes = 2 * MaxDocumentBytes

// fallbackFileName is used when nothing of the proposed name is usable.
const fallbackFileName = "transcript"

// Validate accepts the known formats.
func (f ExportFormat) Validate() error {
	switch f {
	case FormatText, FormatMarkdown:
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownFormat, string(f))
}

// Extension returns the file extension with its dot.
func (f ExportFormat) Extension() string { return "." + string(f) }

// FormatOffset renders a time from the start of the audio as mm:ss below one hour and hh:mm:ss from one hour on.
// Negative values count as 0.
func FormatOffset(ms int64) string {
	sec := max(ms, 0) / 1000
	h, m, s := sec/3600, sec/60%60, sec%60
	if h == 0 {
		return fmt.Sprintf("%02d:%02d", m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// ExportFileName returns a Windows-safe file name for base (for example the lesson title and date) with the
// extension of f.
func ExportFileName(base string, f ExportFormat) string {
	name := shared.SafeFileName(base)
	if name == "" {
		name = fallbackFileName
	}
	return name + f.Extension()
}

// Render renders segments as plain text or Markdown, one segment per line (Markdown: per paragraph), with its start
// time when withTimestamps is set. title, if not empty, is the first line (a Markdown heading). Empty segments are
// left out. Plain text is written as it is; in Markdown the text is escaped (escapeMarkdown), so recognised speech
// can form neither HTML nor links nor other markup.
func Render(title string, segments []Segment, f ExportFormat, withTimestamps bool) (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}

	var b strings.Builder
	md := f == FormatMarkdown
	// One line whatever the caller passes: a line break would end the heading.
	if title = strings.Join(strings.Fields(title), " "); title != "" {
		if md {
			b.WriteString("# " + escapeMarkdown(title) + "\n\n")
		} else {
			b.WriteString(title + "\n\n")
		}
	}

	for _, s := range segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		switch {
		case md && withTimestamps:
			b.WriteString("**" + FormatOffset(s.StartMs) + "** " + escapeMarkdown(text) + "\n\n")
		case md:
			b.WriteString(escapeMarkdown(text) + "\n\n")
		case withTimestamps:
			b.WriteString("[" + FormatOffset(s.StartMs) + "] " + text + "\n")
		default:
			b.WriteString(text + "\n")
		}
		if b.Len() > MaxExportBytes {
			return "", fmt.Errorf("%w: more than %d bytes", ErrExportTooLarge, MaxExportBytes)
		}
	}
	return b.String(), nil
}

// escapeMarkdown escapes one line of recognised speech for Markdown (shared.EscapeMarkdown).
func escapeMarkdown(s string) string { return shared.EscapeMarkdown(s) }
