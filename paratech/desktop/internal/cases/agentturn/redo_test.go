package agentturn

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// competeOnce saves move on chat id right after the next load, as agentchats does without s.mu.
func competeOnce(t *testing.T, e *env, id shared.ThreadID, move func(*thread.Thread, time.Time) error) {
	t.Helper()
	var fired atomic.Bool
	e.threads.afterGet = func() {
		if fired.Swap(true) {
			return
		}
		th, err := e.threads.Get(context.Background(), id)
		if err != nil {
			t.Error(err)
			return
		}
		if err := move(th, e.clock.Now()); err != nil {
			t.Error(err)
			return
		}
		if err := e.threads.Save(context.Background(), th); err != nil {
			t.Error(err)
		}
	}
}

func rename(th *thread.Thread, now time.Time) error { return th.Rename("Мой чат", now) }

// A rename saved between the load of «Да» or the typed answers and their first save does not lose the click: the
// answer is applied again on the stored chat, once, and the result reaches the planner once.
func TestCardAnswerSurvivesARename(t *testing.T) {
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	q := `{"questions":[{"kind":"choice","question":"Какой вариант?","options":["А","Б"],"default":0}]}`
	tests := []struct {
		name   string
		card   values.RawCall
		answer func(e *env, r TurnRef) (Step, error)
		want   string
	}{
		{"approve plan", raw("p1", "show_plan", plan), func(e *env, r TurnRef) (Step, error) {
			return e.svc.ApprovePlan(t.Context(), r)
		}, `{"approved":true}`},
		{"answer questions", raw("q1", "ask_questions", q), func(e *env, r TurnRef) (Step, error) {
			return e.svc.AnswerQuestions(t.Context(), r, []values.Answer{{Question: 0, Option: 1}})
		}, `{"answers":[{"question":0,"option":1}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(tt.card).then(raw("r1", "reply", `{"text":"ок"}`))
			st := ask(t, e, "сделай")

			competeOnce(t, e, st.Thread, rename)
			got, err := tt.answer(e, ref(st))
			if err != nil {
				t.Fatalf("answer after a rename = %v", err)
			}
			if got.Status != thread.StatusDone {
				t.Fatalf("step = %+v, want DONE", got)
			}
			reqs := e.model.requests()
			if len(reqs) != 2 {
				t.Fatalf("Hub steps = %d, want 2", len(reqs))
			}
			h := reqs[1].History
			if a := h[len(h)-1].Results; len(a) != 1 || a[0].ResultJSON != tt.want {
				t.Fatalf("result sent = %+v, want %s", a, tt.want)
			}
			if th := storedThread(t, e, st.Thread); th.Title() != "Мой чат" {
				t.Fatalf("chat title %q, want the rename kept", th.Title())
			}
		})
	}
}

// A rename or an archive saved between Ask's load and its first save does not lose the student's message: the request
// is asked again on the stored chat, once, and reaches the Hub once.
func TestAskSurvivesAChatChange(t *testing.T) {
	tests := []struct {
		name      string
		named     bool // Ask names the chat; else it goes to the current one
		move      func(*thread.Thread, time.Time) error
		sameChat  bool
		wantTitle string
	}{
		{"rename, current chat", false, rename, true, "Мой чат"},
		{"rename, named chat", true, rename, true, "Мой чат"},
		{"archive, named chat comes back", true, (*thread.Thread).Archive, true, "первый вопрос"},
		{"archive, current chat is new", false, (*thread.Thread).Archive, false, "второй вопрос"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("r1", "reply", `{"text":"ок"}`)).then(raw("r2", "reply", `{"text":"ок"}`))
			first := ask(t, e, "первый вопрос")

			competeOnce(t, e, first.Thread, tt.move)
			in := AskInput{Scope: subjectScope(t), Text: "второй вопрос"}
			if tt.named {
				in.ThreadID = first.Thread
			}
			st, err := e.svc.Ask(t.Context(), in)
			if err != nil {
				t.Fatalf("Ask after a chat change = %v", err)
			}
			if st.Status != thread.StatusDone || (st.Thread == first.Thread) != tt.sameChat {
				t.Fatalf("step = %+v, want DONE in the same chat %v", st, tt.sameChat)
			}
			if n := len(e.model.requests()); n != 2 {
				t.Fatalf("Hub steps = %d, want 2", n)
			}
			th := storedThread(t, e, st.Thread)
			if th.Title() != tt.wantTitle || th.Archived() {
				t.Fatalf("chat title %q archived %v, want %q unarchived", th.Title(), th.Archived(), tt.wantTitle)
			}
			texts := 0
			for _, h := range e.history.all(st.Thread) {
				if h.Entry.StudentText == "второй вопрос" {
					texts++
				}
			}
			if texts != 1 {
				t.Fatalf("the message is stored %d times, want once", texts)
			}
		})
	}
}
