package thread

import (
	"slices"

	"paratech/desktop/internal/domain/agent/values"
)

// History window sent to the planner, this chat only (ADR-0023, ADR-0028): at most MaxHistoryTurns earlier requests
// whose pairs hold at most MaxHistoryBytes, and at most MaxRequestTurns turns in the whole step request (the Hub
// limit). The window start moves in steps of WindowStep requests, aligned on the request number counted from the first
// request after the summary, so the history prefix stays byte-identical for up to WindowStep requests in a row (prompt
// cache). A compaction summarizes the oldest CompactPairs pairs after the summary (two blocks).
const (
	MaxHistoryTurns = 20
	MaxHistoryBytes = 48 << 10
	MaxRequestTurns = 64
	WindowStep      = 5
	CompactPairs    = 2 * WindowStep
)

// Window picks the history sent with a step from the stored entries of this chat, oldest first: the summary turn of
// requests 1…summary.Through (if any), then earlier requests after it as pairs, the student's texts then one agent
// reply (ADR-0023 "History pairs": the frozen reply line of the request, or Reply of its entries for a request that
// has none), then every entry of the current request. A pair is the unit of the cut; dropped requests and requests the
// summary covers are skipped. The first pair sent is the first of a block of WindowStep request numbers counted from
// summary.Through+1 (1–5, 6–10, … without a summary): the oldest block start from which at most MaxHistoryTurns pairs
// follow, so 16–20 earlier answers go when there are enough. When the summary and the pairs exceed MaxHistoryBytes or
// the request MaxRequestTurns, the start moves one more block, and past the last block start pair by pair. Echoes of
// earlier requests are never sent: the Hub keeps the model turn of an echo only for a while, and it looks it up for
// the last echo of the window, which must therefore belong to the current request. Every earlier request sent has its
// reply, because the Hub merges consecutive texts once any reply is present. The current request is always sent
// whole; the summary and the pairs only before its text (a summary turn is always followed by a student text).
func Window(entries []values.HistoryEntry, current int, summary values.HistorySummary) []values.HistoryTurn {
	w := layout(entries, current, summary)
	out := make([]values.HistoryTurn, 0, len(w.head)+len(w.now)+2*len(w.pairs))
	out = append(out, w.head...)
	for _, p := range w.pairs {
		out = append(out, p.turns...)
	}
	for _, e := range w.now {
		out = append(out, e.Entry)
	}
	return out
}

// sent is what a window carries: the summary turn (if any), the earlier pairs and the entries of the current request.
type sent struct {
	head  []values.HistoryTurn
	pairs []pair
	now   []values.HistoryEntry
}

// layout selects the window of Window; StepMentions reads the same selection, so the two never disagree.
func layout(entries []values.HistoryEntry, current int, summary values.HistorySummary) sent {
	cur := currentIndex(entries, current)
	now := entries[cur:]

	// A reply stands between two student texts; without the current text first no pair may precede it.
	var pairs []pair
	var head []values.HistoryTurn
	if len(now) > 0 && now[0].Entry.StudentText != "" {
		pairs = earlierPairs(entries[:cur], summary.Through())
		if !summary.IsZero() && len(now)+1 <= MaxRequestTurns {
			head = append(head, summary.Turn())
		}
	}
	start := windowStart(pairs, summary.Through(), len(now)+len(head), sizeOf(head))
	return sent{head: head, pairs: pairs[start:], now: now}
}

// ContextFill is how much of the history window the next request of a chat would use (ADR-0028 «What context used
// means»): the earlier pairs it carries, their bytes with the summary's and the turns with the summary and its own text.
type ContextFill struct {
	Pairs int
	Bytes int
	Turns int
	// Share is max(Pairs/MaxHistoryTurns, Bytes/MaxHistoryBytes, Turns/MaxRequestTurns), from 0 to 1.
	Share float64
	// Overflow: an earlier request that is not dropped is neither sent nor covered by the summary.
	Overflow bool
}

// Fill is the window the next request would carry: what Window sends before request current when that request is its
// text alone. entries are the stored entries of the chat before current; they should reach back past the window
// (Overflow sees only the requests among them).
func Fill(entries []values.HistoryEntry, current int, summary values.HistorySummary) ContextFill {
	earlier := entries[:currentIndex(entries, current)]
	pairs := earlierPairs(earlier, summary.Through())

	f := ContextFill{Turns: 1}
	if !summary.IsZero() {
		f.Turns++
		f.Bytes = summary.Turn().Size()
	}
	start := windowStart(pairs, summary.Through(), f.Turns, f.Bytes)
	f.Pairs = len(pairs) - start
	for _, p := range pairs[start:] {
		f.Bytes += p.size
		f.Turns += len(p.turns)
	}
	f.Share = max(float64(f.Pairs)/MaxHistoryTurns, float64(f.Bytes)/MaxHistoryBytes, float64(f.Turns)/MaxRequestTurns)

	live := map[int]bool{}
	for _, e := range earlier {
		if e.DroppedBy == 0 && e.Turn > summary.Through() {
			live[e.Turn] = true
		}
	}
	f.Overflow = len(live) > f.Pairs
	return f
}

// CompactionPlan tells whether the next request current should first compact the chat (ADR-0028 «Context
// compaction»): when its window would be full (Fill: Share ≥ 1 or Overflow). Then the requests from…through (the
// oldest CompactPairs pairs after the summary, fewer if fewer; dropped requests are not among them) and the previous
// summary become the new summary of 1…through. The pairs are grouped as Window groups them.
func CompactionPlan(entries []values.HistoryEntry, current int, summary values.HistorySummary) (from, through int, ok bool) {
	if f := Fill(entries, current, summary); f.Share < 1 && !f.Overflow {
		return 0, 0, false
	}
	pairs := earlierPairs(entries[:currentIndex(entries, current)], summary.Through())
	if len(pairs) == 0 {
		return 0, 0, false
	}
	n := min(CompactPairs, len(pairs))
	return pairs[0].number, pairs[n-1].number, true
}

// currentIndex is the index of the first entry of request current or a later one.
func currentIndex(entries []values.HistoryEntry, current int) int {
	cur := slices.IndexFunc(entries, func(e values.HistoryEntry) bool { return e.Turn >= current })
	if cur < 0 {
		return len(entries)
	}
	return cur
}

func sizeOf(turns []values.HistoryTurn) int {
	n := 0
	for _, h := range turns {
		n += h.Size()
	}
	return n
}

// pair is an earlier request as sent: its number, its student texts and the reply, and their bytes; mentions are the
// IDs the request mentioned.
type pair struct {
	number   int
	turns    []values.HistoryTurn
	size     int
	mentions []values.Ref
}

// earlierPairs groups the entries of earlier requests after request covered (the summary's Through) by request: its
// student texts in order, then its reply. A request whose texts are not among the entries (cut off by the read limit)
// has no pair, nor has a dropped one.
func earlierPairs(entries []values.HistoryEntry, covered int) []pair {
	var pairs []pair
	for i := 0; i < len(entries); {
		j := i
		for j < len(entries) && entries[j].Turn == entries[i].Turn {
			j++
		}
		if entries[i].DroppedBy != 0 || entries[i].Turn <= covered {
			i = j
			continue
		}
		request := make([]values.HistoryTurn, 0, j-i)
		p := pair{number: entries[i].Turn}
		line := ""
		for _, e := range entries[i:j] {
			request = append(request, e.Entry)
			for _, r := range e.Mentions {
				if !slices.Contains(p.mentions, r) {
					p.mentions = append(p.mentions, r)
				}
			}
			if e.Entry.StudentText != "" {
				p.turns = append(p.turns, values.HistoryTurn{StudentText: e.Entry.StudentText})
			}
			if line == "" {
				line = e.ReplyLine
			}
		}
		if len(p.turns) > 0 {
			if line == "" {
				line = Reply(request) // a request that ended before reply lines were frozen
			}
			p.turns = append(p.turns, values.HistoryTurn{AgentReply: line})
			p.size = sizeOf(p.turns)
			pairs = append(pairs, p)
		}
		i = j
	}
	return pairs
}

// windowStart returns the index of the oldest pair sent, given the pairs after request covered and the turns and bytes
// that go anyway (the summary, the current request): len(pairs) sends none.
func windowStart(pairs []pair, covered, fixedTurns, fixedBytes int) int {
	block := func(i int) int { return (pairs[i].number - covered - 1) / WindowStep }
	blockStart := func(i int) bool { return i == 0 || block(i) != block(i-1) }
	fits := func(start int) bool {
		bytes, turns := fixedBytes, fixedTurns
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
