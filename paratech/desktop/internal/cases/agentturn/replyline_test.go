package agentturn

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// storedLine is the reply line of the stored current request of th1.
func storedLine(t *testing.T, e *env) (thread.Status, string) {
	t.Helper()
	th, err := e.threads.Get(t.Context(), "th1")
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	return turn.Status(), turn.ReplyLine()
}

// TestReplyLineIsSavedWhenTheRequestEnds: the one save of the case freezes the line of a request that became final
// (DONE, FAILED, CANCELLED, STOPPED) from its stored entries and those of the change, in the same save; a request that
// is not final keeps none.
func TestReplyLineIsSavedWhenTheRequestEnds(t *testing.T) {
	end := map[thread.Status]func(th *thread.Thread, now time.Time) error{
		thread.StatusDone:      func(th *thread.Thread, now time.Time) error { return th.Complete(now) },
		thread.StatusFailed:    func(th *thread.Thread, now time.Time) error { return th.Fail(now) },
		thread.StatusCancelled: func(th *thread.Thread, now time.Time) error { return th.Cancel(now) },
		thread.StatusStopped:   func(th *thread.Thread, now time.Time) error { return th.Stop(values.StopUser, now) },
		thread.StatusPlanning:  func(*thread.Thread, time.Time) error { return nil },
	}
	for status, move := range end {
		t.Run(string(status), func(t *testing.T) {
			e := newEnv(t)
			th, err := thread.New("th1", e.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := th.Ask(thread.Request{Text: "что горит"}, e.clock.Now()); err != nil {
				t.Fatal(err)
			}
			first := &change{th: th}
			first.add(values.HistoryTurn{StudentText: "что горит"})
			first.add(values.HistoryTurn{Calls: &values.SealedCalls{Calls: []values.RawCall{raw("c1", "list_assignments", `{}`)}, Seal: []byte("s")}})
			first.add(values.HistoryTurn{Results: []values.ToolResult{{CallID: "c1", ResultJSON: `{}`}}})
			if err := e.svc.save(t.Context(), first); err != nil {
				t.Fatal(err)
			}

			// The stored entries hold the first step; the change holds the echo of the reply, not yet appended.
			c, err := e.svc.load(t.Context(), TurnRef{Thread: "th1", Turn: 1}, "test")
			if err != nil {
				t.Fatal(err)
			}
			c.add(values.HistoryTurn{Calls: &values.SealedCalls{Calls: []values.RawCall{raw("c2", "reply", `{"text":"итог"}`)}, Seal: []byte("s")}})
			if err := move(c.th, e.clock.Now()); err != nil {
				t.Fatal(err)
			}
			if err := e.svc.save(t.Context(), c); err != nil {
				t.Fatal(err)
			}

			got, line := storedLine(t, e)
			want := "reply: итог\nactions: list_assignments ok"
			if status == thread.StatusPlanning {
				want = ""
			}
			if got != status || line != want {
				t.Fatalf("stored %s with line %q, want %s with %q", got, line, status, want)
			}
		})
	}
}

// TestReplyLineOfARequestThatEndsInTheCase: a reply ends the request with its line; «Отмена» of a plan card ends it
// with the plan line.
func TestReplyLineOfARequestThatEndsInTheCase(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "reply", `{"text":"Готово."}`))
	ask(t, e, "привет")
	if status, line := storedLine(t, e); status != thread.StatusDone || line != "reply: Готово." {
		t.Fatalf("stored %s, line %q", status, line)
	}

	e = newEnv(t)
	e.model.then(raw("p1", "show_plan", `{"summary":"Создать папку","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}}]}`))
	st := ask(t, e, "разложи")
	if status, line := storedLine(t, e); status != thread.StatusAwaitingPlan || line != "" {
		t.Fatalf("a request waiting for «Да»: %s, line %q", status, line)
	}
	if _, err := e.svc.Cancel(t.Context(), ref(st)); err != nil {
		t.Fatal(err)
	}
	if status, line := storedLine(t, e); status != thread.StatusCancelled || line != "plan: Создать папку" {
		t.Fatalf("cancelled: %s, line %q", status, line)
	}
}

// TestReplyLineOfAResumedRequest: a stopped request has its line; a later save of it keeps that line (never
// recomputed); «Продолжить» clears it and the real end freezes a fresh one.
func TestReplyLineOfAResumedRequest(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_assignments", `{}`)).then(raw("c2", "reply", `{"text":"Два задания."}`))
	st := ask(t, e, "что горит")
	if _, err := e.svc.Stop(t.Context(), ref(st)); err != nil {
		t.Fatal(err)
	}
	stopped := "no answer: the request ended without a reply\nactions: list_assignments error"
	if status, line := storedLine(t, e); status != thread.StatusStopped || line != stopped {
		t.Fatalf("stopped: %s, line %q", status, line)
	}

	// An entry stored later under the request does not change the frozen line.
	if err := e.history.Append(t.Context(), "th1", 1, values.HistoryTurn{Calls: &values.SealedCalls{
		Calls: []values.RawCall{raw("x1", "reply", `{"text":"другое"}`)}, Seal: []byte("s"),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Interrupt(t.Context(), SubmitInput{Turn: ref(st)}); err != nil {
		t.Fatal(err)
	}
	if _, line := storedLine(t, e); line != stopped {
		t.Fatalf("line after a second save = %q, want it kept", line)
	}

	// Interrupt answered c1 not done; «Продолжить» goes on and the reply ends the request.
	e2 := newEnv(t)
	e2.model.then(raw("c1", "list_assignments", `{}`)).then(raw("c2", "reply", `{"text":"Два задания."}`))
	st = ask(t, e2, "что горит")
	if _, err := e2.svc.Stop(t.Context(), ref(st)); err != nil {
		t.Fatal(err)
	}
	cont, err := e2.svc.Continue(t.Context(), ref(st))
	if err != nil {
		t.Fatal(err)
	}
	if status, line := storedLine(t, e2); status == thread.StatusStopped || line != "" {
		t.Fatalf("after «Продолжить»: %s, line %q", status, line)
	}
	if _, err := e2.svc.Submit(t.Context(), SubmitInput{Turn: ref(cont), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil {
		t.Fatal(err)
	}
	if status, line := storedLine(t, e2); status != thread.StatusDone || line != "reply: Два задания.\nactions: list_assignments ok" {
		t.Fatalf("at the real end: %s, line %q", status, line)
	}
}

// TestReplyLineHistoryFailure: a failed read of the history fails the save of an ended request and stores nothing; a
// stop is still stored, without a line, and its next save freezes it.
func TestReplyLineHistoryFailure(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_assignments", `{}`))
	st := ask(t, e, "что горит")
	broken := errors.New("disk read failed")

	c, err := e.svc.load(t.Context(), ref(st), "test")
	if err != nil {
		t.Fatal(err)
	}
	c.th.Abandon(values.RefuseNotDone, e.clock.Now())
	c.takeResults(e.svc)
	if err := c.th.Complete(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	e.history.reading = broken
	if err := e.svc.save(t.Context(), c); !errors.Is(err, broken) {
		t.Fatalf("save = %v, want the read error", err)
	}
	if status, line := storedLine(t, e); status != thread.StatusPlanning || line != "" {
		t.Fatalf("stored after the failed save: %s, line %q", status, line)
	}

	if _, err := e.svc.Stop(t.Context(), ref(st)); err != nil {
		t.Fatalf("Stop with an unreadable history: %v", err)
	}
	if status, line := storedLine(t, e); status != thread.StatusStopped || line != "" {
		t.Fatalf("stopped: %s, line %q", status, line)
	}
	e.history.reading = nil
	if _, err := e.svc.Interrupt(t.Context(), SubmitInput{Turn: ref(st)}); err != nil {
		t.Fatal(err)
	}
	if _, line := storedLine(t, e); line == "" {
		t.Fatal("the next save of the stopped request froze no line")
	}
}
