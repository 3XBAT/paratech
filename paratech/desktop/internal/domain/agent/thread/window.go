package thread

import (
	"slices"

	"paratech/desktop/internal/domain/agent/values"
)

// History window sent to the planner, this chat only (ADR-0023, ADR-0028): at most MaxHistoryTurns earlier requests
// whose pairs hold at most MaxHistoryBytes, and at most MaxRequestTurns turns in the whole step request (the Hub
// limit). The window start moves in steps of WindowStep requests, aligned on the request number, so the history prefix
// stays byte-identical for up to WindowStep requests in a row (prompt cache).
const (
	MaxHistoryTurns = 20
	MaxHistoryBytes = 48 << 10
	MaxRequestTurns = 64
	WindowStep      = 5
)

// Window picks the history sent with a step from the stored entries of this chat, oldest first: every entry of the
// current request and, before them, earlier requests as pairs, the student's texts then one agent reply built by Reply
// (ADR-0023 "History pairs"). A pair is the unit of the cut; dropped requests are skipped. The first pair sent is the
// first of a block of WindowStep request numbers (1–5, 6–10, …): the oldest block start from which at most
// MaxHistoryTurns pairs follow, so 16–20 earlier answers go when there are enough. When the pairs exceed
// MaxHistoryBytes or the request MaxRequestTurns, the start moves one more block, and past the last block start pair by
// pair. Echoes of earlier requests are never sent: the Hub keeps the model turn of an echo only for a while, and it
// looks it up for the last echo of the window, which must therefore belong to the current request. Every earlier
// request sent has its reply, because the Hub merges consecutive texts once any reply is present. The current request
// is always sent whole.
func Window(entries []values.HistoryEntry, current int) []values.HistoryTurn {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		cur = len(entries)
	}
	now := entries[cur:]

	// A reply stands between two student texts; without the current text first no pair may precede it.
	var pairs []pair
	if len(now) > 0 && now[0].Entry.StudentText != "" {
		pairs = earlierPairs(entries[:cur])
	}
	start := windowStart(pairs, len(now))

	out := make([]values.HistoryTurn, 0, len(now)+2*(len(pairs)-start))
	for _, p := range pairs[start:] {
		out = append(out, p.turns...)
	}
	for _, e := range now {
		out = append(out, e.Entry)
	}
	return out
}

// ContextFill is how much of the history window the next request of a chat would use (ADR-0028 «What context used
// means»): the earlier pairs it carries, their bytes and the turns with its own text.
type ContextFill struct {
	Pairs int
	Bytes int
	Turns int
	// Share is max(Pairs/MaxHistoryTurns, Bytes/MaxHistoryBytes, Turns/MaxRequestTurns), from 0 to 1.
	Share float64
	// Overflow: an earlier request that is not dropped is no longer sent.
	Overflow bool
}

// Fill is the window the next request would carry: what Window sends before request current when that request is its
// text alone. entries are the stored entries of the chat before current; they should reach back past the window
// (Overflow sees only the requests among them).
func Fill(entries []values.HistoryEntry, current int) ContextFill {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		cur = len(entries)
	}
	earlier := entries[:cur]
	pairs := earlierPairs(earlier)
	start := windowStart(pairs, 1)

	f := ContextFill{Pairs: len(pairs) - start, Turns: 1}
	for _, p := range pairs[start:] {
		f.Bytes += p.size
		f.Turns += len(p.turns)
	}
	f.Share = max(float64(f.Pairs)/MaxHistoryTurns, float64(f.Bytes)/MaxHistoryBytes, float64(f.Turns)/MaxRequestTurns)

	live := map[int]bool{}
	for _, e := range earlier {
		if e.DroppedBy == 0 {
			live[e.Turn] = true
		}
	}
	f.Overflow = len(live) > f.Pairs
	return f
}

// pair is an earlier request as sent: its number, its student texts and the reply, and their bytes.
type pair struct {
	number int
	turns  []values.HistoryTurn
	size   int
}

// earlierPairs groups the entries of earlier requests by request: its student texts in order, then its reply. A
// request whose texts are not among the entries (cut off by the read limit) has no pair, nor has a dropped one.
func earlierPairs(entries []values.HistoryEntry) []pair {
	var pairs []pair
	for i := 0; i < len(entries); {
		j := i
		for j < len(entries) && entries[j].Turn == entries[i].Turn {
			j++
		}
		if entries[i].DroppedBy != 0 {
			i = j
			continue
		}
		request := make([]values.HistoryTurn, 0, j-i)
		p := pair{number: entries[i].Turn}
		for _, e := range entries[i:j] {
			request = append(request, e.Entry)
			if e.Entry.StudentText != "" {
				p.turns = append(p.turns, values.HistoryTurn{StudentText: e.Entry.StudentText})
			}
		}
		if len(p.turns) > 0 {
			p.turns = append(p.turns, values.HistoryTurn{AgentReply: Reply(request)})
			for _, h := range p.turns {
				p.size += h.Size()
			}
			pairs = append(pairs, p)
		}
		i = j
	}
	return pairs
}

// windowStart returns the index of the oldest pair sent, given the turns of the current request: len(pairs) sends none.
func windowStart(pairs []pair, nowTurns int) int {
	blockStart := func(i int) bool {
		return i == 0 || (pairs[i].number-1)/WindowStep != (pairs[i-1].number-1)/WindowStep
	}
	fits := func(start int) bool {
		bytes, turns := 0, nowTurns
		for _, p := range pairs[start:] {
			bytes += p.size
			turns += len(p.turns)
		}
		return bytes <= MaxHistoryBytes && turns <= MaxRequestTurns
	}

	// The last block start always qualifies: a block holds at most WindowStep pairs.
	start := len(pairs)
	for i := range pairs {
		if blockStart(i) && len(pairs)-i <= MaxHistoryTurns {
			start = i
			break
		}
	}
	for start < len(pairs) && !fits(start) {
		next := start + 1
		for next < len(pairs) && !blockStart(next) {
			next++
		}
		if next == len(pairs) {
			next = start + 1
		}
		start = next
	}
	return start
}
