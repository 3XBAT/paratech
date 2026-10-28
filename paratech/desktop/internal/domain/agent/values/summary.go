package values

import (
	"fmt"
	"strings"
)

// Limits of the history summary (ADR-0028 «Context compaction»; gateway.proto AgentTurn.history_summary).
const (
	MaxSummaryRunes = 4000
	MaxSummaryBytes = 8 << 10
)

// HistorySummary is the short summary of the earlier requests 1…Through of a chat. Its text is model output: untrusted
// data that is only ever sent back as the summary turn of the history; it is never shown as instructions, never a
// source of provenance IDs and never triggers an action (hard rule 3). The zero value is no summary.
type HistorySummary struct {
	through int
	text    string
}

// ParseHistorySummary trims text and checks it: Through ≥ 1; 1..MaxSummaryRunes printable characters and line
// breaks, at most MaxSummaryBytes of UTF-8, no control or bidi characters.
func ParseHistorySummary(through int, text string) (HistorySummary, error) {
	if through < 1 {
		return HistorySummary{}, fmt.Errorf("%w: a summary covers requests from 1", ErrInvalidSummary)
	}
	text = strings.TrimSpace(text)
	if err := checkSummaryText(text); err != nil {
		return HistorySummary{}, err
	}
	return HistorySummary{through: through, text: text}, nil
}

// checkSummaryText is the text rule of a summary. unicode.IsPrint already refuses the bidi marks, embeddings,
// overrides and isolates (all Cf); they are listed again so a later change of the printable rule keeps them out.
func checkSummaryText(s string) error {
	if len(s) > MaxSummaryBytes {
		return fmt.Errorf("%w: a summary is at most %d bytes", ErrInvalidSummary, MaxSummaryBytes)
	}
	if err := checkPrintable(s, MaxSummaryRunes, true, true); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSummary, err)
	}
	if strings.ContainsFunc(s, bidiRune) {
		return fmt.Errorf("%w: a summary has a bidi character", ErrInvalidSummary)
	}
	return nil
}

func bidiRune(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200E, r == 0x200F, r == 0x061C:
		return true
	}
	return false
}

// Through returns the number of the last request the summary covers; 0 for no summary.
func (s HistorySummary) Through() int { return s.through }

// Text returns the summary text (untrusted model output).
func (s HistorySummary) Text() string { return s.text }

// IsZero reports whether there is no summary.
func (s HistorySummary) IsZero() bool { return s == HistorySummary{} }

// Turn returns the summary as the history turn that opens the window.
func (s HistorySummary) Turn() HistoryTurn { return HistoryTurn{Summary: s.text} }
