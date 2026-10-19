package summaryexport

import (
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
)

// LectureDocument is what RenderLectureText renders (TASK-090). Times are in the zone to show.
type LectureDocument struct {
	Title    string
	Start    time.Time     // zero: not shown
	Duration time.Duration // 0: unknown, not shown
	Blocks   []lecturetext.Block
	// Times: each heading starts with the time of its block in the recording, "[12:05]" or "[1:02:05]".
	Times bool
}

// RenderLectureText renders the lecture text as plain text or Markdown: the title (a Markdown heading), a line with
// the date and length, then each block with its heading (a Markdown heading of the second level) and its paragraphs,
// separated by blank lines. The text is model output: in Markdown every line is escaped, so it forms no markup.
func RenderLectureText(d LectureDocument, f Format) (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	md := f == FormatMarkdown
	var b strings.Builder

	if title := oneLine(d.Title); title != "" {
		if md {
			b.WriteString("# " + line(title, true) + "\n\n")
		} else {
			b.WriteString(title + "\n")
		}
	}
	head := headLine(Document{Start: d.Start, Duration: d.Duration})
	if head != "" {
		b.WriteString(line(head, md) + "\n\n")
	} else if !md && b.Len() > 0 {
		b.WriteString("\n")
	}

	for i, blk := range d.Blocks {
		if i > 0 {
			b.WriteString("\n")
		}
		heading := oneLine(blk.Heading())
		if d.Times {
			heading = "[" + BlockTime(blk.StartMs()) + "] " + heading
		}
		if md {
			b.WriteString("## " + line(heading, true) + "\n\n")
		} else {
			b.WriteString(heading + "\n\n")
		}
		for j, p := range blk.Paragraphs() {
			if j > 0 {
				b.WriteString("\n")
			}
			writeText(&b, p, md)
		}
	}
	return b.String(), nil
}

// BlockTime renders the start of a block in the recording: "12:05", or "1:02:05" from one hour on.
func BlockTime(ms int64) string {
	s := max(ms, 0) / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
