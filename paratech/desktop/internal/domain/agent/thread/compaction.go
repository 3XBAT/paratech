package thread

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/agent/values"
)

// CompactionRequest decides whether the chat's next request should first compact the chat (CompactionPlan of request
// turns+1, as NextFill) and builds the summary request if so: the previous summary and the requests from…through as
// Window groups them (dropped ones skipped; the student's texts of a request joined by a line break; the reply its
// frozen reply line, else Reply of its entries). entries are the stored entries read from the first request with the
// step's limit, oldest first.
//
// Only requests from HistoryFrom on count, as only those are sent: a request older than an expired-request restart is
// neither planned nor summarized (the agent no longer sees it), and once a later summary covers its number it is no
// longer an Overflow either. A request that is not over is never summarized (Compact refuses it), and a running one or
// one with an open step is ErrTurnInProgress (also ErrInvalidTransition): no Hub call during a step.
//
// The texts are shortened to the Hub limits here: each to its byte limit on a rune boundary (the beginning kept), then
// the longest parts again until the encoded data fits; a pair whose student text is blank is left out. No pair left
// means nothing to do. The summary text never enters the provenance check.
func (th *Thread) CompactionRequest(entries []values.HistoryEntry) (values.SummaryRequest, bool, error) {
	t := th.current
	if t == nil {
		return values.SummaryRequest{}, false, nil // an empty chat has nothing to compact
	}
	if t.status.stepping() || (t.stepOpen && !t.final()) {
		return values.SummaryRequest{}, false, fmt.Errorf("%w: %w: %s", ErrTurnInProgress, ErrInvalidTransition, t.status)
	}

	next := th.turns + 1
	sent := entries[currentIndex(entries, th.historyFrom):]
	from, through, ok := CompactionPlan(sent, next, th.summary)
	if !ok {
		return values.SummaryRequest{}, false, nil
	}
	through = min(through, lastOver(t))

	var pairs []values.SummaryPair
	for _, p := range earlierPairs(sent[:currentIndex(sent, next)], th.summary.Through()) {
		if p.number < from || p.number > through {
			continue
		}
		pairs = append(pairs, summaryPairOf(p))
	}
	pairs = fitSummary(th.summary, pairs)
	if len(pairs) == 0 {
		return values.SummaryRequest{}, false, nil
	}
	req, err := values.NewSummaryRequest(th.id, th.summary, through, pairs)
	if err != nil {
		return values.SummaryRequest{}, false, err
	}
	return req, true, nil
}

// summaryPairOf is the pair p of Window as summary data, each part cut to its limit.
func summaryPairOf(p pair) values.SummaryPair {
	var texts []string
	reply := ""
	for _, h := range p.turns {
		if h.StudentText != "" {
			texts = append(texts, h.StudentText)
		}
		if h.AgentReply != "" {
			reply = h.AgentReply
		}
	}
	return values.SummaryPair{
		Student: cleanPart(strings.Join(texts, "\n"), values.MaxSummaryStudentBytes),
		Reply:   cleanPart(reply, values.MaxSummaryReplyBytes),
	}
}

// cleanPart keeps valid UTF-8 without NUL (the Hub refuses NUL), cut to limit bytes on a rune boundary, trimmed.
func cleanPart(s string, limit int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", "")
	return strings.TrimSpace(cutRunes(s, limit))
}

// fitSummary cuts the longest part (student text or reply) until the encoded data fits MaxSummaryDataBytes, at most by
// half of it at a time so the cuts spread over the long parts, then leaves out the pairs whose student text is blank.
func fitSummary(previous values.HistorySummary, pairs []values.SummaryPair) []values.SummaryPair {
	for {
		over := values.SummaryEncodedSize(previous, pairs) - values.MaxSummaryDataBytes
		if over <= 0 {
			break
		}
		part := longestPart(pairs)
		if part == nil || *part == "" {
			break // only the previous summary is left; NewSummaryRequest refuses it
		}
		*part = strings.TrimSpace(cutRunes(*part, max(len(*part)-over, len(*part)/2)))
	}
	kept := pairs[:0]
	for _, p := range pairs {
		if p.Student != "" {
			kept = append(kept, p)
		}
	}
	return kept
}

// longestPart points to the longest text among the pairs, the first one on a tie; nil without pairs.
func longestPart(pairs []values.SummaryPair) *string {
	var out *string
	for i := range pairs {
		for _, s := range []*string{&pairs[i].Student, &pairs[i].Reply} {
			if out == nil || len(*s) > len(*out) {
				out = s
			}
		}
	}
	return out
}
