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
// current request and, before them, the student's texts of earlier requests that fit WindowStart. Echoes of earlier
// requests are never sent: the Hub keeps the model turn of an echo only for a while, and it looks it up for the last
// echo of the window, which must therefore belong to the current request (ADR-0023). Student texts in a row are a
// shape the Hub accepts.
func Window(entries []values.HistoryEntry, current int) []values.HistoryTurn {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		cur = len(entries)
	}
	var earlier []values.HistoryTurn
	for _, e := range entries[:cur] {
		if e.Entry.StudentText != "" {
			earlier = append(earlier, values.HistoryTurn{StudentText: e.Entry.StudentText})
		}
	}
	sizes := make([]int, len(earlier))
	for i, e := range earlier {
		sizes[i] = e.Size()
	}
	start := WindowStart(sizes)
	out := make([]values.HistoryTurn, 0, len(earlier)-start+len(entries)-cur)
	out = append(out, earlier[start:]...)
	for _, e := range entries[cur:] {
		out = append(out, e.Entry)
	}
	return out
}

// WindowStart returns the index of the oldest turn sent in full, given the sizes of the turns oldest first: at most
// MaxHistoryTurns turns and MaxHistoryBytes bytes, counted from the newest. Turns before the index are not sent;
// len(sizes) means none is sent.
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
