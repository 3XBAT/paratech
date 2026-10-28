package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// around puts r between two letters; runes are given by number so no format character is in the source.
func around(r rune) string { return "a" + string(r) + "b" }

func TestParseHistorySummary(t *testing.T) {
	type tc struct {
		name    string
		through int
		text    string
		want    string // "" when refused
	}
	tests := []tc{
		{"plain", 1, "Студент спрашивал о пределах.", "Студент спрашивал о пределах."},
		{"line breaks and trim", 10, "  первая\nвторая\n", "первая\nвторая"},
		{"angle brackets are plain text", 3, "a <b> c", "a <b> c"},
		{"max runes", 5, strings.Repeat("a", MaxSummaryRunes), strings.Repeat("a", MaxSummaryRunes)},
		{"max runes in two bytes", 5, strings.Repeat("я", MaxSummaryRunes), strings.Repeat("я", MaxSummaryRunes)},
		{"too many runes", 5, strings.Repeat("a", MaxSummaryRunes+1), ""},
		{"too many bytes", 5, strings.Repeat("€", MaxSummaryBytes/3+1), ""},
		{"empty", 1, "", ""},
		{"blank", 1, " \n ", ""},
		{"through 0", 0, "текст", ""},
		{"negative through", -1, "текст", ""},
		{"tab", 1, "a\tb", ""},
		{"carriage return", 1, "a\r\nb", ""},
		{"NUL", 1, "a\x00b", ""},
		{"invalid UTF-8", 1, "a\xffb", ""},
	}
	for _, r := range []rune{0x2028, 0x2029, 0xFFFD, 0x200B, 0xFEFF} {
		tests = append(tests, tc{fmt.Sprintf("U+%04X", r), 1, around(r), ""})
	}
	// The bidi characters named by ADR-0028.
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x200E, 0x200F, 0x061C} {
		tests = append(tests, tc{fmt.Sprintf("bidi U+%04X", r), 1, around(r), ""})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := ParseHistorySummary(tt.through, tt.text)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidSummary) || !s.IsZero() {
					t.Fatalf("ParseHistorySummary = %+v, %v; want ErrInvalidSummary", s, err)
				}
				return
			}
			if err != nil || s.Text() != tt.want || s.Through() != tt.through || s.IsZero() {
				t.Fatalf("ParseHistorySummary = %+v, %v", s, err)
			}
			if turn := s.Turn(); turn.Summary != tt.want || turn.Validate() != nil || turn.Size() != len(tt.want) {
				t.Fatalf("Turn = %+v, Validate %v", turn, turn.Validate())
			}
		})
	}
}

func TestBidiRune(t *testing.T) {
	for r := rune(0); r <= 0x3000; r++ {
		want := r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r == 0x200E || r == 0x200F || r == 0x061C
		if bidiRune(r) != want {
			t.Fatalf("bidiRune(U+%04X) = %v", r, !want)
		}
	}
}

func TestHistoryTurnSummary(t *testing.T) {
	tests := []struct {
		name string
		turn HistoryTurn
		ok   bool
	}{
		{"summary", HistoryTurn{Summary: "сводка"}, true},
		{"summary and text", HistoryTurn{Summary: "сводка", StudentText: "вопрос"}, false},
		{"summary and reply", HistoryTurn{Summary: "сводка", AgentReply: "reply: x"}, false},
		{"summary too long", HistoryTurn{Summary: strings.Repeat("a", MaxSummaryRunes+1)}, false},
		{"summary with bidi", HistoryTurn{Summary: around(0x202E)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.turn.Validate(); (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidResult)) {
				t.Fatalf("Validate = %v, want ok %v", err, tt.ok)
			}
		})
	}
}
