package thread

// HistoryReadLimit is the most history entries read to choose a window: enough whole requests (a text, then an echo
// and results per step) for the texts of the earlier window plus the current request, which may run a plan of
// MaxPlanRunSteps steps. The context ring reads exactly as a step does.
const HistoryReadLimit = MaxHistoryTurns*(2*MaxSteps+1) + 2*MaxPlanRunSteps + 1

// FillLevel is how the context ring shows a chat's fill (ADR-0028 «The ring»); the strings go to the UI as they are.
type FillLevel string

const (
	FillNormal FillLevel = "normal" // below FillHighPercent
	FillHigh   FillLevel = "high"   // from FillHighPercent: the accent colour
	FillFull   FillLevel = "full"   // from FillFullPercent: the hint «Начать новый чат»
)

// Thresholds of the levels, in percent of the window.
const (
	FillHighPercent = 70
	FillFullPercent = 90
)

// Percent is the share of the window the next request would use, in whole percent rounded down, at most 100: the
// largest of the pairs, bytes and turns shares, in integers so that no float rounding moves a threshold. An Overflow
// (an earlier request neither sent nor covered by the summary) is 100: the ring shows it as full.
func (f ContextFill) Percent() int {
	if f.Overflow {
		return 100
	}
	p := max(f.Pairs*100/MaxHistoryTurns, f.Bytes*100/MaxHistoryBytes, f.Turns*100/MaxRequestTurns, 0)
	return min(p, 100)
}

// Level is the level of Percent: below 70 normal, 70–89 high, from 90 full.
func (f ContextFill) Level() FillLevel {
	switch p := f.Percent(); {
	case p >= FillFullPercent:
		return FillFull
	case p >= FillHighPercent:
		return FillHigh
	default:
		return FillNormal
	}
}
