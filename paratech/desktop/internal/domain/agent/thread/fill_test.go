package thread

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func TestFillPercentAndLevel(t *testing.T) {
	// The share of the window at which bytes alone reach p percent: the least n with n*100/MaxHistoryBytes ≥ p.
	bytesAt := func(p int) int { return (p*MaxHistoryBytes + 99) / 100 }

	tests := []struct {
		name    string
		fill    ContextFill
		percent int
		level   FillLevel
	}{
		{"empty", ContextFill{}, 0, FillNormal},
		{"13 pairs", Fill(chatHistory(13, numbered), 14, noSummary), 65, FillNormal},
		{"14 pairs", Fill(chatHistory(14, numbered), 15, noSummary), 70, FillHigh},
		{"17 pairs", Fill(chatHistory(17, numbered), 18, noSummary), 85, FillHigh},
		{"18 pairs", Fill(chatHistory(18, numbered), 19, noSummary), 90, FillFull},
		{"20 pairs", Fill(chatHistory(20, numbered), 21, noSummary), 100, FillFull},
		{"bytes 69", ContextFill{Bytes: bytesAt(70) - 1}, 69, FillNormal},
		{"bytes 70", ContextFill{Bytes: bytesAt(70)}, 70, FillHigh},
		{"bytes 89", ContextFill{Bytes: bytesAt(90) - 1}, 89, FillHigh},
		{"bytes 90", ContextFill{Bytes: bytesAt(90)}, 90, FillFull},
		// 0.29*100 in floats is 28.999…; integers keep 29 % at 29.
		{"bytes 29 floor", ContextFill{Bytes: bytesAt(29)}, 29, FillNormal},
		{"bytes just below 29", ContextFill{Bytes: bytesAt(29) - 1}, 28, FillNormal},
		{"turns floor", ContextFill{Turns: 19}, 29, FillNormal}, // 29.69 %
		{"turns 70", ContextFill{Turns: 45}, 70, FillHigh},      // 70.3 %
		{"turns 89", ContextFill{Turns: 57}, 89, FillHigh},      // 89.06 %
		{"turns 90", ContextFill{Turns: 58}, 90, FillFull},      // 90.6 %
		{"largest share wins", ContextFill{Pairs: 2, Bytes: bytesAt(50), Turns: 10}, 50, FillNormal},
		{"clamped", ContextFill{Bytes: 2 * MaxHistoryBytes, Turns: MaxRequestTurns + 3}, 100, FillFull},
		{"overflow", ContextFill{Pairs: 1, Turns: 3, Overflow: true}, 100, FillFull},
		{"overflow of a real chat", Fill(chatHistory(21, numbered), 22, noSummary), 100, FillFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fill.Percent(); got != tt.percent {
				t.Fatalf("Percent(%+v) = %d, want %d", tt.fill, got, tt.percent)
			}
			if got := tt.fill.Level(); got != tt.level {
				t.Fatalf("Level(%+v) = %q, want %q", tt.fill, got, tt.level)
			}
		})
	}
}

func TestFillLevelStrings(t *testing.T) {
	if FillNormal != "normal" || FillHigh != "high" || FillFull != "full" {
		t.Fatalf("levels = %q %q %q", FillNormal, FillHigh, FillFull)
	}
}

// TestFillWithSummary: the summary counts as one turn and its bytes; the requests it covers are neither pairs nor an
// overflow, so a compaction lowers the fill.
func TestFillWithSummary(t *testing.T) {
	entries := chatHistory(21, numbered)
	before := Fill(entries, 22, noSummary)
	if !before.Overflow || before.Percent() != 100 {
		t.Fatalf("before the compaction %+v, %d %%", before, before.Percent())
	}

	text := strings.Repeat("сводка ", 100)
	summary, err := values.ParseHistorySummary(10, text)
	if err != nil {
		t.Fatal(err)
	}
	after := Fill(entries, 22, summary)
	if after.Overflow || after.Pairs != 11 {
		t.Fatalf("after the compaction %+v", after)
	}
	pairs := Fill(chatHistory(21, numbered)[3*10:], 22, noSummary) // requests 11–21 alone
	if after.Turns != pairs.Turns+1 || after.Bytes != pairs.Bytes+summary.Turn().Size() {
		t.Fatalf("summary not counted: %+v, pairs alone %+v", after, pairs)
	}
	if after.Percent() != 55 || after.Level() != FillNormal {
		t.Fatalf("after the compaction %d %% %s", after.Percent(), after.Level())
	}
}
