package agentchats

import (
	"strings"

	"paratech/desktop/internal/domain/agent/values"
)

// Captions of the reply line, worded as the panel words its cards.
const (
	captionPlan     = "Понял так: "
	captionCannotDo = "Целиком не получится."
	captionYours    = "Остаётся на вашей стороне: "
	captionOurs     = "Берёт на себя paratech: "
	lineAsked       = "Были уточняющие вопросы."
)

// request is the stored history of one request: its number, the student's text and its entries in order.
type request struct {
	turn    int
	text    string
	entries []values.HistoryTurn
}

// requestsOf groups history entries by request; a request whose text was not read (the oldest one of a cut read) is
// left out.
func requestsOf(entries []values.HistoryEntry) []request {
	var out []request
	for _, e := range entries {
		if n := len(out); n == 0 || out[n-1].turn != e.Turn {
			out = append(out, request{turn: e.Turn})
		}
		r := &out[len(out)-1]
		if e.Entry.StudentText != "" && r.text == "" {
			r.text = e.Entry.StudentText
		}
		r.entries = append(r.entries, e.Entry)
	}
	kept := out[:0]
	for _, r := range out {
		if r.text != "" {
			kept = append(kept, r)
		}
	}
	return kept
}

// studentText is the text the student typed: the line of upload IDs that values.NewStudentText adds for the planner
// is not shown.
func studentText(stored string) string {
	i := strings.LastIndexByte(stored, '\n')
	last := stored[i+1:]
	if strings.HasPrefix(last, "["+string(values.KindUpload)+":") && strings.HasSuffix(last, "]") {
		if i < 0 {
			return ""
		}
		return stored[:i]
	}
	return stored
}

// replyLine is what the student was shown of a request, one line per card, following thread.Reply (ADR-0023): only
// the texts the planner wrote in card calls that passed the argument checks and were not refused (reply.text,
// show_plan.summary, cannot_do parts; ask_questions as a fixed line without the questions). Tool results, file names
// and other calls never get in. Empty when no card was shown.
func replyLine(request []values.HistoryTurn) string {
	var lines []string
	for i, e := range request {
		if e.Calls == nil {
			continue
		}
		var results []values.ToolResult
		if i+1 < len(request) {
			results = request[i+1].Results
		}
		for _, raw := range e.Calls.Calls {
			if line, ok := cardLine(raw, results); ok {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// cardLine is the line of a card call the student was shown. A card closed unanswered (not_done) was shown; any other
// error result means it was refused. A show_plan still waiting has no result and was shown.
func cardLine(raw values.RawCall, results []values.ToolResult) (string, bool) {
	for _, r := range results {
		if r.CallID == raw.CallID && r.IsError && r != values.RefusalResult(raw.CallID, values.RefuseNotDone) {
			return "", false
		}
	}
	call, err := values.ParseCall(raw.CallID, raw.Tool, raw.ArgumentsJSON)
	if err != nil {
		return "", false
	}
	args := call.Args()
	switch call.Tool() {
	case values.ToolReply:
		text, _ := args.Text("text")
		return string(text), text != ""
	case values.ToolShowPlan:
		summary, _ := args.Text("summary")
		return captionPlan + string(summary), true
	case values.ToolCannotDo:
		parts := []string{captionCannotDo}
		if yours := args.TextList("your_part"); len(yours) > 0 {
			parts = append(parts, captionYours+joinTexts(yours))
		}
		if ours := args.TextList("our_part"); len(ours) > 0 {
			parts = append(parts, captionOurs+joinTexts(ours))
		}
		return strings.Join(parts, " "), true
	case values.ToolAskQuestions:
		return lineAsked, true
	default:
		return "", false
	}
}

func joinTexts(texts []values.Text) string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = string(t)
	}
	return strings.Join(out, "; ")
}
