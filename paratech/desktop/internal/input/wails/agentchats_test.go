package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeChats returns what it is set to and records the calls.
type fakeChats struct {
	list    []agentchats.Chat
	chat    agentchats.Chat
	earlier []agentchats.Request
	err     error
	calls   []string
}

func (f *fakeChats) List(context.Context) ([]agentchats.Chat, error) {
	f.calls = append(f.calls, "List")
	return f.list, f.err
}

func (f *fakeChats) New(context.Context) (agentchats.Chat, error) {
	f.calls = append(f.calls, "New")
	return f.chat, f.err
}

func (f *fakeChats) Rename(_ context.Context, id shared.ThreadID, title string) (agentchats.Chat, error) {
	f.calls = append(f.calls, fmt.Sprintf("Rename %s %s", id, title))
	return f.chat, f.err
}

func (f *fakeChats) Archive(_ context.Context, id shared.ThreadID) (agentchats.Chat, error) {
	f.calls = append(f.calls, "Archive "+id.String())
	return f.chat, f.err
}

func (f *fakeChats) Restore(_ context.Context, id shared.ThreadID) (agentchats.Chat, error) {
	f.calls = append(f.calls, "Restore "+id.String())
	return f.chat, f.err
}

func (f *fakeChats) Earlier(_ context.Context, id shared.ThreadID) ([]agentchats.Request, error) {
	f.calls = append(f.calls, "Earlier "+id.String())
	return f.earlier, f.err
}

func newChatsAgent(t *testing.T, c *fakeChats) *AgentService {
	t.Helper()
	s, _, _, _ := newAgent(t, readyLink())
	s.chats = c
	return s
}

var chatAt = time.Date(2026, 10, 27, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))

// The JSON contract of the chat list and of a chat call the page reads.
func TestAgentChatsContract(t *testing.T) {
	c := &fakeChats{
		list: []agentchats.Chat{
			{ID: "th2", Title: "Матан и физика", LastActive: chatAt, Running: true},
			{ID: "th1", Title: "Новый чат", Archived: true, LastActive: chatAt.Add(-time.Hour)},
		},
		chat: agentchats.Chat{ID: "th3", Title: "Новый чат", LastActive: chatAt},
	}
	s := newChatsAgent(t, c)
	got, err := json.Marshal(s.Chats(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"problem":"","chats":[` +
		`{"threadId":"th2","title":"Матан и физика","archived":false,"lastActive":"2026-10-27T06:30:00Z","running":true},` +
		`{"threadId":"th1","title":"Новый чат","archived":true,"lastActive":"2026-10-27T05:30:00Z","running":false}]}`
	if string(got) != want {
		t.Errorf("Chats JSON =\n%s\nwant\n%s", got, want)
	}

	got, err = json.Marshal(s.NewChat(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	want = `{"problem":"","chat":{"threadId":"th3","title":"Новый чат","archived":false,"lastActive":"2026-10-27T06:30:00Z","running":false}}`
	if string(got) != want {
		t.Errorf("NewChat JSON =\n%s\nwant\n%s", got, want)
	}

	c.list = nil
	if v := s.Chats(context.Background()); v.Problem != "" || v.Chats == nil || len(v.Chats) != 0 {
		t.Errorf("empty list = %+v, want an empty array", v)
	}
}

// Rename, archive and restore reach the use case with the chat id; the result is the chat after the call.
func TestAgentChatCallsForward(t *testing.T) {
	c := &fakeChats{chat: agentchats.Chat{ID: "th1", Title: "Мой чат", LastActive: chatAt}}
	s := newChatsAgent(t, c)
	ctx := context.Background()
	results := []AgentChatResult{s.RenameChat(ctx, "th1", "Мой чат"), s.ArchiveChat(ctx, "th1"), s.RestoreChat(ctx, "th1")}
	for _, r := range results {
		if r.Problem != "" || r.Chat.ThreadID != "th1" || r.Chat.Title != "Мой чат" {
			t.Errorf("result = %+v", r)
		}
	}
	if want := "Rename th1 Мой чат|Archive th1|Restore th1"; strings.Join(c.calls, "|") != want {
		t.Errorf("calls = %v, want %s", c.calls, want)
	}
}

// Errors of the chat calls are the problems the panel knows; an unknown cause is logged, never shown.
func TestAgentChatProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"missing chat", fmt.Errorf("agentchats: rename: %w", shared.ErrNotFound), AgentProblemNotFound},
		{"bad title", fmt.Errorf("agentchats: rename: %w", thread.ErrInvalidTitle), AgentProblemInvalid},
		{"bad id", fmt.Errorf("agentchats: rename: %w", agentchats.ErrInvalidInput), AgentProblemInvalid},
		{"running", fmt.Errorf("agentchats: rename: %w", agentchats.ErrBusy), AgentProblemBusy},
		{"not final", fmt.Errorf("agentchats: archive: %w", thread.ErrInvalidTransition), AgentProblemBusy},
		{"changed meanwhile", fmt.Errorf("agentchats: archive: %w", shared.ErrVersionConflict), AgentProblemBusy},
		{"db down", errors.New("db down"), AgentProblemFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, logs := newAgent(t, readyLink())
			c := &fakeChats{err: tt.err}
			s.chats = c
			ctx := context.Background()
			for _, r := range []AgentChatResult{
				s.NewChat(ctx), s.RenameChat(ctx, "th1", "x"), s.ArchiveChat(ctx, "th1"), s.RestoreChat(ctx, "th1"),
			} {
				if r.Problem != tt.want || r.Chat != (AgentChatView{}) {
					t.Errorf("result = %+v, want %s", r, tt.want)
				}
			}
			if v := s.ChatEarlier(ctx, "th1"); v.Problem != tt.want || v.Requests == nil || len(v.Requests) != 0 {
				t.Errorf("ChatEarlier = %+v, want %s", v, tt.want)
			}
			if logged := strings.Contains(logs.String(), "db down"); logged != (tt.want == AgentProblemFailed) {
				t.Errorf("logged %v for %s:\n%s", logged, tt.want, logs.String())
			}
		})
	}

	s, _, _, _ := newAgent(t, readyLink())
	s.chats = &fakeChats{err: errors.New("db down")}
	if v := s.Chats(context.Background()); v.Problem != AgentProblemFailed || v.Chats == nil {
		t.Errorf("Chats = %+v", v)
	}
}

// A malformed chat id is refused before the use case.
func TestAgentChatRefusesMalformedID(t *testing.T) {
	c := &fakeChats{}
	s := newChatsAgent(t, c)
	ctx := context.Background()
	for _, id := range []string{"", "a/b", "..\\x"} {
		for _, r := range []AgentChatResult{s.RenameChat(ctx, id, "x"), s.ArchiveChat(ctx, id), s.RestoreChat(ctx, id)} {
			if r.Problem != AgentProblemInvalid {
				t.Errorf("%q: result = %+v, want invalid", id, r)
			}
		}
		if v := s.ChatEarlier(ctx, id); v.Problem != AgentProblemInvalid {
			t.Errorf("%q: ChatEarlier = %+v, want invalid", id, v)
		}
	}
	if len(c.calls) != 0 {
		t.Errorf("the use case was called: %v", c.calls)
	}
}

// The earlier requests of a chat: texts, mentions with their labels, the reply line and the current mark.
func TestAgentChatEarlierContract(t *testing.T) {
	lesson, err := values.NewRef(values.KindLesson, "les-2")
	if err != nil {
		t.Fatal(err)
	}
	m, err := values.NewMention(lesson, "Лекция 2")
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeChats{earlier: []agentchats.Request{
		{Turn: 1, Text: "что задали", Reply: "Задано: №5"},
		{Turn: 2, Text: "сравни с лекцией", Mentions: []values.Mention{m}, Current: true},
	}}
	s := newChatsAgent(t, c)
	got, err := json.Marshal(s.ChatEarlier(context.Background(), "th1"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"problem":"","requests":[` +
		`{"turn":1,"text":"что задали","mentions":[],"reply":"Задано: №5","current":false},` +
		`{"turn":2,"text":"сравни с лекцией","mentions":[{"kind":"lesson","id":"les-2","label":"Лекция 2"}],"reply":"","current":true}]}`
	if string(got) != want {
		t.Errorf("ChatEarlier JSON =\n%s\nwant\n%s", got, want)
	}
}
