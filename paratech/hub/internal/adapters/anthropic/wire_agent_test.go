package anthropic_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// wireMessage is what the history tests read of one sent message.
type wireMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

// wireText is a text block of a sent message.
type wireText struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control"`
}

func sentMessages(t *testing.T, body []byte) []wireMessage {
	t.Helper()
	var req struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	return req.Messages
}

// texts reads the blocks of a message as text blocks; "" stands for a block of another type.
func texts(t *testing.T, m wireMessage) []string {
	t.Helper()
	out := make([]string, 0, len(m.Content))
	for _, raw := range m.Content {
		var b wireText
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		if b.Type != "text" {
			b.Text = ""
		}
		out = append(out, b.Text)
	}
	return out
}

func student(t *testing.T, text string) values.Turn {
	t.Helper()
	turn, err := values.StudentTurn(text)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func agentReply(t *testing.T, text string) values.Turn {
	t.Helper()
	turn, err := values.AgentReplyTurn(text)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

// wantHistory checks roles and text blocks of the sent messages, and that only the last block of the last message
// carries the cache breakpoint.
func wantHistory(t *testing.T, body []byte, roles []string, blocks [][]string) {
	t.Helper()
	msgs := sentMessages(t, body)
	if len(msgs) != len(roles) {
		t.Fatalf("got %d messages, want %d: %s", len(msgs), len(roles), body)
	}
	for i, m := range msgs {
		if m.Role != roles[i] {
			t.Errorf("message %d role = %s, want %s", i, m.Role, roles[i])
		}
		if blocks[i] != nil && !reflect.DeepEqual(texts(t, m), blocks[i]) {
			t.Errorf("message %d texts = %q, want %q", i, texts(t, m), blocks[i])
		}
		if i > 0 && msgs[i-1].Role == m.Role {
			t.Errorf("messages %d and %d share the role %s", i-1, i, m.Role)
		}
		for j, raw := range m.Content {
			var b wireText
			_ = json.Unmarshal(raw, &b)
			last := i == len(msgs)-1 && j == len(m.Content)-1
			if (b.CacheControl != nil) != last {
				t.Errorf("message %d block %d: cache_control %s, want it only on the last block", i, j, b.CacheControl)
			}
		}
	}
}

// TestStepSendsHistoryPairs: earlier requests with their agent replies alternate user/assistant, and the current
// request is the last student text (TASK-181).
func TestStepSendsHistoryPairs(t *testing.T) {
	var rec recorder
	srv := rec.serve(t, http.StatusOK, toolUseJSON)
	call := agentCall(t,
		student(t, "what is a goroutine"), agentReply(t, "reply: A goroutine is a light thread."),
		student(t, "first try"), student(t, "who is Rybakov"), agentReply(t, "cannot_do: unknown person"),
		student(t, "and the deadline?"))
	if _, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	wantHistory(t, rec.bodies[0],
		[]string{"user", "assistant", "user", "assistant", "user"},
		[][]string{
			{"what is a goroutine"}, {"reply: A goroutine is a light thread."},
			{"first try", "who is Rybakov"}, {"cannot_do: unknown person"},
			{"and the deadline?"},
		})
}

// TestStepMarksUnansweredRequests: a history without agent replies (a desktop before TASK-183) gets the fixed marker
// between consecutive student texts instead of one merged user message.
func TestStepMarksUnansweredRequests(t *testing.T) {
	var rec recorder
	srv := rec.serve(t, http.StatusOK, toolUseJSON)
	call := agentCall(t, student(t, "what is a goroutine"), student(t, "who is Rybakov"), student(t, "and the deadline?"))
	if _, err := newClient(t, srv.URL, time.Minute).Step(t.Context(), call); err != nil {
		t.Fatal(err)
	}
	wantHistory(t, rec.bodies[0],
		[]string{"user", "assistant", "user", "assistant", "user"},
		[][]string{
			{"what is a goroutine"}, {values.MissingReplyText}, {"who is Rybakov"}, {values.MissingReplyText},
			{"and the deadline?"},
		})
}

// TestStepKeepsTheEchoWithPairs: with pairs or with markers before it, the current request's echo still goes back as
// the kept turn unchanged, the API accepts it, and the cache breakpoint is on the last result.
func TestStepKeepsTheEchoWithPairs(t *testing.T) {
	for name, earlier := range map[string][]values.Turn{
		"pairs":   {student(t, "what is a goroutine"), agentReply(t, "reply: A light thread.")},
		"markers": {student(t, "what is a goroutine")},
	} {
		api := testenv.NewFakeMessagesAPI(t,
			[]testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}},
			[]testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Nothing is due."}`}})
		client := newClient(t, api.URL, time.Minute)
		current := student(t, "who is Rybakov")
		first, err := client.Step(t.Context(), agentCall(t, append(append([]values.Turn(nil), earlier...), current)...))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		echo, err := values.CallsTurn(first.Calls(), []byte{1})
		if err != nil {
			t.Fatal(err)
		}
		res, err := values.NewToolResult("toolu_01", `{"lessons":[]}`, false)
		if err != nil {
			t.Fatal(err)
		}
		results, err := values.ResultsTurn([]values.ToolResult{res})
		if err != nil {
			t.Fatal(err)
		}
		turns := append(append([]values.Turn(nil), earlier...), current, echo, results)
		call, err := agentCall(t, turns...).WithAssistant(first.Assistant())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Step(t.Context(), call); err != nil || api.Rejected() != 0 {
			t.Fatalf("%s: continuation err = %v, rejected = %d", name, err, api.Rejected())
		}

		reqs := api.Requests()
		body := reqs[len(reqs)-1]
		var kept any
		if err := json.Unmarshal([]byte(first.Assistant().Raw()), &kept); err != nil {
			t.Fatal(err)
		}
		if got := assistantContent(t, body); !reflect.DeepEqual(got, kept) {
			t.Errorf("%s: the kept turn changed: %v", name, got)
		}
		msgs := sentMessages(t, body)
		answer := "reply: A light thread."
		if name == "markers" {
			answer = values.MissingReplyText
		}
		if len(msgs) != 5 || msgs[1].Role != "assistant" || !reflect.DeepEqual(texts(t, msgs[1]), []string{answer}) ||
			!reflect.DeepEqual(texts(t, msgs[2]), []string{"who is Rybakov"}) || msgs[3].Role != "assistant" || msgs[4].Role != "user" {
			t.Fatalf("%s: messages = %s", name, body)
		}
		var last struct {
			Type         string          `json:"type"`
			CacheControl json.RawMessage `json:"cache_control"`
		}
		if err := json.Unmarshal(msgs[4].Content[len(msgs[4].Content)-1], &last); err != nil || last.Type != "tool_result" || last.CacheControl == nil {
			t.Fatalf("%s: last block = %s", name, msgs[4].Content[len(msgs[4].Content)-1])
		}
	}
}
