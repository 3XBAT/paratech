package values

import (
	"fmt"
	"strings"
)

// LineKind says what the action of an action line did, so the panel words its line from a template (catalog v10,
// ADR-0033 §2, §8, §10). The desktop sets it from the use case's outcome, never from model text.
type LineKind string

// Line kinds.
const (
	LineMarkedDone       LineKind = "marked_done"       // «Отмечено выполненным: N»
	LineMarkedUndone     LineKind = "marked_undone"     // mark_assignments_done{done: false}: back to work, N
	LineNoteText         LineKind = "note_text"         // «Записано в конспект: …»
	LineNoteUpload       LineKind = "note_upload"       // an upload added to a note
	LineNoteMove         LineKind = "note_move"         // a note moved to a lesson or a day
	LineNoteRename       LineKind = "note_rename"       // a note renamed
	LineNoteRemove       LineKind = "note_remove"       // a note removed (softly)
	LineRecordingStarted LineKind = "recording_started" // «Запись началась · Остановить»
	LineRecordingStopped LineKind = "recording_stopped" // «Запись остановлена · Начать запись»
	LineSetting          LineKind = "setting"           // a setting of the allowlist set to a value of its own
)

// Limits of a line result.
const (
	MaxLineCount        = 50  // assignments a marked line counts
	MaxExcerptLines     = 3   // lines of the excerpt of a saved text
	MaxExcerptLineRunes = 120 // characters of one excerpt line, the ellipsis included
)

// excerptEllipsis ends an excerpt line that was cut.
const excerptEllipsis = "…"

// Valid reports whether k is one of the kinds.
func (k LineKind) Valid() bool {
	switch k {
	case LineMarkedDone, LineMarkedUndone, LineNoteText, LineNoteUpload, LineNoteMove, LineNoteRename, LineNoteRemove,
		LineRecordingStarted, LineRecordingStopped, LineSetting:
		return true
	}
	return false
}

// LineResult is what an action line shows of its action beyond «Отменить»; the zero value is the generic line. Count is
// the number of assignments a marked line changed; Setting and Value are the setting a setting line set and its new
// value, both from the closed enums; Excerpt is the start of the text a note_text line saved: up to MaxExcerptLines
// lines joined with "\n", each of up to MaxExcerptLineRunes printable characters. The excerpt is the student's own
// text, plain text only, set only on the line of the live update (a call replayed from the journal has none).
type LineResult struct {
	Kind    LineKind
	Count   int
	Setting Setting
	Value   string
	Excerpt string
}

// IsZero reports the generic line.
func (r LineResult) IsZero() bool { return r == LineResult{} }

// Validate checks the result: the zero value, or a known kind with exactly the fields of its kind.
func (r LineResult) Validate() error {
	if r.IsZero() {
		return nil
	}
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: unknown line kind", ErrInvalidCard)
	}
	marked := r.Kind == LineMarkedDone || r.Kind == LineMarkedUndone
	switch {
	case marked && (r.Count <= 0 || r.Count > MaxLineCount):
		return fmt.Errorf("%w: a marked line counts 1..%d assignments", ErrInvalidCard, MaxLineCount)
	case !marked && r.Count != 0:
		return fmt.Errorf("%w: only a marked line has a count", ErrInvalidCard)
	}
	if r.Kind == LineSetting {
		if !r.Setting.Valid() || !ValidSettingValue(r.Setting, r.Value) {
			return fmt.Errorf("%w: a setting line names a setting and a value of it", ErrInvalidCard)
		}
	} else if r.Setting != "" || r.Value != "" {
		return fmt.Errorf("%w: only a setting line has a setting", ErrInvalidCard)
	}
	if r.Excerpt == "" {
		return nil
	}
	if r.Kind != LineNoteText {
		return fmt.Errorf("%w: only a note_text line has an excerpt", ErrInvalidCard)
	}
	return validExcerpt(r.Excerpt)
}

func validExcerpt(s string) error {
	lines := strings.Split(s, "\n")
	if len(lines) > MaxExcerptLines {
		return fmt.Errorf("%w: an excerpt has at most %d lines", ErrInvalidCard, MaxExcerptLines)
	}
	for _, l := range lines {
		if l != strings.TrimSpace(l) || strings.Contains(l, "  ") {
			return fmt.Errorf("%w: an excerpt line is not trimmed", ErrInvalidCard)
		}
		// The student's own text, rendered as plain text: angle brackets are allowed.
		if err := checkPrintable(l, MaxExcerptLineRunes, false, true); err != nil {
			return fmt.Errorf("%w: excerpt: %w", ErrInvalidCard, err)
		}
	}
	return nil
}

// NoteExcerpt is the excerpt of text a note_text line shows: its first MaxExcerptLines non-blank lines, each with
// control and other non-printable characters turned into spaces (format characters dropped), spaces collapsed and cut
// to MaxExcerptLineRunes with an ellipsis. "" when text has no printable line.
func NoteExcerpt(text string) string {
	var lines []string
	for raw := range strings.SplitSeq(text, "\n") {
		l := excerptLine(raw)
		if l == "" {
			continue
		}
		lines = append(lines, l)
		if len(lines) == MaxExcerptLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func excerptLine(s string) string { return dataLine(s, MaxExcerptLineRunes) }
