package thread

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// NoReply is the agent reply of an earlier request in which no card text of the planner was shown.
const NoReply = "no answer: the request ended without a reply"

// Fixed labels of the reply lines (ADR-0022 "Planner sees").
const (
	labelReply    = "reply: "
	labelPlan     = "plan: "
	labelCannotDo = "cannot_do: "
	labelAsked    = "asked: "
	labelActions  = "actions: "

	askedText   = "questions to the student"
	unknownTool = "unknown_tool"
	outcomeOK   = "ok"
	outcomeErr  = "error"
)

// Reply builds the agent reply of an earlier request from its stored entries (ADR-0023 "History pairs"). It holds
// only what the planner itself wrote in card calls that passed argument checks and were not refused (reply.text,
// show_plan.summary, cannot_do.your_part/our_part; ask_questions as a fixed line without the questions), and one line
// with the catalog names of the other calls and ok | error by is_error of their results. Tool results, reader texts,
// titles and file names never get in (ADR-0022). The reply is cut to MaxAgentReplyBytes on a rune boundary.
func Reply(request []values.HistoryTurn) string {
	var lines, actions []string
	for i, e := range request {
		if e.Calls == nil {
			continue
		}
		var results []values.ToolResult
		if i+1 < len(request) {
			results = request[i+1].Results
		}
		for _, raw := range e.Calls.Calls {
			res, answered := resultOf(results, raw.CallID)
			// A card closed unanswered (Abandon: not_done) was shown; any other error means it was refused.
			refused := answered && res.IsError && res != values.RefusalResult(raw.CallID, values.RefuseNotDone)
			if line, ok := cardLine(raw, refused); ok {
				lines = append(lines, line)
				continue
			}
			name := unknownTool
			if t, err := values.ParseTool(raw.Tool); err == nil {
				name = t.String()
			}
			outcome := outcomeErr // a call without a result did not run as far as the planner may assume
			if answered && !res.IsError {
				outcome = outcomeOK
			}
			actions = append(actions, name+" "+outcome)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, NoReply)
	}
	if len(actions) > 0 {
		lines = append(lines, labelActions+strings.Join(actions, ", "))
	}
	return cutRunes(strings.Join(lines, "\n"), values.MaxAgentReplyBytes)
}

// FreezeReply fixes the reply line of the current request once it is final (DONE, FAILED, CANCELLED, STOPPED): Reply
// of its entries among entries, which may hold other requests too. The line is kept forever (ADR-0028 «Retention»)
// and Window uses it instead of the steps, which are purged later. A line already frozen does not change; a request
// that is not final is ErrInvalidTransition. A STOPPED request that goes on loses its line (move) and is frozen again
// when it ends.
func (th *Thread) FreezeReply(entries []values.HistoryEntry, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if !t.final() {
		return fmt.Errorf("%w: reply line of a request that is %s", ErrInvalidTransition, t.status)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if t.replyLine != "" {
		return nil
	}
	var request []values.HistoryTurn
	for _, e := range entries {
		if e.Turn == t.number {
			request = append(request, e.Entry)
		}
	}
	// The request keeps its own end time; only the thread changed.
	t.replyLine, th.updatedAt = Reply(request), now
	return nil
}

// cardLine is the line of a card call the student was shown; a refused card or one with invalid arguments has none.
// A show_plan still waiting for the student has no result and was shown.
func cardLine(raw values.RawCall, refused bool) (string, bool) {
	if refused {
		return "", false
	}
	call, err := values.ParseCall(raw.CallID, raw.Tool, raw.ArgumentsJSON)
	if err != nil {
		return "", false
	}
	args := call.Args()
	switch call.Tool() {
	case values.ToolReply:
		text, _ := args.Text("text")
		return labelReply + oneLine(text), true
	case values.ToolShowPlan:
		summary, _ := args.Text("summary")
		return labelPlan + oneLine(summary), true
	case values.ToolCannotDo:
		var parts []string
		if yours := args.TextList("your_part"); len(yours) > 0 {
			parts = append(parts, "your_part: "+joinTexts(yours))
		}
		if ours := args.TextList("our_part"); len(ours) > 0 {
			parts = append(parts, "our_part: "+joinTexts(ours))
		}
		return labelCannotDo + strings.Join(parts, "; "), true
	case values.ToolAskQuestions:
		return labelAsked + askedText, true
	default:
		return "", false
	}
}

func resultOf(results []values.ToolResult, callID string) (values.ToolResult, bool) {
	for _, r := range results {
		if r.CallID == callID {
			return r, true
		}
	}
	return values.ToolResult{}, false
}

func joinTexts(texts []values.Text) string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = oneLine(t)
	}
	return strings.Join(out, " | ")
}

// oneLine keeps a card text on its line, so the labels stay the only line starts.
func oneLine(t values.Text) string { return strings.ReplaceAll(string(t), "\n", " ") }

// cutRunes returns the longest prefix of s of at most n bytes that ends on a rune boundary.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
