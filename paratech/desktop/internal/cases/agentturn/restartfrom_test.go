package agentturn

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// chatOfAnswers asks n requests in chat th1, each answered «ответ N».
func chatOfAnswers(t *testing.T, e *env, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		e.model.then(raw(fmt.Sprintf("r%d", i), "reply", fmt.Sprintf(`{"text":"ответ %d"}`, i)))
		if st := ask(t, e, fmt.Sprintf("просьба %d", i)); st.Status != thread.StatusDone {
			t.Fatalf("request %d = %+v", i, st)
		}
	}
}

func restartFrom(t *testing.T, e *env, n int, text string) (Step, error) {
	t.Helper()
	return e.svc.Ask(t.Context(), AskInput{ThreadID: "th1", Scope: subjectScope(t), Text: text, RestartFrom: n})
}

// droppedBy returns the request that dropped each stored request, 0 for the kept ones.
func droppedBy(e *env) map[int]int {
	out := map[int]int{}
	for _, en := range e.history.all("th1") {
		out[en.Turn] = en.DroppedBy
	}
	return out
}

// «Скрыть» from request 2: the agent gets request 1 and the new text only; the dropped requests stay stored with
// their mark; a second restart from request 1 drops the first restart too.
func TestAskRestartFrom(t *testing.T) {
	e := newEnv(t)
	chatOfAnswers(t, e, 3)
	stored := len(e.history.all("th1"))

	e.model.then(raw("n1", "reply", `{"text":"заново"}`))
	st, err := restartFrom(t, e, 2, "иначе")
	if err != nil || st.Turn != 4 || st.Status != thread.StatusDone {
		t.Fatalf("restart = %+v, %v", st, err)
	}
	want := []values.HistoryTurn{{StudentText: "просьба 1"}, {AgentReply: "reply: ответ 1"}, {StudentText: "иначе"}}
	if h := e.model.last(t).History; !reflect.DeepEqual(h[:3], want) || h[0].Calls != nil {
		t.Fatalf("planner history = %+v", h)
	}
	if got := droppedBy(e); !reflect.DeepEqual(got, map[int]int{1: 0, 2: 4, 3: 4, 4: 0}) {
		t.Fatalf("dropped by = %v", got)
	}
	if n := len(e.history.all("th1")); n <= stored {
		t.Fatalf("stored entries %d, had %d: nothing may be deleted", n, stored)
	}
	th, _ := e.threads.Get(t.Context(), "th1")
	if turn, _ := th.Turn(); turn.DropsFrom() != 2 || turn.ReplyLine() != "reply: заново" {
		t.Fatalf("stored request drops from %d, line %q", turn.DropsFrom(), turn.ReplyLine())
	}

	e.model.then(raw("n2", "reply", `{"text":"с начала"}`))
	if st, err = restartFrom(t, e, 1, "с нуля"); err != nil || st.Turn != 5 {
		t.Fatalf("second restart = %+v, %v", st, err)
	}
	if h := e.model.last(t).History; len(h) != 1 || h[0].StudentText != "с нуля" {
		t.Fatalf("planner history after the second restart = %+v", h)
	}
	if got := droppedBy(e); !reflect.DeepEqual(got, map[int]int{1: 5, 2: 5, 3: 5, 4: 5, 5: 0}) {
		t.Fatalf("dropped by after the second restart = %v", got)
	}
}

func TestAskRestartFromRefusals(t *testing.T) {
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	tests := []struct {
		name    string
		setup   func(t *testing.T, e *env)
		in      AskInput
		wantErr error
	}{
		{"negative", nil, AskInput{ThreadID: "th1", RestartFrom: -1}, ErrInvalidInput},
		{"no chat named", nil, AskInput{RestartFrom: 1}, ErrInvalidInput},
		{"past the last request", nil, AskInput{ThreadID: "th1", RestartFrom: 3}, thread.ErrInvalidTransition},
		{"missing chat", nil, AskInput{ThreadID: "nope", RestartFrom: 1}, shared.ErrNotFound},
		{"awaiting the plan", func(t *testing.T, e *env) {
			e.model.then(raw("p1", "show_plan", plan))
			ask(t, e, "добавь")
		}, AskInput{ThreadID: "th1", RestartFrom: 1}, thread.ErrInvalidTransition},
		{"running calls", func(t *testing.T, e *env) {
			e.model.then(raw("c1", "list_subjects", `{}`))
			ask(t, e, "мои предметы")
		}, AskInput{ThreadID: "th1", RestartFrom: 1}, thread.ErrTurnInProgress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			chatOfAnswers(t, e, 2)
			if tt.setup != nil {
				tt.setup(t, e)
			}
			before, _ := e.threads.Get(t.Context(), "th1")
			steps := len(e.model.requests())
			in := tt.in
			in.Scope, in.Text = subjectScope(t), "заново"
			if _, err := e.svc.Ask(t.Context(), in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Ask = %v, want %v", err, tt.wantErr)
			}
			after, _ := e.threads.Get(t.Context(), "th1")
			if after.Version() != before.Version() || len(e.model.requests()) != steps {
				t.Fatalf("a refused restart changed the chat or called the Hub")
			}
			for turn, by := range droppedBy(e) {
				if by != 0 {
					t.Fatalf("request %d dropped by %d", turn, by)
				}
			}
		})
	}
}

// A failed write rolls the marks back with the request: the chat is as before and the restart can be asked again.
func TestAskRestartFromRollsBack(t *testing.T) {
	e := newEnv(t)
	chatOfAnswers(t, e, 2)
	e.history.failing = errors.New("disk full")
	if _, err := restartFrom(t, e, 1, "заново"); err == nil {
		t.Fatal("a failed write was accepted")
	}
	e.history.failing = nil
	th, _ := e.threads.Get(t.Context(), "th1")
	if turn, _ := th.Turn(); turn.Number() != 2 {
		t.Fatalf("stored request %d after the rollback", turn.Number())
	}
	if got := droppedBy(e); !reflect.DeepEqual(got, map[int]int{1: 0, 2: 0}) {
		t.Fatalf("dropped by after the rollback = %v", got)
	}

	e.model.then(raw("n1", "reply", `{"text":"ок"}`))
	if st, err := restartFrom(t, e, 1, "заново"); err != nil || st.Turn != 3 {
		t.Fatalf("restart again = %+v, %v", st, err)
	}
}

// The step reads exactly as the context ring does.
func TestHistoryLimitIsTheRingLimit(t *testing.T) {
	if historyLimit != thread.HistoryReadLimit {
		t.Fatalf("step read limit %d, ring %d", historyLimit, thread.HistoryReadLimit)
	}
}
