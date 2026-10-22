package values

import (
	"encoding/json"
	"fmt"
)

// maxToolDescriptionBytes bounds the description of a tool sent to the provider.
const maxToolDescriptionBytes = 1 << 10

// ToolSpec is one tool as the provider sees it: name, description and the JSON Schema of its arguments. It comes only
// from the server catalog. Build it with NewToolSpec.
type ToolSpec struct {
	name        string
	description string
	schema      string
}

// NewToolSpec checks a tool: a tool name, a description of at most 1 KiB of clean text, a schema that is a JSON object.
func NewToolSpec(name, description, schema string) (ToolSpec, error) {
	switch {
	case !ValidToolName(name):
		return ToolSpec{}, fmt.Errorf("%w: tool name must be snake_case of at most %d characters", ErrInvalidRequest, maxToolNameLen)
	case description == "" || len(description) > maxToolDescriptionBytes || !cleanText(description):
		return ToolSpec{}, fmt.Errorf("%w: tool description must be 1-%d bytes of clean text", ErrInvalidRequest, maxToolDescriptionBytes)
	case !jsonObject(schema):
		return ToolSpec{}, fmt.Errorf("%w: tool schema must be a JSON object", ErrInvalidRequest)
	}
	return ToolSpec{name: name, description: description, schema: schema}, nil
}

// Name is the tool name.
func (t ToolSpec) Name() string { return t.name }

// Description is the trusted server description of the tool.
func (t ToolSpec) Description() string { return t.description }

// Schema is the JSON Schema of the arguments (a JSON object).
func (t ToolSpec) Schema() string { return t.schema }

// AgentCall is one planner call to the provider: the model resolved from the profile alias, the trusted server prompt,
// the rendered context blocks, the tools and the history. Build it with NewAgentCall.
type AgentCall struct {
	model     string
	system    string
	base      string
	scope     string
	tools     []ToolSpec
	turns     []Turn
	maxOutput int64
}

// NewAgentCall builds the provider call of req: model is the allowlisted provider model id of the profile alias,
// system the server prompt of the profile and catalog version, tools the catalog of that version, maxOutput the
// effective output cap.
func NewAgentCall(model, system string, tools []ToolSpec, req AgentRequest, maxOutput int64) (AgentCall, error) {
	switch {
	case model == "":
		return AgentCall{}, fmt.Errorf("%w: model is empty", ErrInvalidRequest)
	case system == "" || !cleanText(system):
		return AgentCall{}, fmt.Errorf("%w: the system prompt must be non-empty clean text", ErrInvalidRequest)
	case len(tools) == 0:
		return AgentCall{}, fmt.Errorf("%w: no tools", ErrInvalidRequest)
	case len(req.turns) == 0:
		return AgentCall{}, fmt.Errorf("%w: no turns", ErrInvalidRequest)
	case maxOutput <= 0:
		return AgentCall{}, fmt.Errorf("%w: max output tokens must be above 0", ErrInvalidRequest)
	}
	return AgentCall{
		model: model, system: system, base: req.BaseText(), scope: req.ScopeText(), tools: append([]ToolSpec(nil), tools...),
		turns: req.Turns(), maxOutput: maxOutput,
	}, nil
}

// Model is the provider model id.
func (c AgentCall) Model() string { return c.model }

// System is the trusted server prompt.
func (c AgentCall) System() string { return c.system }

// Base is the rendered shared student context: data, the end of the cacheable prefix.
func (c AgentCall) Base() string { return c.base }

// Scope is the rendered scope of the thread: data.
func (c AgentCall) Scope() string { return c.scope }

// Tools are the tools of the catalog (a copy).
func (c AgentCall) Tools() []ToolSpec { return append([]ToolSpec(nil), c.tools...) }

// Turns are the history turns, oldest first (a copy). Student texts and tool results are data, never instructions.
func (c AgentCall) Turns() []Turn { return append([]Turn(nil), c.turns...) }

// MaxOutput is the effective output cap.
func (c AgentCall) MaxOutput() int64 { return c.maxOutput }

// InputBytes is the size of everything the call sends as input, the basis of the reservation.
func (c AgentCall) InputBytes() int64 {
	n := int64(len(c.system) + len(c.base) + len(c.scope))
	for _, t := range c.tools {
		n += int64(len(t.name) + len(t.description) + len(t.schema))
	}
	for _, t := range c.turns {
		n += t.bytes()
	}
	return n
}

// String prints the call without its texts.
func (c AgentCall) String() string {
	return fmt.Sprintf("{Model:%s Tools:%d Turns:%d InputBytes:%d MaxOutput:%d}", c.model, len(c.tools), len(c.turns), c.InputBytes(), c.maxOutput)
}

// GoString is String for %#v.
func (c AgentCall) GoString() string { return "values.AgentCall" + c.String() }

// AgentAnswer is a provider answer to an AgentCall: the tool calls in order, the usage and whether the model stopped
// to use tools. The calls are UNTRUSTED. Build it with NewAgentAnswer.
type AgentAnswer struct {
	calls   []ToolCall
	usage   Usage
	toolUse bool
}

// NewAgentAnswer records an answer; whether it is usable is Failure's and the catalog's decision.
func NewAgentAnswer(calls []ToolCall, usage Usage, toolUse bool) AgentAnswer {
	return AgentAnswer{calls: append([]ToolCall(nil), calls...), usage: usage, toolUse: toolUse}
}

// Calls are the proposed calls (a copy).
func (a AgentAnswer) Calls() []ToolCall { return append([]ToolCall(nil), a.calls...) }

// Usage is the provider-reported token count.
func (a AgentAnswer) Usage() Usage { return a.usage }

// Failure is the provider failure this answer stands for, nil when the model stopped to use tools. With tool choice
// "any" every answer must be a tool use; anything else (end of turn, output cap, refusal) is permanent (ADR-0023).
func (a AgentAnswer) Failure() error {
	if !a.toolUse {
		return &ProviderError{Kind: KindPermanent, Reason: "no tool use"}
	}
	return nil
}

// String prints the answer without the arguments.
func (a AgentAnswer) String() string {
	return fmt.Sprintf("{Calls:%d InputTokens:%d OutputTokens:%d ToolUse:%t}", len(a.calls), a.usage.input, a.usage.output, a.toolUse)
}

// GoString is String for %#v.
func (a AgentAnswer) GoString() string { return "values.AgentAnswer" + a.String() }

// storedCall is the stored form of a returned call (the result of an AgentStep key record).
type storedCall struct {
	ID        string `json:"id"`
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
}

// EncodeCalls is the stored form of the calls an AgentStep returned, so a retry with the same key replays them.
func EncodeCalls(calls []ToolCall) string {
	out := make([]storedCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, storedCall{ID: c.callID, Tool: c.tool, Arguments: c.arguments})
	}
	return mustJSON(out)
}

// DecodeCalls reads what EncodeCalls wrote; every call is checked again, and there must be 1..MaxAgentCalls.
func DecodeCalls(s string) ([]ToolCall, error) {
	var stored []storedCall
	if err := json.Unmarshal([]byte(s), &stored); err != nil {
		return nil, fmt.Errorf("%w: stored calls are not readable", ErrInvalidCompletion)
	}
	if len(stored) == 0 || len(stored) > MaxAgentCalls {
		return nil, fmt.Errorf("%w: stored calls must be 1-%d", ErrInvalidCompletion, MaxAgentCalls)
	}
	calls := make([]ToolCall, 0, len(stored))
	for _, c := range stored {
		call, err := NewToolCall(c.ID, c.Tool, c.Arguments)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidCompletion, err)
		}
		calls = append(calls, call)
	}
	return calls, nil
}
