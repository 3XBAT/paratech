package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// stuckTurns keeps the one request of thread th1 like agentturn does, enough for TASK-171: a running request refuses
// Ask and Rerun as busy, Interrupt and Stop stop it, RecoverIdle stops it unless a loop is live.
type stuckTurns struct {
	Turns // the other operations are not used

	mu        sync.Mutex
	turn      int
	status    thread.Status
	reason    values.StopReason
	calls     []agentturn.Call // what the next Ask hands to the Driver
	submitErr error            // Submit fails with it and changes nothing
	noRecover bool             // RecoverIdle reports a stop but the request stays running (a broken recovery)
	busy      chan struct{}    // closed when Ask is first refused as busy
	asks      int
	submits   int
	recovered int
	idleLive  []bool // what live said in each RecoverIdle
}

func (s *stuckTurns) stepLocked() agentturn.Step {
	st := agentturn.Step{Thread: th1, Turn: s.turn, Status: s.status, StopReason: s.reason}
	if s.status == thread.StatusStopped {
		st.Cards = []values.Card{values.StoppedCard{Reason: s.reason}}
	}
	return st
}

func (s *stuckTurns) Ask(context.Context, agentturn.AskInput) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if running(s.status) {
		if s.busy != nil {
			close(s.busy)
			s.busy = nil
		}
		return agentturn.Step{}, fmt.Errorf("agentturn: ask: %w", thread.ErrTurnInProgress)
	}
	s.asks++
	return s.startLocked(), nil
}

func (s *stuckTurns) startLocked() agentturn.Step {
	s.turn++
	s.reason = ""
	if len(s.calls) == 0 {
		s.status = thread.StatusDone
		return s.stepLocked()
	}
	s.status = thread.StatusExecuting
	st := s.stepLocked()
	st.Calls = s.calls
	return st
}

func (s *stuckTurns) Rerun(_ context.Context, ref agentturn.TurnRef) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if running(s.status) {
		return agentturn.Step{}, fmt.Errorf("agentturn: rerun: %w: %w", thread.ErrTurnInProgress, thread.ErrInvalidTransition)
	}
	if ref.Turn != s.turn {
		return agentturn.Step{}, agentturn.ErrStaleTurn
	}
	s.calls = nil
	return s.startLocked(), nil
}

func (s *stuckTurns) Submit(context.Context, agentturn.SubmitInput) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submits++
	if s.submitErr != nil {
		return agentturn.Step{}, s.submitErr
	}
	s.status = thread.StatusDone
	return s.stepLocked(), nil
}

func (s *stuckTurns) Stop(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !running(s.status) {
		return agentturn.Step{}, thread.ErrInvalidTransition
	}
	s.status, s.reason = thread.StatusStopped, values.StopUser
	return s.stepLocked(), nil
}

func (s *stuckTurns) Interrupt(context.Context, agentturn.SubmitInput) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.status == thread.StatusStopped:
	case running(s.status):
		s.status, s.reason = thread.StatusStopped, values.StopOffline
	default:
		return agentturn.Step{}, thread.ErrInvalidTransition
	}
	return s.stepLocked(), nil
}

func (s *stuckTurns) RecoverIdle(_ context.Context, _ agentturn.IdleInput, live func(shared.ThreadID) bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !running(s.status) {
		return false, nil
	}
	l := live(th1)
	s.idleLive = append(s.idleLive, l)
	if l {
		return false, nil
	}
	s.recovered++
	if !s.noRecover {
		s.status, s.reason = thread.StatusStopped, values.StopOffline
	}
	return true, nil
}

type stuckReport struct {
	status                    thread.Status
	reason                    values.StopReason
	turn, asks, submits, recs int
	idleLive                  []bool
}

func (s *stuckTurns) report() stuckReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stuckReport{status: s.status, reason: s.reason, turn: s.turn, asks: s.asks, submits: s.submits,
		recs: s.recovered, idleLive: slices.Clone(s.idleLive)}
}

func lastUpdate(t *testing.T, sink *chanSink) Update {
	t.Helper()
	ups := sink.all()
	if len(ups) == 0 {
		t.Fatal("no update")
	}
	return ups[len(ups)-1]
}

// A failed Submit closes the request (stopped offline, continuable) instead of leaving it running without a loop; the
// next request passes without any recovery, and the call that ran is not run again.
func TestSubmitErrorClosesTheRequest(t *testing.T) {
	r := newReads()
	turns := &stuckTurns{calls: []agentturn.Call{readCall(t, "c1", values.ToolJobStatus, `{"ids":["j1"]}`)},
		submitErr: errors.New("disk full")}
	d, sink := newDriver(t, r, turns)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "как дела с работой"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	rep := turns.report()
	if rep.status != thread.StatusStopped || rep.reason != values.StopOffline || rep.submits != 1 {
		t.Fatalf("after a failed Submit: %+v", rep)
	}
	if up := lastUpdate(t, sink); up.Status != thread.StatusStopped || up.Problem != ProblemFailed || up.Thread != th1 {
		t.Fatalf("last update = %+v", up)
	}

	turns.mu.Lock()
	turns.calls, turns.submitErr = nil, nil
	turns.mu.Unlock()
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "ещё"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); rep.asks != 2 || rep.recs != 0 || rep.status != thread.StatusDone {
		t.Fatalf("next Ask: %+v", rep)
	}
	if len(r.activity.asked) != 1 {
		t.Fatalf("job_status ran %d times", len(r.activity.asked))
	}
}

// A request left running by a loop that is gone (no loop of this process drives the thread): Ask and Rerun recover it
// once and pass; nothing is run for the old request.
func TestBusyWithoutALoopIsRecoveredOnce(t *testing.T) {
	tests := []struct {
		name string
		op   func(d *Driver) error
	}{
		{"ask", func(d *Driver) error { return d.Ask(context.Background(), agentturn.AskInput{Text: "привет"}) }},
		{"rerun", func(d *Driver) error { return d.Rerun(context.Background(), ref1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newReads()
			turns := &stuckTurns{turn: 1, status: thread.StatusExecuting}
			d, sink := newDriver(t, r, turns)
			if err := tt.op(d); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			rep := turns.report()
			if rep.recs != 1 || rep.turn != 2 || rep.status != thread.StatusDone || !slices.Equal(rep.idleLive, []bool{false}) {
				t.Fatalf("report %+v", rep)
			}
			if up := lastUpdate(t, sink); up.Problem != ProblemNone || up.Turn != 2 || up.Status != thread.StatusDone {
				t.Fatalf("last update = %+v", up)
			}
			if slices.ContainsFunc(sink.all(), func(u Update) bool { return u.Problem == ProblemBusy }) {
				t.Fatalf("busy shown: %+v", sink.all())
			}
			if len(r.activity.asked) != 0 {
				t.Fatalf("a call ran: %v", r.activity.asked)
			}
		})
	}
}

// The recovery runs once per operation: a request that stays running after it is still busy.
func TestRecoveryIsTriedOnce(t *testing.T) {
	turns := &stuckTurns{turn: 1, status: thread.StatusExecuting, noRecover: true}
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); rep.recs != 1 || rep.asks != 0 {
		t.Fatalf("report %+v", rep)
	}
	if up := lastUpdate(t, sink); up.Problem != ProblemBusy {
		t.Fatalf("last update = %+v", up)
	}
}

// A tool that hangs keeps its loop live: a new request is busy and nothing is recovered. «Стоп» cuts the tool, the
// loop closes the request, and the next request passes; the hung call never runs again.
func TestHungToolStopsAndTheNextRequestPasses(t *testing.T) {
	r := newReads()
	r.library.block, r.library.started = true, make(chan struct{})
	busy := make(chan struct{})
	turns := &stuckTurns{calls: []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}, busy: busy}
	d, sink := newDriver(t, r, turns)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	<-r.library.started
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "ещё"}); err != nil {
		t.Fatal(err)
	}
	<-busy
	if err := d.Stop(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if rep.status != thread.StatusStopped || rep.reason != values.StopUser || rep.recs != 0 || rep.submits != 0 {
		t.Fatalf("after «Стоп»: %+v", rep)
	}
	if slices.Contains(rep.idleLive, false) { // empty when «Стоп» landed before the recovery looked
		t.Fatalf("live loop not seen: %v", rep.idleLive)
	}
	if !slices.ContainsFunc(sink.all(), func(u Update) bool { return u.Problem == ProblemBusy }) {
		t.Fatalf("no busy update: %+v", sink.all())
	}

	turns.mu.Lock()
	turns.calls = nil
	turns.mu.Unlock()
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "ещё раз"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); rep.asks != 2 || rep.status != thread.StatusDone || rep.recs != 0 {
		t.Fatalf("next Ask: %+v", rep)
	}
}

// Submit that fails with the stored request stopped (the use case aborted) or of an older request needs no Interrupt.
func TestSubmitErrorOfAClosedRequest(t *testing.T) {
	stopped := agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusStopped, StopReason: values.StopOffline,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopOffline}}}
	tests := []struct {
		name string
		st   agentturn.Step
		err  error
	}{
		{"stored stopped", stopped, errors.New("base: db locked")},
		{"stale", agentturn.Step{}, agentturn.ErrStaleTurn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns := (&scriptedTurns{}).
				then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
				then(tt.st, tt.err)
			d, _ := newDriver(t, newReads(), turns)
			if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			if rep := turns.report(); len(rep.interrupted) != 0 {
				t.Fatalf("interrupted %+v", rep.interrupted)
			}
		})
	}
}

// Interrupt refusing the outcomes of a failed Submit is retried without them: the request is closed all the same.
func TestCloseDropsRefusedOutcomes(t *testing.T) {
	turns := (&scriptedTurns{refuseOutcomes: true}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
		then(agentturn.Step{}, errors.New("disk full"))
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	rep := turns.report()
	if len(rep.interrupted) != 2 || len(rep.interrupted[0].Outcomes) != 1 || len(rep.interrupted[1].Outcomes) != 0 ||
		len(rep.stopped) != 1 {
		t.Fatalf("report %+v", rep)
	}
	if up := lastUpdate(t, sink); up.Status != thread.StatusStopped || up.Problem != ProblemFailed {
		t.Fatalf("last update = %+v", up)
	}
}

// A running step with no call for the Driver (a broken invariant) is closed, not left running.
func TestRunningStepWithoutCallsIsClosed(t *testing.T) {
	turns := (&scriptedTurns{}).then(step(thread.StatusPlanning, nil), nil)
	d, sink := newDriver(t, newReads(), turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.stopped) != 1 || len(rep.interrupted[0].Outcomes) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if up := lastUpdate(t, sink); up.Status != thread.StatusStopped || up.StopReason != values.StopOffline {
		t.Fatalf("last update = %+v", up)
	}
}

// A tool that panics answers failed and the loop goes on: the request is not left without its loop.
func TestToolPanicAnswersFailed(t *testing.T) {
	r := newReads()
	r.library.panics = true
	turns := (&scriptedTurns{}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
		then(step(thread.StatusDone, nil), nil)
	d, _ := newDriver(t, r, turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	rep := turns.report()
	if len(rep.submitted) != 1 || len(rep.submitted[0].Outcomes) != 1 ||
		rep.submitted[0].Outcomes[0].Result.ResultJSON != `{"error":"failed"}` {
		t.Fatalf("report %+v", rep)
	}
}

// panicTurns panics in Submit, after the loop ran its calls.
type panicTurns struct{ *scriptedTurns }

func (panicTurns) Submit(context.Context, agentturn.SubmitInput) (agentturn.Step, error) {
	panic("submit: broken")
}

// A panic in the loop closes the request through Interrupt and releases the calls of its thread, so «Продолжить» is
// not refused as busy afterwards.
func TestLoopPanicClosesTheRequest(t *testing.T) {
	turns := (&scriptedTurns{}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
		then(step(thread.StatusDone, nil), nil)
	d, sink := newDriver(t, newReads(), panicTurns{turns})
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.stopped) != 1 {
		t.Fatalf("report %+v", rep)
	}
	if up := lastUpdate(t, sink); up.Status != thread.StatusStopped || up.Problem != ProblemFailed {
		t.Fatalf("last update = %+v", up)
	}
	d.mu.Lock()
	inflight, loops := len(d.inflight), len(d.loops)
	d.mu.Unlock()
	if inflight != 0 || loops != 0 {
		t.Fatalf("left behind: inflight %d, loops %d", inflight, loops)
	}
	if err := d.Continue(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if up := lastUpdate(t, sink); up.Problem == ProblemBusy || up.Status != thread.StatusDone {
		t.Fatalf("after Continue = %+v", up)
	}
}
