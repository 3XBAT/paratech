package values

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Types of one planner step on the Hub (gateway.proto AgentStep, ADR-0023). They cross the AgentModel port of
// cases/agentturn, so they live here (ARCH-26); the hubclient adapter maps them to the protobuf messages.

// RawCall is a tool call as the Hub returned it: untrusted until ParseCall accepts it.
type RawCall struct {
	CallID        string
	Tool          string
	ArgumentsJSON string
}

// SealedCalls are the calls of one step with the Hub's seal; the desktop echoes them unchanged in the history.
type SealedCalls struct {
	Calls []RawCall
	Seal  []byte
}

// ToolResult is what the desktop reports for one call: JSON built from an allowlist of typed fields (ADR-0022).
type ToolResult struct {
	CallID     string
	ResultJSON string
	IsError    bool
}

// MaxAgentReplyBytes bounds the agent reply of an earlier request (gateway.proto AgentTurn.agent_reply).
const MaxAgentReplyBytes = 2 << 10

// HistoryTurn is one history entry of the thread: exactly one of StudentText, AgentReply, Summary, Calls or Results
// is set. AgentReply is never stored as a turn: thread.Window takes it from the frozen reply line of an earlier request
// or builds it from its stored entries. Summary is the HistorySummary text, only ever the first turn of a window.
type HistoryTurn struct {
	StudentText string
	AgentReply  string
	Summary     string
	Calls       *SealedCalls
	Results     []ToolResult
}

// Validate checks that exactly one part of the entry is set and that it is within the Hub limits.
func (h HistoryTurn) Validate() error {
	set := 0
	if h.Summary != "" {
		set++
		if err := checkSummaryText(h.Summary); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidResult, err)
		}
	}
	if h.StudentText != "" {
		set++
		if len(h.StudentText) > MaxStudentTextBytes {
			return fmt.Errorf("%w: student text longer than %d bytes", ErrInvalidResult, MaxStudentTextBytes)
		}
	}
	if h.AgentReply != "" {
		set++
		if len(h.AgentReply) > MaxAgentReplyBytes || !utf8.ValidString(h.AgentReply) ||
			strings.ContainsRune(h.AgentReply, 0) || strings.TrimSpace(h.AgentReply) == "" {
			return fmt.Errorf("%w: an agent reply is 1..%d bytes of UTF-8 without NUL", ErrInvalidResult, MaxAgentReplyBytes)
		}
	}
	if h.Calls != nil {
		set++
		if len(h.Calls.Calls) == 0 || len(h.Calls.Seal) == 0 {
			return fmt.Errorf("%w: an echo needs calls and a seal", ErrInvalidResult)
		}
	}
	if len(h.Results) > 0 {
		set++
		for _, r := range h.Results {
			if err := r.Validate(); err != nil {
				return err
			}
		}
	}
	if set != 1 {
		return fmt.Errorf("%w: a history entry holds exactly one of text, reply, summary, calls and results", ErrInvalidResult)
	}
	return nil
}

// Size returns the bytes of the entry that count toward the history window.
func (h HistoryTurn) Size() int {
	n := len(h.StudentText) + len(h.AgentReply) + len(h.Summary)
	if h.Calls != nil {
		n += len(h.Calls.Seal)
		for _, c := range h.Calls.Calls {
			n += len(c.CallID) + len(c.Tool) + len(c.ArgumentsJSON)
		}
	}
	for _, r := range h.Results {
		n += len(r.CallID) + len(r.ResultJSON)
	}
	return n
}

// HistoryEntry is a stored history entry with the number of the request it belongs to. DroppedBy is the number of the
// request that restarted the chat from an earlier message (ADR-0028), 0 when the request is not dropped; the window
// skips dropped requests. ReplyLine is the reply line frozen when the request became final (thread.FreezeReply), set
// by the store on the entries of its request; empty for a request that is not final or ended before the line existed.
// Mentions are the IDs of the objects the request mentioned (ADR-0028), also set by the store on every entry of its
// request; labels stay in the store.
type HistoryEntry struct {
	Turn      int
	Entry     HistoryTurn
	DroppedBy int
	ReplyLine string
	Mentions  []Ref
}

// StepRequest is one planner step: the key ag:<thread>:<turn>:<step>, the shared base, the current scope, the mentions
// of the step (thread.StepMentions, IDs only) and the history window of this thread only.
type StepRequest struct {
	Key             string
	Thread          shared.ThreadID
	Base            Base
	Scope           Scope
	Mentions        []StepMention
	History         []HistoryTurn
	MaxOutputTokens int
}

// MaxReplyCalls bounds the calls of a reply the desktop records; over the budget of 4 the request stops anyway.
const MaxReplyCalls = 8

// Validate checks the shape of a reply before anything of it is recorded: 1..MaxReplyCalls calls with well-formed,
// distinct call IDs, a seal, and non-negative usage. The calls themselves are parsed one by one with ParseCall.
func (r StepReply) Validate() error {
	if len(r.Calls.Calls) == 0 || len(r.Calls.Calls) > MaxReplyCalls || len(r.Calls.Seal) == 0 {
		return fmt.Errorf("%w: a reply has 1..%d sealed calls", ErrInvalidResult, MaxReplyCalls)
	}
	if r.InputTokens < 0 || r.OutputTokens < 0 {
		return fmt.Errorf("%w: negative usage", ErrInvalidResult)
	}
	ids := map[string]bool{}
	for _, c := range r.Calls.Calls {
		if err := checkCallID(c.CallID); err != nil || ids[c.CallID] {
			return fmt.Errorf("%w: call ids must be distinct and well formed", ErrInvalidResult)
		}
		ids[c.CallID] = true
	}
	return nil
}

// StepReply is the Hub's answer: 1..4 sealed calls and the usage the budget counts.
type StepReply struct {
	Calls        SealedCalls
	InputTokens  int
	OutputTokens int
	Replayed     bool // the stored result of an earlier call with the same key (free)
}
