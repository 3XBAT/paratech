package agentturn

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
)

// Before every Hub step the panel learns the request (thread, turn, status, no cards), after the open step is stored:
// «Стоп» works during the first step of a new request (ADR-0026).
func TestProgressBeforeTheHubStep(t *testing.T) {
	p := &recProgress{}
	e := newEnvWith(t, p)
	p.threads = e.threads
	seen := 0
	check := func(req values.StepRequest) {
		steps, stored := p.got()
		if len(steps) != seen+1 {
			t.Fatalf("progress before step %d: %d updates", seen+1, len(steps))
		}
		st := steps[seen]
		if st.Thread != req.Thread || st.Turn != 1 || st.Scope != subjectScope(t) || st.Status != thread.StatusPlanning ||
			st.StopReason != "" || len(st.Calls) != 0 || len(st.Cards) != 0 {
			t.Fatalf("progress %d = %+v", seen+1, st)
		}
		if cur := stored[seen].Current; cur == nil || !cur.StepOpen {
			t.Fatalf("progress %d published before the open step was stored: %+v", seen+1, cur)
		}
		seen++
	}
	e.model.do(func(req values.StepRequest) (values.StepReply, error) {
		check(req)
		return reply(raw("c1", "list_subjects", `{}`)), nil
	}).do(func(req values.StepRequest) (values.StepReply, error) {
		check(req)
		return reply(raw("r1", "reply", `{"text":"один"}`)), nil
	})

	st := ask(t, e, "мои предметы")
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("model steps checked = %d", seen)
	}
	// Moves that call no Hub step publish nothing.
	if _, err := e.svc.Stop(t.Context(), ref(st)); err == nil {
		t.Fatal("Stop of a done request went through")
	}
	if steps, _ := p.got(); len(steps) != 2 {
		t.Fatalf("progress updates = %d", len(steps))
	}
}

// A local failure in the middle of a step stores the request stopped and returns that stored state with the error, so
// the Driver shows what is stored.
func TestAbortReturnsTheStoredRequest(t *testing.T) {
	e := newEnv(t)
	e.history.reading = errors.New("disk read failed")
	st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "мои предметы"})
	if err == nil {
		t.Fatal("Ask went through without its history")
	}
	if st.Thread != "th1" || st.Turn != 1 || st.Status != thread.StatusStopped || st.StopReason != values.StopOffline ||
		len(st.Cards) != 1 || st.Cards[0] != (values.StoppedCard{Reason: values.StopOffline}) {
		t.Fatalf("step of the aborted request = %+v", st)
	}
	th, _ := e.threads.Get(t.Context(), "th1")
	if turn, _ := th.Turn(); turn.Status() != thread.StatusStopped || !turn.StepOpen() {
		t.Fatalf("stored = %s, open step %v", turn.Status(), turn.StepOpen())
	}
}

// Owner report 2026-10-24: after «нет связи» and «Повторить» on a new question the planner answered the previous one.
// The desktop sends every attempt of the new request with the new text last and the same history; the earlier request
// goes as its text only, without its answer (thread.Window), so this pins the request shape the Hub gets: the repeats
// change nothing on the desktop side.
func TestAttemptsOfANewRequestEndWithItsText(t *testing.T) {
	tests := []struct {
		name  string
		fail  error
		again func(e *env, r TurnRef) (Step, error)
	}{
		{"offline, Продолжить", backoff.Transient(errors.New("hub restarted")), func(e *env, r TurnRef) (Step, error) {
			return e.svc.Continue(t.Context(), r)
		}},
		{"failed, Повторить", backoff.Permanent(errors.New("hub refused")), func(e *env, r TurnRef) (Step, error) {
			return e.svc.Retry(t.Context(), r)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("r1", "reply", `{"text":"горутина — это"}`)).
				fail(tt.fail).fail(tt.fail).
				then(raw("r2", "reply", `{"text":"рыбаков — это"}`))
			ask(t, e, "что такое горутина")
			st := ask(t, e, "кто такой рыбаков")
			for range 2 {
				var err error
				if st, err = tt.again(e, ref(st)); err != nil {
					t.Fatal(err)
				}
			}
			if st.Status != thread.StatusDone || st.Turn != 2 {
				t.Fatalf("after two repeats = %+v", st)
			}
			reqs := e.model.requests()
			if len(reqs) != 4 {
				t.Fatalf("requests = %d", len(reqs))
			}
			for i, req := range reqs[1:] {
				h := req.History
				if len(h) != 2 || h[0].StudentText != "что такое горутина" || h[1].StudentText != "кто такой рыбаков" {
					t.Fatalf("attempt %d of the new request = %+v", i+1, h)
				}
			}
		})
	}
}
