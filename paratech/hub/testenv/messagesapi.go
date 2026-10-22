package testenv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Markers of the thinking blocks FakeMessagesAPI issues, for leak checks: they must never reach the client.
const (
	FakeThinkingMarker  = "fake-thinking-marker"
	FakeSignatureMarker = "fake-signature-marker"
)

// FakeToolUse is one tool_use block of a scripted FakeMessagesAPI answer; Input is a JSON object.
type FakeToolUse struct {
	ID    string
	Name  string
	Input string
}

// FakeMessagesAPI is a local Messages API for planner steps that behaves like the real one with adaptive thinking on
// (docs/refs/anthropic.md section 7): every answer starts with a signed thinking block, and a request whose last
// assistant message holds tool_use must start that message with a thinking block it issued, text and signature
// unchanged; otherwise it answers 400 invalid_request_error. Answers are scripted; each call takes the next one.
// Safe for concurrent use.
type FakeMessagesAPI struct {
	URL string

	mu       sync.Mutex
	answers  [][]FakeToolUse
	issued   map[string]string // signature -> thinking text
	requests [][]byte
	rejected int
}

// NewFakeMessagesAPI starts the fake with the scripted answers; it stops when the test ends.
func NewFakeMessagesAPI(t testing.TB, answers ...[]FakeToolUse) *FakeMessagesAPI {
	t.Helper()
	f := &FakeMessagesAPI{answers: answers, issued: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

// Requests returns the request bodies, oldest first.
func (f *FakeMessagesAPI) Requests() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.requests...)
}

// Rejected is how many requests were refused for a missing or changed thinking block.
func (f *FakeMessagesAPI) Rejected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rejected
}

func (f *FakeMessagesAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, body)

	if msg := f.checkThinking(body); msg != "" {
		f.rejected++
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": msg},
		})
		return
	}
	if len(f.answers) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"type": "error", "error": map[string]any{"type": "api_error", "message": "no scripted answer"},
		})
		return
	}
	calls := f.answers[0]
	f.answers = f.answers[1:]

	// Characters that JSON encoders may escape differently, so "unchanged" is checked on the value, not the bytes.
	n := len(f.issued) + 1
	thinking := fmt.Sprintf("%s %d: <check> the lessons & then reply", FakeThinkingMarker, n)
	signature := fmt.Sprintf("%s-%d-EqoBCkgIARAB+/=", FakeSignatureMarker, n)
	f.issued[signature] = thinking
	content := []map[string]any{
		{"type": "thinking", "thinking": thinking, "signature": signature},
		{"type": "text", "text": "Calling tools."},
	}
	for _, c := range calls {
		content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": json.RawMessage(c.Input)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": fmt.Sprintf("msg_%d", n), "type": "message", "role": "assistant", "model": "claude-fake",
		"content": content, "stop_reason": "tool_use", "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 900, "output_tokens": 60},
	})
}

// checkThinking returns why the request is refused, or "".
func (f *FakeMessagesAPI) checkThinking(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "malformed request"
	}
	last := -1
	for i, m := range req.Messages {
		if m.Role == "assistant" {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(req.Messages[last].Content, &blocks); err != nil {
		return "malformed assistant content"
	}
	toolUse := false
	for _, b := range blocks {
		toolUse = toolUse || b.Type == "tool_use"
	}
	if !toolUse {
		return ""
	}
	if len(blocks) == 0 || blocks[0].Type != "thinking" {
		return fmt.Sprintf("messages.%d.content.0.type: Expected `thinking` or `redacted_thinking`, but found `%s`. "+
			"When `thinking` is enabled, a final `assistant` message must start with a thinking block.", last, firstType(blocks))
	}
	if want, ok := f.issued[blocks[0].Signature]; !ok || want != blocks[0].Thinking {
		return fmt.Sprintf("messages.%d.content.0: Invalid `signature` in `thinking` block", last)
	}
	return ""
}

func firstType(blocks []contentBlock) string {
	if len(blocks) == 0 {
		return "nothing"
	}
	return blocks[0].Type
}

// contentBlock is what the fake reads of an assistant content block.
type contentBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
