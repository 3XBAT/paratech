package agentturn

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// current reads the current request of th and checks that no Hub step was asked meanwhile.
func current(t *testing.T, e *env, th shared.ThreadID) (Step, bool, error) {
	t.Helper()
	before := len(e.model.requests())
	st, ok, err := e.svc.Current(t.Context(), th)
	if n := len(e.model.requests()); n != before {
		t.Fatalf("Current called the Hub: %d steps, want %d", n, before)
	}
	return st, ok, err
}

// TestCurrentRebuildsTheLiveCard: the request a chat waits on comes back with the card the step showed, from the stored
// state only; a request that ended or a chat without a request has none.
func TestCurrentRebuildsTheLiveCard(t *testing.T) {
	plan := `{"summary":"Создать папку","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}}],` +
		`"notes":["Папка в предмете Физика"]}`
	q := `{"questions":[{"kind":"choice","question":"Какой вариант?","options":["А","Б"],"default":0}]}`
	tests := []struct {
		name   string
		script func(m *fakeModel)
		then   func(t *testing.T, e *env, st Step) Step // moves the request after Ask; nil: none
		status thread.Status
		cards  bool // the step's cards come back as they were shown
	}{
		{name: "plan waits for Да", script: func(m *fakeModel) { m.then(raw("p1", "show_plan", plan)) },
			status: thread.StatusAwaitingPlan, cards: true},
		{name: "questions wait for answers", script: func(m *fakeModel) { m.then(raw("q1", "ask_questions", q)) },
			status: thread.StatusAwaitingAnswers, cards: true},
		{name: "second question round", script: func(m *fakeModel) {
			m.then(raw("q1", "ask_questions", q)).then(raw("q2", "ask_questions", q))
		}, then: func(t *testing.T, e *env, st Step) Step {
			st, err := e.svc.AnswerQuestions(t.Context(), ref(st), []values.Answer{{Question: 0, Option: 1}})
			if err != nil {
				t.Fatal(err)
			}
			return st
		}, status: thread.StatusAwaitingAnswers, cards: true},
		{name: "stopped offline, Продолжить", script: func(m *fakeModel) {
			m.fail(backoff.Transient(shared.ErrNetworkUnavailable))
		}, status: thread.StatusStopped, cards: true},
		{name: "stopped by the student", script: func(m *fakeModel) { m.then(raw("c1", "list_subjects", `{}`)) },
			then: func(t *testing.T, e *env, st Step) Step {
				st, err := e.svc.Stop(t.Context(), ref(st))
				if err != nil {
					t.Fatal(err)
				}
				return st
			}, status: thread.StatusStopped, cards: true},
		{name: "running, calls with the Driver", script: func(m *fakeModel) { m.then(raw("c1", "list_subjects", `{}`)) },
			status: thread.StatusPlanning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tt.script(e.model)
			shown := ask(t, e, "сделай")
			if tt.then != nil {
				shown = tt.then(t, e, shown)
			}

			got, ok, err := current(t, e, shown.Thread)
			if err != nil || !ok {
				t.Fatalf("Current = %v, %v", ok, err)
			}
			if got.Thread != shown.Thread || got.Turn != shown.Turn || got.Status != tt.status ||
				got.StopReason != shown.StopReason || got.Scope != shown.Scope || len(got.Calls) != 0 {
				t.Fatalf("Current = %+v, shown %+v", got, shown)
			}
			want := []values.Card{}
			if tt.cards {
				want = shown.Cards
			}
			if len(want) == 0 && len(got.Cards) == 0 {
				return
			}
			if !reflect.DeepEqual(got.Cards, want) {
				t.Fatalf("cards = %+v, shown %+v", got.Cards, want)
			}
		})
	}
}

func TestCurrentWithoutAWaitingRequest(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"Готово"}`))
	done := ask(t, e, "что есть")
	empty, err := thread.New("th9", time.Date(2026, 10, 22, 9, 0, 0, 0, moscow))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), empty); err != nil {
		t.Fatal(err)
	}

	for _, th := range []shared.ThreadID{done.Thread, "th9"} {
		if st, ok, err := current(t, e, th); ok || err != nil || len(st.Cards) != 0 {
			t.Fatalf("Current(%s) = %+v, %v, %v; want none", th, st, ok, err)
		}
	}
	if _, ok, err := current(t, e, "nope"); ok || !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Current of a missing chat = %v, %v", ok, err)
	}
	if _, ok, err := current(t, e, "a/b"); ok || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Current of a malformed id = %v, %v", ok, err)
	}
}

// A waiting request whose card call cannot be read is an error, never a missing chat or a request with no card.
func TestCurrentBrokenHistory(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("p1", "show_plan", `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`))
	st := ask(t, e, "добавь предмет")

	e.history.mu.Lock()
	e.history.reading = errors.New("disk")
	e.history.mu.Unlock()
	if _, ok, err := current(t, e, st.Thread); ok || err == nil || errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Current with a failed read = %v, %v", ok, err)
	}

	e.history.mu.Lock()
	e.history.reading, e.history.entries = nil, nil
	e.history.mu.Unlock()
	if _, ok, err := current(t, e, st.Thread); ok || !errors.Is(err, errNoCard) || errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Current without the echo = %v, %v", ok, err)
	}
}
