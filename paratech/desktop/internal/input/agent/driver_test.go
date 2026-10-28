package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

func newDriver(t *testing.T, r *reads, turns Turns) (*Driver, *chanSink) {
	t.Helper()
	sink := &chanSink{}
	d, err := New(r.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, sink
}

const th1 = shared.ThreadID("th1")

func step(status thread.Status, calls []agentturn.Call, cards ...values.Card) agentturn.Step {
	return agentturn.Step{Thread: th1, Turn: 1, Status: status, Calls: calls, Cards: cards}
}

func TestNewChecksDependencies(t *testing.T) {
	r := newReads()
	d := r.deps(&scriptedTurns{}, &chanSink{})
	d.Deliveries = nil
	if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without deliveries = %v", err)
	}
	if _, err := NewFacts(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewFacts(nil) = %v", err)
	}
	if _, err := NewBaseSubjects(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewBaseSubjects(nil) = %v", err)
	}
}

// The loop: the step's read call runs, its result goes back with Submit, and the final reply card is shown.
func TestDriverRunsReadCallsAndShowsCards(t *testing.T) {
	r := newReads()
	r.library.subjects = []*subject.Subject{newSubject(t, "s1", "Физика")}
	turns := (&scriptedTurns{}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
		then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Предметов: 1"}), nil)
	d, sink := newDriver(t, r, turns)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	submitted := rep.submitted
	if len(submitted) != 1 || len(submitted[0].Outcomes) != 1 || len(rep.cancelled) != 0 || len(rep.stopped) != 0 {
		t.Fatalf("report %+v", rep)
	}
	out := submitted[0].Outcomes[0]
	if out.Result.CallID != "c1" || out.Result.IsError || !strings.Contains(out.Result.ResultJSON, `"id":"s1"`) ||
		len(out.Seen) != 1 || out.Seen[0] != mustRef(t, values.KindSubject, "s1") {
		t.Fatalf("outcome = %+v", out)
	}
	if submitted[0].Turn != (agentturn.TurnRef{Thread: th1, Turn: 1}) {
		t.Fatalf("turn = %+v", submitted[0].Turn)
	}
	ups := sink.all()
	if len(ups) != 2 || ups[1].Status != thread.StatusDone || len(ups[1].Cards) != 1 || ups[1].Cards[0].Kind() != values.CardReply {
		t.Fatalf("updates = %+v", ups)
	}
}

// Tools that are not plain reads are not run by this desktop yet: the planner gets "unavailable", nothing else runs.
func TestDriverAnswersUnavailableTools(t *testing.T) {
	tests := []struct {
		name string
		call agentturn.Call
	}{
		{"paid reader", agentturn.Call{Call: mustCall(t, "c1", values.ToolAnswerQuestion, `{"question":"Что такое ряд?"}`),
			Class: actionpolicy.ClassR, Paid: true, PlanStep: -1}},
		{"upload reader", agentturn.Call{Call: mustCall(t, "c1", values.ToolInspectUpload, `{"upload":"u1"}`),
			Class: actionpolicy.ClassR, Paid: true, PlanStep: -1}},
		{"reversible action", agentturn.Call{Call: mustCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`),
			Class: actionpolicy.ClassU, PlanStep: -1}},
		{"proposal", agentturn.Call{Call: mustCall(t, "c1", values.ToolProposeDeadline, `{"assignment":"a1"}`),
			Class: actionpolicy.ClassC, PlanStep: -1}},
		{"read tool not classified R", agentturn.Call{Call: mustCall(t, "c1", values.ToolListSubjects, `{}`),
			Class: actionpolicy.ClassU, PlanStep: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newReads()
			turns := (&scriptedTurns{}).
				then(step(thread.StatusExecuting, []agentturn.Call{tt.call}), nil).
				then(step(thread.StatusDone, nil), nil)
			d, _ := newDriver(t, r, turns)
			if err := d.Ask(t.Context(), agentturn.AskInput{Text: "сделай"}); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			submitted := turns.report().submitted
			if len(submitted) != 1 {
				t.Fatalf("submitted %+v", submitted)
			}
			res := submitted[0].Outcomes[0].Result
			if !res.IsError || res.ResultJSON != `{"error":"unavailable"}` {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

// Errors of the step use case reach the panel as an enum, never as text, in an update that names the request the
// button acted on.
func TestDriverPublishesProblems(t *testing.T) {
	tests := []struct {
		err  error
		want Problem
	}{
		{thread.ErrTurnInProgress, ProblemBusy},
		{agentturn.ErrStaleTurn, ProblemStale},
		{thread.ErrBudgetExceeded, ProblemBudget},
		{thread.ErrRetryRequired, ProblemRetry},
		{thread.ErrRestartRequired, ProblemRestart},
		{fmt.Errorf("agentturn: rerun: %w", thread.ErrRerunBlocked), ProblemRerun},
		{fmt.Errorf("%w: %w: planning", thread.ErrTurnInProgress, thread.ErrInvalidTransition), ProblemBusy},
		{agentturn.ErrInvalidInput, ProblemInvalid},
		{thread.ErrInvalidTransition, ProblemInvalid},
		{errors.New("disk full"), ProblemFailed},
	}
	for _, tt := range tests {
		turns := (&scriptedTurns{}).then(agentturn.Step{}, tt.err)
		d, sink := newDriver(t, newReads(), turns)
		if err := d.Continue(t.Context(), agentturn.TurnRef{Thread: th1, Turn: 1}); err != nil {
			t.Fatal(err)
		}
		d.Wait()
		if ups := sink.all(); len(ups) != 1 || ups[0].Problem != tt.want || ups[0].Thread != th1 || ups[0].Turn != 1 {
			t.Errorf("%v: updates %+v", tt.err, ups)
		}
	}
}

// A failed Ask has no thread yet: its update names the scope; a failed Submit names the request of the step.
func TestDriverProblemNamesTheRequest(t *testing.T) {
	scope := subjectScope(t, "s1")
	turns := (&scriptedTurns{}).then(agentturn.Step{}, thread.ErrTurnInProgress)
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Scope: scope, Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if ups := sink.all(); len(ups) != 1 || ups[0].Problem != ProblemBusy || ups[0].Scope != scope || ups[0].Thread != "" {
		t.Fatalf("Ask updates %+v", ups)
	}

	first := step(thread.StatusExecuting, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)})
	first.Scope = scope
	turns = (&scriptedTurns{}).then(first, nil).then(agentturn.Step{}, errors.New("disk full"))
	d, sink = newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Scope: scope, Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	ups := sink.all()
	if len(ups) != 2 || ups[1].Problem != ProblemFailed || ups[1].Thread != th1 || ups[1].Turn != 1 || ups[1].Scope != scope {
		t.Fatalf("Submit updates %+v", ups)
	}
}

// A card that breaks its rules (too long, a bidi override) is dropped, never shown. Angle brackets are no longer a
// break in a reply (catalog v7): the page shows its text as plain text.
func TestDriverDropsInvalidCards(t *testing.T) {
	turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil,
		values.ReplyCard{Text: values.Text(strings.Repeat("я", values.MaxReplyTextRunes+1))},
		values.ReplyCard{Text: "a\u202eb"}, values.ReplyCard{Text: "Готово"},
		values.QuestionsCard{Round: 3}, nil), nil)
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	ups := sink.all()
	if len(ups) != 1 || len(ups[0].Cards) != 1 || ups[0].Cards[0].(values.ReplyCard).Text != "Готово" {
		t.Fatalf("updates = %+v", ups)
	}
}

// The stopped card of a lost connection reaches the panel with the request (offline: «Не завершено: нет связи ·
// Продолжить»); «Продолжить» runs the loop again.
func TestDriverOfflineStopAndContinue(t *testing.T) {
	stopped := agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusStopped, StopReason: values.StopOffline,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopOffline}}}
	turns := (&scriptedTurns{}).then(stopped, nil).then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Готово"}), nil)
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	ups := sink.all()
	if len(ups) != 1 || ups[0].Status != thread.StatusStopped || ups[0].StopReason != values.StopOffline ||
		len(ups[0].Cards) != 1 || ups[0].Cards[0] != (values.StoppedCard{Reason: values.StopOffline}) {
		t.Fatalf("offline update = %+v", ups)
	}
	if err := d.Continue(t.Context(), agentturn.TurnRef{Thread: th1, Turn: 1}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if ups := sink.all(); len(ups) != 2 || ups[1].Status != thread.StatusDone {
		t.Fatalf("after Continue = %+v", ups)
	}
}

// Cancelling the ctx while a call runs ends the loop: the call is not reported, the request stops continuable (not
// cancelled) through a context that still works, the panel gets the stopped request, and no goroutine is left (Wait
// returns; run with -race).
func TestDriverCancelViaContext(t *testing.T) {
	r := newReads()
	r.library.block, r.library.started = true, make(chan struct{})
	turns := (&scriptedTurns{}).then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil)
	d, sink := newDriver(t, r, turns)

	ctx, cancel := context.WithCancel(t.Context())
	if err := d.Ask(ctx, agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	<-r.library.started
	cancel()
	d.Wait()

	rep := turns.report()
	if len(rep.submitted) != 0 || len(rep.cancelled) != 0 || len(rep.stopped) != 1 ||
		rep.stopped[0] != (agentturn.TurnRef{Thread: th1, Turn: 1}) {
		t.Fatalf("report %+v", rep)
	}
	if rep.opCtx[0] != nil {
		t.Fatalf("Interrupt got a dead context: %v", rep.opCtx[0])
	}
	ups := sink.all()
	if len(ups) != 2 || ups[1].Status != thread.StatusStopped || ups[1].StopReason != values.StopOffline {
		t.Fatalf("updates = %+v", ups)
	}
}

// The ctx ends while the step use case answers with calls: they are never run, the request stops continuable.
func TestDriverContextEndsBeforeCalls(t *testing.T) {
	r := newReads()
	turns := (&scriptedTurns{}).then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil)
	ctx, cancel := context.WithCancel(t.Context())
	turns.beforeAnswer = cancel
	d, _ := newDriver(t, r, turns)
	if err := d.Ask(ctx, agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.submitted) != 0 || len(rep.cancelled) != 0 || len(rep.stopped) != 1 {
		t.Fatalf("report %+v", rep)
	}
}

// The ctx ends while the step use case answers without calls: the stored step still reaches the panel.
func TestDriverContextEndsWithFinalStep(t *testing.T) {
	turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Готово"}), nil)
	ctx, cancel := context.WithCancel(t.Context())
	turns.beforeAnswer = cancel
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(ctx, agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if ups := sink.all(); len(ups) != 1 || ups[0].Status != thread.StatusDone {
		t.Fatalf("updates = %+v", ups)
	}
	if rep := turns.report(); len(rep.stopped) != 0 || len(rep.cancelled) != 0 {
		t.Fatalf("report %+v", rep)
	}
}

// Close cancels a running loop, waits for it and refuses new requests; the request stays continuable.
func TestDriverClose(t *testing.T) {
	r := newReads()
	r.library.block, r.library.started = true, make(chan struct{})
	turns := (&scriptedTurns{}).then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil)
	d, _ := newDriver(t, r, turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	<-r.library.started
	d.Close()
	if rep := turns.report(); len(rep.stopped) != 1 || len(rep.cancelled) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "ещё"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ask after Close = %v", err)
	}
}

// A step use case that never ends a request (a broken invariant) cannot keep the loop going: it stops after the step
// budget of a request with an approved plan (the larger one, so a long plan is never cut by the guard) and cancels
// the request.
func TestDriverLoopIsBounded(t *testing.T) {
	turns := &scriptedTurns{endless: step(thread.StatusExecuting, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)})}
	d, _ := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.submitted) != thread.MaxPlanRunSteps || len(rep.cancelled) != 1 || len(rep.stopped) != 0 {
		t.Fatalf("submitted %d, cancelled %d, stopped %d", len(rep.submitted), len(rep.cancelled), len(rep.stopped))
	}
}

// Every button of the panel goes through the step use case and publishes its result.
func TestDriverButtons(t *testing.T) {
	ref := agentturn.TurnRef{Thread: th1, Turn: 1}
	ops := map[string]func(d *Driver) error{
		"approve": func(d *Driver) error { return d.ApprovePlan(t.Context(), ref) },
		"revise":  func(d *Driver) error { return d.RevisePlan(t.Context(), ref, "без лабораторных") },
		"answer": func(d *Driver) error {
			return d.AnswerQuestions(t.Context(), ref, []values.Answer{{Question: 0, Option: 1}})
		},
		"stop":     func(d *Driver) error { return d.Stop(t.Context(), ref) },
		"continue": func(d *Driver) error { return d.Continue(t.Context(), ref) },
		"retry":    func(d *Driver) error { return d.Retry(t.Context(), ref) },
		"restart":  func(d *Driver) error { return d.Restart(t.Context(), ref) },
		"cancel":   func(d *Driver) error { return d.Cancel(t.Context(), ref) },
	}
	for name, op := range ops {
		turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil), nil)
		d, sink := newDriver(t, newReads(), turns)
		if err := op(d); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		d.Wait()
		if ups := sink.all(); len(ups) != 1 || ups[0].Thread != th1 {
			t.Errorf("%s: updates %+v", name, ups)
		}
	}
}
