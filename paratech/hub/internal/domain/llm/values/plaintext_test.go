package values

import (
	"strings"
	"testing"
)

// TestPlainText: only a text-only end of turn offers its trimmed text (TASK-200); a call, a refusal, a broken call,
// another stop reason or a blank text offer none, and the answer never prints the text.
func TestPlainText(t *testing.T) {
	u, _ := NewUsage(10, 2)
	call, err := NewToolCall("t1", "reply", `{"text":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	const text = "secret-plain-text"
	for name, tc := range map[string]struct {
		answer AgentAnswer
		want   string
	}{
		"end of turn":    {NewAgentAnswer(nil, u, false).WithStop(StopEnd).WithText("\n " + text + " \n"), text},
		"blank":          {NewAgentAnswer(nil, u, false).WithStop(StopEnd).WithText(" \n"), ""},
		"output cap":     {NewAgentAnswer(nil, u, false).WithStop(StopMaxTokens).WithText(text), ""},
		"no stop reason": {NewAgentAnswer(nil, u, false).WithText(text), ""},
		"tool use":       {NewAgentAnswer([]ToolCall{call}, u, true).WithText(text), ""},
		"calls, no use":  {NewAgentAnswer([]ToolCall{call}, u, false).WithStop(StopEnd).WithText(text), ""},
		"refused":        {RefusedAgentAnswer(u).WithText(text), ""},
		"malformed":      {MalformedAgentAnswer(u).WithStop(StopEnd).WithText(text), ""},
	} {
		got, ok := tc.answer.PlainText()
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: PlainText = %q, %t; want %q", name, got, ok, tc.want)
		}
		if strings.Contains(tc.answer.String(), text) || strings.Contains(tc.answer.GoString(), text) {
			t.Errorf("%s: the answer prints its text", name)
		}
	}
}
