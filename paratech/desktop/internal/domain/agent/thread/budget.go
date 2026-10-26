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

// History window sent to the planner, this thread only: at most MaxHistoryTurns earlier requests whose pairs hold at
// most MaxHistoryBytes, and at most MaxRequestTurns turns in the whole step request (the Hub limit, ADR-0023).
const (
	MaxHistoryTurns = 12
	MaxHistoryBytes = 24 << 10
	MaxRequestTurns = 64
)

// Window picks the history sent with a step from the stored entries of this thread, oldest first: every entry of the
// current request and, before them, earlier requests as pairs, the student's texts then one agent reply built by Reply
// (ADR-0023 "History pairs"). A pair is the unit of the cut: the newest pairs that fit WindowStart and MaxRequestTurns
// are sent whole. Echoes of earlier requests are never sent: the Hub keeps the model turn of an echo only for a while,
// and it looks it up for the last echo of the window, which must therefore belong to the current request. Every
// earlier request sent has its reply, because the Hub merges consecutive texts once any reply is present. The current
// request is always sent whole.
func Window(entries []values.HistoryEntry, current int) []values.HistoryTurn {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		cur = len(entries)
	}
	now := entries[cur:]

	// A reply stands between two student texts; without the current text first no pair may precede it.
	var pairs [][]values.HistoryTurn
	if len(now) > 0 && now[0].Entry.StudentText != "" {
		pairs = earlierPairs(entries[:cur])
	}
	sizes := make([]int, len(pairs))
	turns := len(now)
	for i, p := range pairs {
		for _, h := range p {
			sizes[i] += h.Size()
		}
	}
	start := WindowStart(sizes)
	for _, p := range pairs[start:] {
		turns += len(p)
	}
	for start < len(pairs) && turns > MaxRequestTurns {
		turns -= len(pairs[start])
		start++
	}

	out := make([]values.HistoryTurn, 0, turns)
	for _, p := range pairs[start:] {
		out = append(out, p...)
	}
	for _, e := range now {
		out = append(out, e.Entry)
	}
	return out
}

// earlierPairs groups the entries of earlier requests by request: its student texts in order, then its reply. A
// request whose texts are not among the entries (cut off by the read limit) has no pair.
func earlierPairs(entries []values.HistoryEntry) [][]values.HistoryTurn {
	var pairs [][]values.HistoryTurn
	for i := 0; i < len(entries); {
		j := i
		for j < len(entries) && entries[j].Turn == entries[i].Turn {
			j++
		}
		request := make([]values.HistoryTurn, 0, j-i)
		var pair []values.HistoryTurn
		for _, e := range entries[i:j] {
			request = append(request, e.Entry)
			if e.Entry.StudentText != "" {
				pair = append(pair, values.HistoryTurn{StudentText: e.Entry.StudentText})
			}
		}
		if len(pair) > 0 {
			pairs = append(pairs, append(pair, values.HistoryTurn{AgentReply: Reply(request)}))
		}
		i = j
	}
	return pairs
}

// WindowStart returns the index of the oldest unit sent in full, given the sizes of the units (earlier requests)
// oldest first: at most MaxHistoryTurns units and MaxHistoryBytes bytes, counted from the newest. Units before the
// index are not sent; len(sizes) means none is sent.
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
