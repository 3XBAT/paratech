package anthropic

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"paratech/hub/internal/domain/llm/values"
)

// Tool use values of the Messages API (docs/refs/anthropic.md section 7).
const (
	toolChoiceAny     = "any"
	cacheEphemeral    = "ephemeral"
	blockText         = "text"
	blockToolUse      = "tool_use"
	blockToolResult   = "tool_result"
	stopReasonToolUse = "tool_use"
	stopReasonRefusal = "refusal"
	roleUser          = "user"
	roleAssistant     = "assistant"
)

type agentWireRequest struct {
	Model      string             `json:"model"`
	MaxTokens  int64              `json:"max_tokens"`
	System     []agentWireBlock   `json:"system"`
	Messages   []agentWireMessage `json:"messages"`
	Tools      []wireTool         `json:"tools"`
	ToolChoice wireToolChoice     `json:"tool_choice"`
}

type agentWireMessage struct {
	Role    string           `json:"role"`
	Content []agentWireBlock `json:"content"`
}

// agentWireBlock is a text, tool_use or tool_result block; the fields of the other kinds stay empty and are omitted.
type agentWireBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	ToolUseID    string            `json:"tool_use_id,omitempty"`
	Content      string            `json:"content,omitempty"`
	IsError      bool              `json:"is_error,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireCacheControl struct {
	Type string `json:"type"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type wireToolChoice struct {
	Type string `json:"type"`
}

// newAgentWireRequest lays out a planner call so the stable part is a cacheable prefix: tools, then the server prompt
// and the shared student context (cache breakpoint), then the thread scope; the history follows with a breakpoint on
// its last block, so the next step of the same request reads it from the cache. Student texts and tool results go
// only into user turns, never into system.
func newAgentWireRequest(call values.AgentCall) agentWireRequest {
	tools := make([]wireTool, 0, len(call.Tools()))
	for _, t := range call.Tools() {
		tools = append(tools, wireTool{Name: t.Name(), Description: t.Description(), InputSchema: json.RawMessage(t.Schema())})
	}
	return agentWireRequest{
		Model:     call.Model(),
		MaxTokens: call.MaxOutput(),
		System: []agentWireBlock{
			{Type: blockText, Text: call.System()},
			{Type: blockText, Text: call.Base(), CacheControl: &wireCacheControl{Type: cacheEphemeral}},
			{Type: blockText, Text: call.Scope()},
		},
		Messages:   agentMessages(call.Turns()),
		Tools:      tools,
		ToolChoice: wireToolChoice{Type: toolChoiceAny},
	}
}

// agentMessages maps the turns to messages: student text and tool results are user turns, echoed calls the assistant
// turn with tool_use blocks. Consecutive turns of one role share a message (results first, as the API wants tool
// results at the start of a user message). The last block carries the second cache breakpoint.
func agentMessages(turns []values.Turn) []agentWireMessage {
	var msgs []agentWireMessage
	add := func(role string, blocks ...agentWireBlock) {
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content = append(msgs[n-1].Content, blocks...)
			return
		}
		msgs = append(msgs, agentWireMessage{Role: role, Content: blocks})
	}
	for _, t := range turns {
		switch t.Kind() {
		case values.TurnStudentText:
			add(roleUser, agentWireBlock{Type: blockText, Text: t.Text()})
		case values.TurnCalls:
			for _, c := range t.Calls() {
				add(roleAssistant, agentWireBlock{Type: blockToolUse, ID: c.CallID(), Name: c.Tool(), Input: json.RawMessage(c.Arguments())})
			}
		case values.TurnResults:
			for _, r := range t.Results() {
				add(roleUser, agentWireBlock{Type: blockToolResult, ToolUseID: r.CallID(), Content: r.JSON(), IsError: r.IsError()})
			}
		}
	}
	if n := len(msgs); n > 0 {
		last := &msgs[n-1].Content[len(msgs[n-1].Content)-1]
		last.CacheControl = &wireCacheControl{Type: cacheEphemeral}
	}
	return msgs
}

type agentWireResponse struct {
	Type       string               `json:"type"`
	Content    []agentWireBlockRead `json:"content"`
	StopReason *string              `json:"stop_reason"`
	Usage      *wireUsage           `json:"usage"`
}

type agentWireBlockRead struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// readAgentAnswer decodes a 200 answer to a planner call. Text and thinking blocks are ignored; every tool_use block
// becomes a call with its input re-encoded canonically (sorted keys, no duplicate keys), so the arguments the Hub
// checks, seals and returns are one exact string. A block that is not a valid call makes a malformed answer and a
// refusal a refused one, both with their usage; a body without usage is a permanent error.
func readAgentAnswer(resp *http.Response) (values.AgentAnswer, error) {
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != contentTypeJSON {
		return values.AgentAnswer{}, permanent("unexpected content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return values.AgentAnswer{}, errRead
	}
	if len(body) > maxResponseBytes {
		return values.AgentAnswer{}, permanent("response too large")
	}
	var r agentWireResponse
	if err := json.Unmarshal(body, &r); err != nil || r.Type != responseTypeMessage || r.Usage == nil {
		return values.AgentAnswer{}, permanent("malformed response")
	}
	usage, ok := r.Usage.toUsage()
	if !ok {
		return values.AgentAnswer{}, permanent("malformed usage")
	}
	if r.StopReason != nil && *r.StopReason == stopReasonRefusal {
		return values.RefusedAgentAnswer(usage), nil
	}
	var calls []values.ToolCall
	for _, b := range r.Content {
		if b.Type != blockToolUse {
			continue
		}
		// A bad call is the model's output, billed by the provider: an answer with usage, so the Hub charges it.
		args, ok := canonicalObject(b.Input)
		if !ok {
			return values.MalformedAgentAnswer(usage), nil
		}
		call, err := values.NewToolCall(b.ID, b.Name, args)
		if err != nil {
			return values.MalformedAgentAnswer(usage), nil
		}
		calls = append(calls, call)
	}
	toolUse := r.StopReason != nil && *r.StopReason == stopReasonToolUse
	return values.NewAgentAnswer(calls, usage, toolUse), nil
}

// canonicalObject re-encodes a JSON object with sorted keys, exact numbers and no HTML escaping.
func canonicalObject(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil || v == nil || dec.More() {
		return "", false
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return string(bytes.TrimRight(out.Bytes(), "\n")), true
}
