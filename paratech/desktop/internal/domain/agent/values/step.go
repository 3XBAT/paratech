package values

import "paratech/desktop/internal/domain/shared"

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

// HistoryTurn is one history entry of the thread: exactly one of StudentText, Calls or Results is set.
type HistoryTurn struct {
	StudentText string
	Calls       *SealedCalls
	Results     []ToolResult
}

// StepRequest is one planner step: the key ag:<thread>:<turn>:<step>, the shared base, the current scope and the
// history window of this thread only.
type StepRequest struct {
	Key             string
	Thread          shared.ThreadID
	Base            Base
	Scope           Scope
	History         []HistoryTurn
	MaxOutputTokens int
}

// StepReply is the Hub's answer: 1..4 sealed calls and the usage the budget counts.
type StepReply struct {
	Calls        SealedCalls
	InputTokens  int
	OutputTokens int
	Replayed     bool // the stored result of an earlier call with the same key (free)
}
