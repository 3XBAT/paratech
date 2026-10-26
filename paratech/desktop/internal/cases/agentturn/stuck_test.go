package agentturn

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// storedTurn is the current request as stored.
func storedTurn(t *testing.T, e *env, id shared.ThreadID) thread.Turn {
	t.Helper()
	th, err := e.threads.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	return turn
}

func (h *memHistory) setFailing(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failing = err
}

// A failure after the results of a step were stored never leaves the request PLANNING or EXECUTING: it is stored
// stopped as «нет связи», so the next request of the thread is not busy (TASK-171).
func TestStepFailureStopsTheRequest(t *testing.T) {
	errDisk := errors.New("disk full")
	tests := []struct {
		name string
		// arm prepares the failure of the step after Submit and returns the ctx of Submit.
		arm    func(t *testing.T, e *env) context.Context
		disarm func(e *env)
		hub    int // Hub calls of the failed Submit
	}{
		{"base fails", func(t *testing.T, e *env) context.Context {
			e.subjects.fail(errDisk)
			return t.Context()
		}, func(e *env) { e.subjects.fail(nil) }, 0},
		{"save after the step fails", func(t *testing.T, e *env) context.Context {
			e.model.do(func(values.StepRequest) (values.StepReply, error) {
				e.history.setFailing(errDisk)
				return reply(raw("r1", "reply", `{"text":"ок"}`)), nil
			})
			return t.Context()
		}, func(e *env) { e.history.setFailing(nil) }, 1},
		{"the step panics", func(t *testing.T, e *env) context.Context {
			e.model.do(func(values.StepRequest) (values.StepReply, error) { panic("boom") })
			return t.Context()
		}, func(*env) {}, 1},
		{"the caller goes away", func(t *testing.T, e *env) context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			e.model.do(func(values.StepRequest) (values.StepReply, error) {
				cancel()
				return values.StepReply{}, context.Canceled
			})
			return ctx
		}, func(*env) {}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("c1", "list_subjects", `{}`))
			st := ask(t, e, "мои предметы")
			ctx := tt.arm(t, e)

			if _, err := e.svc.Submit(ctx, SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{"subjects":[]}`)}}); err == nil {
				t.Fatal("Submit succeeded")
			}
			if n := len(e.model.requests()); n != 1+tt.hub {
				t.Fatalf("Hub calls = %d, want %d", n, 1+tt.hub)
			}
			turn := storedTurn(t, e, st.Thread)
			if turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline || len(turn.OpenCalls()) != 0 {
				t.Fatalf("stored = %s %s, open %+v", turn.Status(), turn.StopReason(), turn.OpenCalls())
			}

			tt.disarm(e)
			e.model.then(raw("r2", "reply", `{"text":"готово"}`))
			next, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "ещё раз"})
			if err != nil || next.Turn != 2 || next.Status != thread.StatusDone {
				t.Fatalf("next Ask = %+v, %v", next, err)
			}
		})
	}
}

// A save that fails after the Hub answered keeps the step open: «Продолжить» replays its key (free on the Hub) and
// the request ends with the reply it dropped.
func TestFailedSaveAfterStepContinues(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_subjects", `{}`)).do(func(values.StepRequest) (values.StepReply, error) {
		e.history.setFailing(errors.New("disk full"))
		return reply(raw("r1", "reply", `{"text":"ок"}`)), nil
	})
	st := ask(t, e, "мои предметы")
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err == nil {
		t.Fatal("Submit succeeded")
	}
	e.history.setFailing(nil)
	got, err := e.svc.Continue(t.Context(), ref(st))
	if err != nil || got.Status != thread.StatusDone || len(got.Cards) != 1 || got.Cards[0].Kind() != values.CardReply {
		t.Fatalf("Continue = %+v, %v", got, err)
	}
	reqs := e.model.requests()
	if len(reqs) != 3 || reqs[1].Key != reqs[2].Key {
		t.Fatalf("requests = %d, keys %q", len(reqs), reqs[len(reqs)-1].Key)
	}
}

// A base that fails for the first step of a new request stores nothing: the thread stays as it was, not busy.
func TestBaseFailureOfANewRequest(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "привет")
	e.subjects.fail(errors.New("db locked"))
	if _, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "ещё"}); err == nil {
		t.Fatal("Ask succeeded")
	}
	if turn := storedTurn(t, e, st.Thread); turn.Number() != 1 || turn.Status() != thread.StatusDone {
		t.Fatalf("stored = %d %s", turn.Number(), turn.Status())
	}
	e.subjects.fail(nil)
	e.model.then(raw("r2", "reply", `{"text":"ок"}`))
	if got, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "ещё"}); err != nil || got.Status != thread.StatusDone {
		t.Fatalf("Ask = %+v, %v", got, err)
	}
}

// The owner's hypothesis (TASK-171): the caption of a recording pasted as the request (ellipsis, middle dots, maybe a
// line break) is a valid request text and its step completes.
func TestRecordingCaptionAsRequest(t *testing.T) {
	for _, text := range []string{
		"Автоматизация… (Лекция) · 20.10.2026, 15:59:29 · Длительность 1:20:00",
		"Автоматизация… (Лекция)\n20.10.2026, 15:59:29 · Длительность 1:20:00\n",
		"Автоматизация… (Лекция) · 20.10.2026, 15:59:29\r\n· Длительность 1:20:00",
	} {
		if _, err := values.NewStudentText(text, nil); err != nil {
			t.Fatalf("NewStudentText(%q) = %v", text, err)
		}
		e := newEnv(t)
		e.model.then(raw("r1", "reply", `{"text":"Это запись лекции."}`))
		st, err := e.svc.Ask(t.Context(), AskInput{Text: text})
		if err != nil || st.Status != thread.StatusDone {
			t.Fatalf("Ask(%q) = %+v, %v", text, st, err)
		}
		if h := e.history.all(st.Thread); len(h) == 0 || h[0].Entry.StudentText == "" {
			t.Fatalf("stored history = %+v", h)
		}
		e.model.then(raw("r2", "reply", `{"text":"ок"}`))
		if _, err := e.svc.Ask(t.Context(), AskInput{Text: "а что ещё?"}); err != nil {
			t.Fatalf("next Ask = %v", err)
		}
	}
}

// RecoverIdle stops a request left running only when no loop of the process drives its thread; the calls handed to
// the Driver become outcome_unknown, never run again, and the next request passes.
func TestRecoverIdle(t *testing.T) {
	noLoop := func(shared.ThreadID) bool { return false }
	tests := []struct {
		name    string
		in      func(st Step) IdleInput
		live    func(shared.ThreadID) bool
		stopped bool
	}{
		{"by scope, no loop", func(Step) IdleInput { return IdleInput{Scope: subjectScope(t)} }, noLoop, true},
		{"by thread, no loop", func(st Step) IdleInput { return IdleInput{Thread: st.Thread} }, noLoop, true},
		{"a live loop", func(st Step) IdleInput { return IdleInput{Thread: st.Thread} },
			func(shared.ThreadID) bool { return true }, false},
		{"another subject", func(Step) IdleInput { return IdleInput{} }, noLoop, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("w1", "create_subject", `{"name":"Химия"}`))
			st := ask(t, e, "создай предмет")

			ok, err := e.svc.RecoverIdle(t.Context(), tt.in(st), tt.live)
			if err != nil || ok != tt.stopped {
				t.Fatalf("RecoverIdle = %v, %v; want %v", ok, err, tt.stopped)
			}
			turn := storedTurn(t, e, st.Thread)
			if !tt.stopped {
				if turn.Status() != thread.StatusExecuting || len(turn.OpenCalls()) != 1 {
					t.Fatalf("a running request changed: %s %+v", turn.Status(), turn.OpenCalls())
				}
				return
			}
			if turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline || len(turn.OpenCalls()) != 0 {
				t.Fatalf("stored = %s %s %+v", turn.Status(), turn.StopReason(), turn.OpenCalls())
			}
			all := e.history.all(st.Thread)
			if last := all[len(all)-1].Entry; len(last.Results) != 1 || last.Results[0].ResultJSON != `{"error":"outcome_unknown"}` {
				t.Fatalf("last history entry = %+v", last)
			}
			if ok, err := e.svc.RecoverIdle(t.Context(), tt.in(st), noLoop); ok || err != nil {
				t.Fatalf("second RecoverIdle = %v, %v", ok, err)
			}
			e.model.then(raw("r1", "reply", `{"text":"ок"}`))
			next, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "ещё"})
			if err != nil || next.Turn != 2 || len(next.Calls) != 0 {
				t.Fatalf("next Ask = %+v, %v", next, err)
			}
		})
	}
	e := newEnv(t)
	if ok, err := e.svc.RecoverIdle(t.Context(), IdleInput{Thread: "nope"}, noLoop); ok || err != nil {
		t.Fatalf("RecoverIdle of a missing thread = %v, %v", ok, err)
	}
}

// A «Стоп» saved while RecoverIdle looks (Stop does not take the lock of the case) wins: the request no longer runs,
// so RecoverIdle reports false without an error and the student's reason stays.
func TestRecoverIdleAfterAStopInBetween(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("w1", "create_subject", `{"name":"Химия"}`))
	st := ask(t, e, "создай предмет")
	fired := false
	e.threads.afterGet = func() {
		if fired {
			return
		}
		fired = true
		if _, err := e.svc.Stop(context.Background(), ref(st)); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
	ok, err := e.svc.RecoverIdle(t.Context(), IdleInput{Thread: st.Thread}, func(shared.ThreadID) bool { return false })
	if ok || err != nil {
		t.Fatalf("RecoverIdle = %v, %v", ok, err)
	}
	if turn := storedTurn(t, e, st.Thread); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopUser {
		t.Fatalf("stored = %s %s", turn.Status(), turn.StopReason())
	}
}

// Interrupt closes the request whatever the report holds (TASK-171): an outcome of a call that is not open for the
// Driver is skipped, and one the thread refuses answers its call outcome_unknown, never not done, since it ran.
func TestInterruptKeepsWhatItCan(t *testing.T) {
	done := func(t *testing.T) Outcome {
		return Outcome{Result: values.ToolResult{CallID: "c1", ResultJSON: `{"changed":true,"done":true,"id":"s9"}`},
			Created: mustRef(t, values.KindSubject, "s9")}
	}
	tests := []struct {
		name     string
		outcomes func(t *testing.T) []Outcome
		first    func(t *testing.T) string // the result of c1 the planner gets
	}{
		{"a call answered already", func(t *testing.T) []Outcome {
			return []Outcome{done(t), okResult("c1", `{"changed":false}`)}
		}, func(t *testing.T) string { return done(t).Result.ResultJSON }},
		{"a call that is not open", func(t *testing.T) []Outcome {
			return []Outcome{okResult("zz", `{}`), done(t)}
		}, func(t *testing.T) string { return done(t).Result.ResultJSON }},
		{"an outcome the thread refuses", func(t *testing.T) []Outcome {
			// A failed result cannot create an ID: the thread refuses it, yet the action ran.
			return []Outcome{{Result: values.RefusalResult("c1", values.RefuseFailed), Created: mustRef(t, values.KindSubject, "s9")}}
		}, func(*testing.T) string { return `{"error":"outcome_unknown"}` }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("c1", "create_subject", `{"name":"Химия"}`), raw("c2", "list_subjects", `{}`)).
				then(raw("r1", "reply", `{"text":"ок"}`))
			st := ask(t, e, "добавь химию")
			got, err := e.svc.Interrupt(t.Context(), SubmitInput{Turn: ref(st), Outcomes: tt.outcomes(t)})
			if err != nil || got.Status != thread.StatusStopped || got.StopReason != values.StopOffline {
				t.Fatalf("Interrupt = %+v, %v", got, err)
			}
			if _, err := e.svc.Continue(t.Context(), ref(st)); err != nil {
				t.Fatal(err)
			}
			res := lastResults(t, e)
			if len(res) != 2 || res[0].ResultJSON != tt.first(t) || res[1].ResultJSON != `{"error":"not_done"}` {
				t.Fatalf("results sent = %+v", res)
			}
		})
	}
}
