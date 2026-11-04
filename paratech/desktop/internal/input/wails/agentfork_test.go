package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// The JSON contract of ForkChat the page reads: the new chat and the text for its input.
func TestAgentForkChatContract(t *testing.T) {
	c := &fakeChats{fork: agentchats.ForkResult{
		Chat: agentchats.Chat{ID: "th9", Title: "Физика (fork)", LastActive: chatAt, Fill: &agentchats.Fill{Percent: 5, Level: thread.FillNormal}},
		Text: "просьба 3",
	}}
	s := newChatsAgent(t, c)
	got, err := json.Marshal(s.ForkChat(context.Background(), "th1", 3))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"problem":"","chat":{"threadId":"th9","title":"Физика (fork)","archived":false,"lastActive":"2026-10-27T06:30:00Z",` +
		`"running":false,"fill":{"percent":5,"level":"normal","overflow":false}},"text":"просьба 3"}`
	if string(got) != want {
		t.Errorf("ForkChat JSON =\n%s\nwant\n%s", got, want)
	}
	if want := "Fork th1 3"; strings.Join(c.calls, "|") != want {
		t.Errorf("calls = %v, want %s", c.calls, want)
	}
}

// A malformed id or a turn below 1 is refused before the use case.
func TestAgentForkChatRefusesBadInput(t *testing.T) {
	c := &fakeChats{}
	s := newChatsAgent(t, c)
	for _, tt := range []struct {
		id   string
		turn int
	}{{"", 1}, {"a/b", 1}, {"th1", 0}, {"th1", -2}} {
		if r := s.ForkChat(context.Background(), tt.id, tt.turn); r.Problem != AgentProblemInvalid || r.Text != "" ||
			r.Chat != (AgentChatView{}) {
			t.Errorf("ForkChat(%q, %d) = %+v, want invalid", tt.id, tt.turn, r)
		}
	}
	if len(c.calls) != 0 {
		t.Errorf("the use case was called: %v", c.calls)
	}
}

// Errors of a fork are the problems the panel knows; an unknown cause is logged by its op and never with the text.
func TestAgentForkChatProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"not a request to fork from", fmt.Errorf("agentchats: fork: %w", thread.ErrInvalidFork), AgentProblemInvalid},
		{"bad input", fmt.Errorf("agentchats: fork: %w", agentchats.ErrInvalidInput), AgentProblemInvalid},
		{"missing chat", fmt.Errorf("agentchats: fork: %w", shared.ErrNotFound), AgentProblemNotFound},
		{"id taken", fmt.Errorf("agentchats: fork: %w", shared.ErrVersionConflict), AgentProblemBusy},
		{"db down", errors.New("db down"), AgentProblemFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, logs := newAgent(t, readyLink())
			// A result next to the error must never leak out.
			s.chats = &fakeChats{err: tt.err, fork: agentchats.ForkResult{Text: "секретный текст", Chat: agentchats.Chat{ID: "th9"}}}
			r := s.ForkChat(context.Background(), "th1", 2)
			if r.Problem != tt.want || r.Text != "" || r.Chat != (AgentChatView{}) {
				t.Fatalf("ForkChat = %+v, want %s", r, tt.want)
			}
			failed := tt.want == AgentProblemFailed
			if logged := strings.Contains(logs.String(), opAgentFork) && strings.Contains(logs.String(), "db down"); logged != failed {
				t.Errorf("logged %v for %s:\n%s", logged, tt.want, logs.String())
			}
			if strings.Contains(logs.String(), "секретный") {
				t.Errorf("the log holds the text: %s", logs.String())
			}
		})
	}
}
