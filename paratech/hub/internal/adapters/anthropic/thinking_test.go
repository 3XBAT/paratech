package anthropic_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// continuation is the call of the next step: the first text, the echo of answer's calls and their results.
func continuation(t *testing.T, answer values.AgentAnswer) values.AgentCall {
	t.Helper()
	text, err := values.StudentTurn("student-text-marker")
	if err != nil {
		t.Fatal(err)
	}
	echo, err := values.CallsTurn(answer.Calls(), []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	var results []values.ToolResult
	for _, c := range answer.Calls() {
		r, err := values.NewToolResult(c.CallID(), `{"lessons":[]}`, false)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	res, err := values.ResultsTurn(results)
	if err != nil {
		t.Fatal(err)
	}
	return agentCall(t, text, echo, res)
}

// assistantContent decodes the content of the last assistant message of a request body.
func assistantContent(t *testing.T, body []byte) any {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "assistant" {
			return req.Messages[i].Content
		}
	}
	t.Fatal("no assistant message")
	return nil
}

// TestStepKeepsTheRawTurn: the answer carries the whole content array as received (thinking with its signature,
// text, tool_use), while its calls are read as before.
func TestStepKeepsTheRawTurn(t *testing.T) {
	api := testenv.NewFakeMessagesAPI(t, []testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}})
	answer, err := newClient(t, api.URL, time.Minute).Step(t.Context(), agentCall(t))
	if err != nil || answer.Failure() != nil {
		t.Fatalf("answer = %v, %v", answer, err)
	}
	raw := answer.Assistant().Raw()
	if !strings.Contains(raw, testenv.FakeThinkingMarker) || !strings.Contains(raw, testenv.FakeSignatureMarker) ||
		!strings.Contains(raw, `"toolu_01"`) {
		t.Fatalf("kept turn = %s", raw)
	}
	if len(answer.Calls()) != 1 || strings.Contains(answer.Calls()[0].Arguments(), testenv.FakeThinkingMarker) {
		t.Fatalf("calls = %v", answer.Calls())
	}
}

// TestContinuationWithoutTheThinkingBlockIsRejected: with thinking on, the API refuses an echo of tool_use without
// the turn's thinking block; that is what the Hub would send if it did not keep the turn.
func TestContinuationWithoutTheThinkingBlockIsRejected(t *testing.T) {
	api := testenv.NewFakeMessagesAPI(t,
		[]testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}},
		[]testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Nothing is due."}`}})
	client := newClient(t, api.URL, time.Minute)
	first, err := client.Step(t.Context(), agentCall(t))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Step(t.Context(), continuation(t, first))
	if !errors.Is(err, values.ErrProviderPermanent) || api.Rejected() != 1 {
		t.Fatalf("echo without thinking: err = %v, rejected = %d", err, api.Rejected())
	}

	// A changed signature is refused too: the kept turn must go back unchanged.
	forged, err := values.NewAssistantTurn(strings.Replace(first.Assistant().Raw(), testenv.FakeSignatureMarker, "forged", 1))
	if err != nil {
		t.Fatal(err)
	}
	call, err := continuation(t, first).WithAssistant(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Step(t.Context(), call); !errors.Is(err, values.ErrProviderPermanent) || api.Rejected() != 2 {
		t.Fatalf("echo with a changed signature: err = %v, rejected = %d", err, api.Rejected())
	}
}

// TestContinuationSendsTheKeptTurnUnchanged: with the kept turn attached, the assistant message is exactly the
// content the API returned (signature included) and the API accepts it.
func TestContinuationSendsTheKeptTurnUnchanged(t *testing.T) {
	api := testenv.NewFakeMessagesAPI(t,
		[]testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}},
		[]testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Nothing is due."}`}})
	client := newClient(t, api.URL, time.Minute)
	first, err := client.Step(t.Context(), agentCall(t))
	if err != nil {
		t.Fatal(err)
	}
	call, err := continuation(t, first).WithAssistant(first.Assistant())
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Step(t.Context(), call)
	if err != nil || api.Rejected() != 0 || second.Calls()[0].Tool() != "reply" {
		t.Fatalf("continuation = %v, %v (rejected %d)", second, err, api.Rejected())
	}

	var want any
	if err := json.Unmarshal([]byte(first.Assistant().Raw()), &want); err != nil {
		t.Fatal(err)
	}
	reqs := api.Requests()
	if got := assistantContent(t, reqs[len(reqs)-1]); !reflect.DeepEqual(got, want) {
		t.Fatalf("assistant content sent = %v, want %v", got, want)
	}
}
