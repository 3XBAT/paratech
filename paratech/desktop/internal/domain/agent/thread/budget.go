package thread

import (
	"slices"

	"paratech/desktop/internal/domain/agent/values"
)

// Budget limits of one request (ADR-0021). Over a limit the request stops with «Остановлено: лимит просьбы».
const (
	MaxSteps         = 8
	MaxCallsPerStep  = 4
	MaxWrites        = 6
	MaxPaidUnplanned = 1 // paid jobs without an approved plan
	MaxInputTokens   = 60_000
	MaxRetries       = 3 // «Повторить» of one failed step; each retry is a new paid attempt on the Hub
)

// Budget is what a request has used so far.
type Budget struct {
	Steps          int
	Writes         int
	PaidUnplanned  int
	InputTokens    int
	QuestionRounds int
}

func (b Budget) valid() bool {
	return b.Steps >= 0 && b.Steps <= MaxSteps && b.Writes >= 0 && b.Writes <= MaxWrites &&
		b.PaidUnplanned >= 0 && b.PaidUnplanned <= MaxPaidUnplanned && b.InputTokens >= 0 &&
		b.QuestionRounds >= 0 && b.QuestionRounds <= maxRounds
}

// History window sent to the planner: the last turns of this thread only.
const (
	MaxHistoryTurns = 12
	MaxHistoryBytes = 24 << 10
)

// Window picks the history sent with a step from the stored entries of this thread, oldest first: every entry of the
// current request and, before them, the earlier entries that fit WindowStart. The window starts at the student's text
// of a request, the shape the Hub accepts (an echo of calls is always followed by its results in storage).
func Window(entries []values.HistoryEntry, current int) []values.HistoryTurn {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		cur = len(entries)
	}
	sizes := make([]int, cur)
	for i, e := range entries[:cur] {
		sizes[i] = e.Entry.Size()
	}
	start := WindowStart(sizes)
	for start < cur && !requestStart(entries, start) {
		start++
	}
	out := make([]values.HistoryTurn, 0, len(entries)-start)
	for _, e := range entries[start:] {
		out = append(out, e.Entry)
	}
	return out
}

func requestStart(entries []values.HistoryEntry, i int) bool {
	return entries[i].Entry.StudentText != "" && (i == 0 || entries[i-1].Turn != entries[i].Turn)
}

// WindowStart returns the index of the oldest turn sent in full, given the sizes of the turns oldest first: at most
// MaxHistoryTurns turns and MaxHistoryBytes bytes, counted from the newest. Turns before the index shrink to action
// lines; len(sizes) means none is sent in full.
func WindowStart(sizes []int) int {
	total := 0
	start := len(sizes)
	for i := len(sizes) - 1; i >= 0 && len(sizes)-i <= MaxHistoryTurns; i-- {
		if sizes[i] < 0 || total+sizes[i] > MaxHistoryBytes {
			break
		}
		total += sizes[i]
		start = i
	}
	return start
}
