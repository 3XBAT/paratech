package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

const agentSystem = "System prompt of the planner."

func agentCall(t *testing.T, turns ...values.Turn) values.AgentCall {
	t.Helper()
	base, err := values.NewAgentBase([]values.Subject{{ID: "subj-1", Name: "Матан"}}, "2026-10-22", "Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewAgentScope("subj-1", values.ScreenToday, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) == 0 {
		text, err := values.StudentTurn("student-text-marker")
		if err != nil {
			t.Fatal(err)
		}
		turns = []values.Turn{text}
	}
	req, err := values.NewAgentRequest("k", "th", values.ProfilePanel, 1, base, scope, turns, 0)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := values.NewToolSpec("reply", "Card shown to the student.", `{"type":"object","properties":{"text":{"type":"string"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	call, err := values.NewAgentCall("claude-haiku-5-5", agentSystem, []values.ToolSpec{tool}, req, 400)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

// recorder is a fake Messages API that records request bodies and answers with a fixed response.
type recorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (r *recorder) serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, b)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *recorder) last(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var m map[string]any
	if err := json.Unmarshal(r.bodies[len(r.bodies)-1], &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const toolUseJSON = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5",
	"content":[{"type":"thinking","thinking":"...","signature":"x"},{"type":"text","text":"ignored"},
		{"type":"tool_use","id":"toolu_01A","name":"find_lessons","input":{"subject":"subj-1","kind":"lecture","n":1.50}},
		{"type":"tool_use","id":"toolu_01B","name":"reply","input":{"text":"a <b> & c"}}],
	"stop_reason":"tool_use","stop_sequence":null,
	"usage":{"input_tokens":20,"output_tokens":7,"cache_creation_input_tokens":1000,"cache_read_input_tokens":3000}}`

func TestStepParsesToolUse(t *testing.T) {
	var rec recorder
	srv := rec.serve(t, http.StatusOK, toolUseJSON)
	answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
	if err != nil {
		t.Fatal(err)
	}
	if answer.Failure() != nil {
		t.Fatalf("failure = %v", answer.Failure())
	}
	calls := answer.Calls()
	if len(calls) != 2 || calls[0].CallID() != "toolu_01A" || calls[0].Tool() != "find_lessons" ||
		calls[0].Arguments() != `{"kind":"lecture","n":1.50,"subject":"subj-1"}` || calls[1].Arguments() != `{"text":"a <b> & c"}` {
		t.Fatalf("calls = %v (%s, %s)", calls, calls[0].Arguments(), calls[1].Arguments())
	}
	// Settled as input + cache writes + cache reads, and output (docs/refs/anthropic.md section 3).
	if u := answer.Usage(); u.Input() != 4020 || u.Output() != 7 {
		t.Fatalf("usage = %d/%d", u.Input(), u.Output())
	}
}

func TestStepRequestShape(t *testing.T) {
	ok := func(turn values.Turn, err error) values.Turn {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return turn
	}
	c1, _ := values.NewToolCall("toolu_1", "find_lessons", `{"subject":"subj-1"}`)
	c2, _ := values.NewToolCall("toolu_2", "list_subjects", `{}`)
	r1, _ := values.NewToolResult("toolu_1", `{"lessons":[]}`, false)
	r2, _ := values.NewToolResult("toolu_2", `{"error":"refused"}`, true)
	call := agentCall(t,
		ok(values.StudentTurn("first-text")),
		ok(values.CallsTurn([]values.ToolCall{c1, c2}, []byte{1})),
		ok(values.ResultsTurn([]values.ToolResult{r1, r2})),
		ok(values.StudentTurn("second-text")),
	)
	var rec recorder
	srv := rec.serve(t, http.StatusOK, toolUseJSON)
	if _, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	body := rec.last(t)

	if body["model"] != "claude-haiku-5-5" || body["max_tokens"] != float64(400) {
		t.Fatalf("model/max_tokens = %v/%v", body["model"], body["max_tokens"])
	}
	// Forced tool choice ("any"/"tool") is a 400 on the 5.x models (docs/refs/anthropic.md section 7).
	if tc := body["tool_choice"].(map[string]any); len(tc) != 1 || tc["type"] != "auto" {
		t.Fatalf("tool_choice = %v", tc)
	}
	tools := body["tools"].([]any)
	tool := tools[0].(map[string]any)
	if len(tools) != 1 || tool["name"] != "reply" || tool["description"] != "Card shown to the student." ||
		tool["input_schema"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools = %v", tools)
	}

	system := body["system"].([]any)
	if len(system) != 3 {
		t.Fatalf("system = %v", system)
	}
	s0, s1, s2 := system[0].(map[string]any), system[1].(map[string]any), system[2].(map[string]any)
	if s0["text"] != agentSystem || s0["cache_control"] != nil || !strings.Contains(s1["text"].(string), `"subj-1"`) ||
		s1["cache_control"].(map[string]any)["type"] != "ephemeral" || !strings.Contains(s2["text"].(string), `"today"`) ||
		s2["cache_control"] != nil {
		t.Fatalf("system blocks = %v", system)
	}

	msgs := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	m0, m1, m2 := msgs[0].(map[string]any), msgs[1].(map[string]any), msgs[2].(map[string]any)
	if m0["role"] != "user" || m0["content"].([]any)[0].(map[string]any)["text"] != "first-text" {
		t.Fatalf("message 0 = %v", m0)
	}
	uses := m1["content"].([]any)
	use := uses[0].(map[string]any)
	if m1["role"] != "assistant" || len(uses) != 2 || use["type"] != "tool_use" || use["id"] != "toolu_1" || use["name"] != "find_lessons" ||
		use["input"].(map[string]any)["subject"] != "subj-1" || len(uses[1].(map[string]any)["input"].(map[string]any)) != 0 {
		t.Fatalf("message 1 = %v", m1)
	}
	content := m2["content"].([]any)
	res1, res2, text := content[0].(map[string]any), content[1].(map[string]any), content[2].(map[string]any)
	if m2["role"] != "user" || len(content) != 3 || res1["type"] != "tool_result" || res1["tool_use_id"] != "toolu_1" ||
		res1["content"] != `{"lessons":[]}` || res1["is_error"] != nil || res2["is_error"] != true || text["text"] != "second-text" {
		t.Fatalf("message 2 = %v", m2)
	}
	if text["cache_control"].(map[string]any)["type"] != "ephemeral" || res1["cache_control"] != nil || res2["cache_control"] != nil {
		t.Fatalf("the cache breakpoint must be on the last block only: %v", m2)
	}
	// thinking: a disabled config is a 400 on the 5.x models, so the field is never sent.
	for _, unwanted := range []string{"temperature", "top_p", "top_k", "stream", "thinking"} {
		if _, ok := body[unwanted]; ok {
			t.Errorf("request has %s", unwanted)
		}
	}
}

func TestStepWithoutToolUseIsAFailure(t *testing.T) {
	// "tool_use": a text-only answer is unusable even when the stop reason claims tool use.
	for _, stop := range []string{`"end_turn"`, `"max_tokens"`, `"refusal"`, `null`, `"pause_turn"`, `"tool_use"`} {
		body := `{"type":"message","content":[{"type":"text","text":"plain text"}],"stop_reason":` + stop +
			`,"usage":{"input_tokens":5,"output_tokens":2}}`
		var rec recorder
		srv := rec.serve(t, http.StatusOK, body)
		answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
		if err != nil {
			t.Fatalf("%s: %v", stop, err)
		}
		var pe *values.ProviderError
		if f := answer.Failure(); !errors.As(f, &pe) || pe.Kind != values.KindPermanent {
			t.Errorf("stop %s: failure = %v, want permanent", stop, f)
		}
		if answer.Usage().Total() != 7 {
			t.Errorf("stop %s: usage = %v", stop, answer.Usage())
		}
		if answer.Refused() != (stop == `"refusal"`) {
			t.Errorf("stop %s: refused = %t", stop, answer.Refused())
		}
	}
}

// TestStepReadsTheTextOfAnEndOfTurn: a text-only end of turn carries its text blocks joined (thinking skipped) for the
// reply card of TASK-200; any other stop reason carries none.
func TestStepReadsTheTextOfAnEndOfTurn(t *testing.T) {
	const content = `[{"type":"thinking","thinking":"secret-thinking","signature":"sig"},` +
		`{"type":"text","text":"Горутина — "},{"type":"text","text":"лёгкий поток."}]`
	for stop, want := range map[string]string{
		`"end_turn"`: "Горутина — лёгкий поток.", `"max_tokens"`: "", `"stop_sequence"`: "", `"pause_turn"`: "", `null`: "",
	} {
		body := `{"type":"message","content":` + content + `,"stop_reason":` + stop + `,"usage":{"input_tokens":5,"output_tokens":2}}`
		var rec recorder
		srv := rec.serve(t, http.StatusOK, body)
		answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
		if err != nil {
			t.Fatalf("%s: %v", stop, err)
		}
		text, ok := answer.PlainText()
		if text != want || ok != (want != "") || answer.Failure() == nil || !answer.Assistant().IsZero() {
			t.Errorf("stop %s: text %q %t, failure %v", stop, text, ok, answer.Failure())
		}
		if strings.Contains(answer.String(), "Горутина") {
			t.Errorf("stop %s: String prints the text: %s", stop, answer)
		}
	}
}

// TestStepKeepsTheStopReason: the answer carries the stop reason for the log line, and a cut at the output cap fails as
// "output cap" (TASK-172), also when the cut left a broken call.
func TestStepKeepsTheStopReason(t *testing.T) {
	const usage = `,"usage":{"input_tokens":5,"output_tokens":1024}}`
	for name, tc := range map[string]struct {
		body   string
		stop   values.StopReason
		reason string
	}{
		"tool use":     {toolUseJSON, values.StopToolUse, ""},
		"end of turn":  {`{"type":"message","content":[{"type":"text","text":"t"}],"stop_reason":"end_turn"` + usage, values.StopEnd, "no tool use"},
		"output cap":   {`{"type":"message","content":[{"type":"text","text":"t"}],"stop_reason":"max_tokens"` + usage, values.StopMaxTokens, "output cap"},
		"cut call":     {`{"type":"message","content":[{"type":"tool_use","id":"t1","name":"reply","input":[]}],"stop_reason":"max_tokens"` + usage, values.StopMaxTokens, "output cap"},
		"pause":        {`{"type":"message","content":[],"stop_reason":"pause_turn"` + usage, values.StopOther, "no tool use"},
		"no reason":    {`{"type":"message","content":[],"stop_reason":null` + usage, values.StopOther, "no tool use"},
		"refusal":      {`{"type":"message","content":[],"stop_reason":"refusal"` + usage, values.StopRefusal, "refused"},
		"bad tool use": {`{"type":"message","content":[{"type":"tool_use","id":"t 1","name":"reply","input":{}}],"stop_reason":"tool_use"` + usage, values.StopToolUse, "malformed tool call"},
	} {
		var rec recorder
		srv := rec.serve(t, http.StatusOK, tc.body)
		answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reason := ""
		var pe *values.ProviderError
		if errors.As(answer.Failure(), &pe) {
			reason = pe.Reason
		}
		if answer.Stop() != tc.stop || reason != tc.reason {
			t.Errorf("%s: stop %s, failure %q; want %s, %q", name, answer.Stop(), reason, tc.stop, tc.reason)
		}
	}
}

// TestStepRefusalWinsOverContent: the stop reason is read before the blocks, so a refusal with a call is refused.
func TestStepRefusalWinsOverContent(t *testing.T) {
	body := `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"reply","input":{"text":"x"}}],` +
		`"stop_reason":"refusal","usage":{"input_tokens":5,"output_tokens":2}}`
	var rec recorder
	srv := rec.serve(t, http.StatusOK, body)
	answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
	if err != nil {
		t.Fatal(err)
	}
	if !answer.Refused() || len(answer.Calls()) != 0 || answer.Failure() == nil {
		t.Fatalf("answer = %v, want refused without calls", answer)
	}
}

// TestStepMalformedCallsKeepTheUsage: a tool_use block that is not a valid call is the model's billed output, so it
// is an unusable answer with its usage, not an error without one.
func TestStepMalformedCallsKeepTheUsage(t *testing.T) {
	for name, body := range map[string]string{
		"array input": `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"reply","input":[]}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
		"no input":    `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"reply"}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
		"bad id":      `{"type":"message","content":[{"type":"tool_use","id":"t 1","name":"reply","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
		"bad name":    `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"Reply","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
		"huge input":  `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"reply","input":{"text":"` + strings.Repeat("x", values.MaxArgumentsBytes) + `"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		var rec recorder
		srv := rec.serve(t, http.StatusOK, body)
		answer, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var pe *values.ProviderError
		if f := answer.Failure(); !errors.As(f, &pe) || pe.Kind != values.KindPermanent || answer.Refused() {
			t.Errorf("%s: failure = %v, want permanent and not refused", name, f)
		}
		if answer.Usage().Total() != 2 || len(answer.Calls()) != 0 {
			t.Errorf("%s: answer = %v", name, answer)
		}
		if strings.Contains(answer.String(), "xxxx") || strings.Contains(fmt.Sprint(answer.Failure()), "xxxx") {
			t.Errorf("%s: the answer quotes the arguments", name)
		}
	}
}

func TestStepMalformedAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"no usage":    `{"type":"message","content":[],"stop_reason":"tool_use"}`,
		"not message": `{"type":"error"}`,
		"not JSON":    `{`,
		"bad usage":   `{"type":"message","content":[],"stop_reason":"tool_use","usage":{"input_tokens":-1,"output_tokens":1}}`,
	} {
		var rec recorder
		srv := rec.serve(t, http.StatusOK, body)
		_, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
		var pe *values.ProviderError
		if !errors.As(err, &pe) || pe.Kind != values.KindPermanent || !strings.HasPrefix(err.Error(), "anthropic.Step: ") {
			t.Errorf("%s: err = %v, want a permanent ProviderError", name, err)
		}
		if err != nil && (strings.Contains(err.Error(), "xxxx") || strings.Contains(err.Error(), "student-text")) {
			t.Errorf("%s: the error quotes text: %v", name, err)
		}
	}
}

func TestStepErrorsAreClassifiedLikeComplete(t *testing.T) {
	var rec recorder
	srv := rec.serve(t, http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"`+bodyMarker+`"}}`)
	_, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), agentCall(t))
	var pe *values.ProviderError
	if !errors.As(err, &pe) || pe.Kind != values.KindTransient || strings.Contains(err.Error(), bodyMarker) {
		t.Fatalf("429: err = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newClient(t, srv.URL, time.Minute).Step(ctx, agentCall(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
	if _, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), values.AgentCall{}); !errors.Is(err, values.ErrProviderPermanent) {
		t.Fatalf("empty call: err = %v", err)
	}
}
