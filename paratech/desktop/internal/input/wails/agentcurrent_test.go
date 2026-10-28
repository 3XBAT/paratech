package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeCurrent answers Current with what it is set to and records the chats asked.
type fakeCurrent struct {
	update AgentUpdate
	ok     bool
	err    error
	asked  []shared.ThreadID
}

func (f *fakeCurrent) Current(_ context.Context, id shared.ThreadID) (AgentUpdate, bool, error) {
	f.asked = append(f.asked, id)
	return f.update, f.ok, f.err
}

func newCurrentAgent(t *testing.T, c *fakeCurrent) (*AgentService, *fakeAgentDriver) {
	t.Helper()
	s, d, _, _ := newAgent(t, readyLink())
	s.current = c
	return s, d
}

// TestAgentChatCurrentIsTheUpdateEvent: the update of ChatCurrent is the JSON the panel emits as agent:update for the
// same request — a plan for «Да», questions, a stopped request for «Продолжить» — and the Driver starts nothing.
func TestAgentChatCurrentIsTheUpdateEvent(t *testing.T) {
	steps, err := values.DecodePlanSteps(`[{"tool":"create_subject","arguments":{"name":"Физика"}},{"tool":"propose_deadline","arguments":{"assignment":"a-1"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewScope(mustRef(t, values.KindSubject, "sub-1"), values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		u    AgentUpdate
	}{
		{"plan waits for Да", AgentUpdate{Thread: "th1", Turn: 2, Scope: scope, Status: thread.StatusAwaitingPlan,
			Cards: []values.Card{values.PlanCard{Summary: "Понял так", Steps: steps, Notes: []values.Text{"без писем"}}}}},
		{"questions wait for answers", AgentUpdate{Thread: "th1", Turn: 2, Scope: scope, Status: thread.StatusAwaitingAnswers,
			Cards: []values.Card{values.QuestionsCard{Round: 2, Questions: []values.Question{
				{Kind: values.QuestionChoice, Text: "Когда?", Options: []values.Text{"сегодня", "завтра"}, Default: 1},
			}}}}},
		{"stopped, Продолжить", AgentUpdate{Thread: "th1", Turn: 2, Scope: scope, Status: thread.StatusStopped,
			StopReason: values.StopUser, Cards: []values.Card{values.StoppedCard{Reason: values.StopUser}}}},
		{"running, no card", AgentUpdate{Thread: "th1", Turn: 2, Scope: scope, Status: thread.StatusExecuting}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &fakeCurrent{update: tt.u, ok: true}
			s, d := newCurrentAgent(t, c)
			got := s.ChatCurrent(context.Background(), "th1")
			if got.Problem != "" || got.Update == nil {
				t.Fatalf("ChatCurrent = %+v", got)
			}
			gotJSON, err := json.Marshal(got.Update)
			if err != nil {
				t.Fatal(err)
			}

			panel := NewAgentPanel(discard())
			em := &fakeEmitter{}
			panel.Attach(em)
			panel.Publish(tt.u)
			wantJSON, err := json.Marshal(em.events[0].data[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Update.Cards) != len(tt.u.Cards) {
				t.Errorf("cards = %+v, want %d", got.Update.Cards, len(tt.u.Cards))
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("ChatCurrent update =\n%s\nagent:update =\n%s", gotJSON, wantJSON)
			}
			if len(c.asked) != 1 || c.asked[0] != "th1" || len(d.calls) != 0 {
				t.Errorf("asked %v, driver calls %+v", c.asked, d.calls)
			}
		})
	}
}

// The contract of the snapshot: problem and update; an ended request or a chat without one is a null update; a
// problem or progress mark of the mapped update never reaches the page; a broken card is dropped as in agent:update.
func TestAgentChatCurrentContract(t *testing.T) {
	c := &fakeCurrent{ok: true, update: AgentUpdate{Thread: "th1", Turn: 1, Status: thread.StatusStopped,
		StopReason: values.StopOffline, Problem: "busy", Progress: true,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopOffline}, values.QuestionsCard{Round: 3}}}}
	s, _ := newCurrentAgent(t, c)
	got, err := json.Marshal(s.ChatCurrent(context.Background(), "th1"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"problem":"","update":{"threadId":"th1","turn":1,"subjectId":"","lessonId":"","assignmentId":"","screen":"",` +
		`"status":"STOPPED","stopReason":"offline","problem":"","progress":false,"cards":[` +
		card(`"kind":"stopped","stopReason":"offline"`) + `],"fill":null}}`
	if string(got) != want {
		t.Errorf("ChatCurrent JSON =\n%s\nwant\n%s", got, want)
	}

	c.ok, c.update = false, AgentUpdate{}
	got, err = json.Marshal(s.ChatCurrent(context.Background(), "th1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"problem":"","update":null}` {
		t.Errorf("no current request = %s", got)
	}
}

// Errors are the problems of the chat calls: a missing chat is not_found, as ChatEarlier; an unknown cause is logged,
// never shown. A malformed id is refused before the core; without the core the call is unavailable.
func TestAgentChatCurrentProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"missing chat", fmt.Errorf("agentturn: current: %w", shared.ErrNotFound), AgentProblemNotFound},
		{"db down", errors.New("db down"), AgentProblemFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, logs := newAgent(t, readyLink())
			s.current = &fakeCurrent{err: tt.err, ok: true, update: AgentUpdate{Thread: "th1", Turn: 1}}
			if v := s.ChatCurrent(context.Background(), "th1"); v.Problem != tt.want || v.Update != nil {
				t.Errorf("ChatCurrent = %+v, want %s", v, tt.want)
			}
			if logged := strings.Contains(logs.String(), "db down"); logged != (tt.want == AgentProblemFailed) {
				t.Errorf("logged %v:\n%s", logged, logs.String())
			}
		})
	}

	c := &fakeCurrent{}
	s, _ := newCurrentAgent(t, c)
	for _, id := range []string{"", "a/b", "..\\x"} {
		if v := s.ChatCurrent(context.Background(), id); v.Problem != AgentProblemInvalid || v.Update != nil {
			t.Errorf("%q: ChatCurrent = %+v, want invalid", id, v)
		}
	}
	if len(c.asked) != 0 {
		t.Errorf("the core was asked: %v", c.asked)
	}

	s.current = nil
	if v := s.ChatCurrent(context.Background(), "th1"); v.Problem != AgentProblemUnavailable {
		t.Errorf("without the core = %+v", v)
	}
}
