package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

var ref1 = agentturn.TurnRef{Thread: th1, Turn: 1}

// stopTurns is the step use case around «Стоп»: Stop stores the request stopped by the student, Interrupt keeps the
// stored reason (offline when the request was not stopped), as agentturn does.
type stopTurns struct {
	mu          sync.Mutex
	first       agentturn.Step // the answer of Ask
	resumed     agentturn.Step // the answer of Continue
	askGate     chan struct{}  // when set, Ask waits for it (a Hub step in flight)
	askCtx      error          // ctx.Err() of Ask when it returned
	reason      values.StopReason
	stops       int
	continues   int
	reruns      int
	submitted   []agentturn.SubmitInput
	interrupted []agentturn.SubmitInput
}

func (s *stopTurns) Ask(ctx context.Context, _ agentturn.AskInput) (agentturn.Step, error) {
	if s.askGate != nil {
		<-s.askGate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.askCtx = ctx.Err()
	return s.first, nil
}

func (s *stopTurns) Submit(_ context.Context, in agentturn.SubmitInput) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submitted = append(s.submitted, in)
	return agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusDone}, nil
}

func (s *stopTurns) Stop(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	s.reason = values.StopUser
	return s.stoppedStep(), nil
}

func (s *stopTurns) Interrupt(_ context.Context, in agentturn.SubmitInput) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupted = append(s.interrupted, in)
	if s.reason == "" {
		s.reason = values.StopOffline
	}
	return s.stoppedStep(), nil
}

func (s *stopTurns) Continue(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.continues++
	s.reason = ""
	return s.resumed, nil
}

func (s *stopTurns) stoppedStep() agentturn.Step {
	return agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusStopped, StopReason: s.reason,
		Cards: []values.Card{values.StoppedCard{Reason: s.reason}}}
}

var errNotScripted = errors.New("stop turns: not scripted")

func (s *stopTurns) ApprovePlan(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

func (s *stopTurns) RevisePlan(context.Context, agentturn.TurnRef, string) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

func (s *stopTurns) AnswerQuestions(context.Context, agentturn.TurnRef, []values.Answer) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

func (s *stopTurns) Retry(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

func (s *stopTurns) Restart(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

// Rerun starts the next request, done at once.
func (s *stopTurns) Rerun(_ context.Context, ref agentturn.TurnRef) (agentturn.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reruns++
	s.reason = ""
	return agentturn.Step{Thread: ref.Thread, Turn: ref.Turn + 1, Status: thread.StatusDone}, nil
}

func (s *stopTurns) Cancel(context.Context, agentturn.TurnRef) (agentturn.Step, error) {
	return agentturn.Step{}, errNotScripted
}

func (s *stopTurns) RecoverIdle(context.Context, agentturn.IdleInput, func(shared.ThreadID) bool) (bool, error) {
	return false, nil
}

type stopReport struct {
	stops, continues, reruns int
	submitted, interrupted   []agentturn.SubmitInput
	askCtx                   error
}

func (s *stopTurns) report() stopReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stopReport{stops: s.stops, continues: s.continues, reruns: s.reruns, submitted: slices.Clone(s.submitted),
		interrupted: slices.Clone(s.interrupted), askCtx: s.askCtx}
}

// lastStop is the reason of the last stopped update the panel got.
func lastStop(ups []Update) values.StopReason {
	for i := len(ups) - 1; i >= 0; i-- {
		if ups[i].Status == thread.StatusStopped {
			return ups[i].StopReason
		}
	}
	return ""
}

// «Стоп» while a read runs: the read is cut through its own context and gets no outcome (not done), the next call
// never starts, nothing is submitted, and the request stays stopped by the student.
func TestStopDuringARead(t *testing.T) {
	r := newReads()
	r.library.block, r.library.started = true, make(chan struct{})
	turns := &stopTurns{first: step(thread.StatusPlanning, []agentturn.Call{
		readCall(t, "c1", values.ToolListSubjects, `{}`), readCall(t, "c2", values.ToolJobStatus, `{"ids":["j1"]}`),
	})}
	sink := &chanSink{}
	d, err := New(r.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	<-r.library.started
	if err := d.Stop(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if rep.stops != 1 || len(rep.submitted) != 0 || len(rep.interrupted) != 1 || len(rep.interrupted[0].Outcomes) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if len(r.activity.asked) != 0 {
		t.Fatalf("a call ran after «Стоп»: job_status asked %v", r.activity.asked)
	}
	if got := lastStop(sink.all()); got != values.StopUser {
		t.Fatalf("stop reason = %q, want user", got)
	}
}

// «Стоп» during the first of two actions: the first finishes with its real outcome (journal DONE, kept in Interrupt),
// the second never runs, and the reason stays the student's.
func TestStopBetweenTwoActions(t *testing.T) {
	w := newWorld(t)
	turns := &stopTurns{first: step(thread.StatusExecuting, []agentturn.Call{
		writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`),
		writeCall(t, "c2", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`),
	})}
	sink := &chanSink{}
	d, err := New(w.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	w.lib.onCreate = func() {
		if err := d.Stop(context.Background(), ref1); err != nil {
			t.Error(err)
		}
	}

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "добавь химию и папку"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if rep.stops != 1 || len(rep.submitted) != 0 || len(rep.interrupted) != 1 {
		t.Fatalf("report %+v", rep)
	}
	outs := rep.interrupted[0].Outcomes
	if len(outs) != 1 || outs[0].Result.CallID != "c1" || outs[0].Result.IsError || outs[0].Created.IsZero() {
		t.Fatalf("outcomes kept = %+v", outs)
	}
	if !slices.Equal(w.lib.calls, []string{"CreateSubject"}) {
		t.Fatalf("library calls = %v", w.lib.calls)
	}
	entries := w.store.list(t.Context())
	if len(entries) != 1 || entries[0].CallID() != "c1" || entries[0].Status() != action.StatusDone {
		t.Fatalf("journal = %+v", entries)
	}
	if got := lastStop(sink.all()); got != values.StopUser {
		t.Fatalf("stop reason = %q, want user", got)
	}
}

// «Стоп» during an action, then «Продолжить» before the action returns: the resume is refused as busy (the loop still
// runs), so the mark stays, the read and the action after the cut call never run, nothing is submitted with missing
// outcomes and the request ends stopped through Interrupt; once the loop has closed the step «Продолжить» goes through.
func TestContinueDuringAnActionCutByStop(t *testing.T) {
	w := newWorld(t)
	turns := &stopTurns{
		first: step(thread.StatusExecuting, []agentturn.Call{
			writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`),
			readCall(t, "c2", values.ToolJobStatus, `{"ids":["j1"]}`),
			writeCall(t, "c3", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`),
		}),
		resumed: step(thread.StatusDone, nil),
	}
	sink := &chanSink{}
	d, err := New(w.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	w.lib.onCreate = func() {
		if err := d.Stop(context.Background(), ref1); err != nil {
			t.Error(err)
		}
		d.mu.Lock()
		m := d.stops[ref1]
		d.mu.Unlock()
		<-m.done // the stop is stored and the calls context is cut
		if err := d.Continue(context.Background(), ref1); err != nil {
			t.Error(err)
		}
	}

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "добавь химию и папку"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if rep.stops != 1 || rep.continues != 0 || len(rep.submitted) != 0 || len(rep.interrupted) != 1 {
		t.Fatalf("report %+v", rep)
	}
	if outs := rep.interrupted[0].Outcomes; len(outs) != 1 || outs[0].Result.CallID != "c1" || outs[0].Result.IsError {
		t.Fatalf("outcomes kept = %+v", outs)
	}
	if !slices.Equal(w.lib.calls, []string{"CreateSubject"}) || len(w.activity.asked) != 0 {
		t.Fatalf("a call ran after «Стоп»: library %v, job_status %v", w.lib.calls, w.activity.asked)
	}
	ups := sink.all()
	if !slices.ContainsFunc(ups, func(u Update) bool { return u.Problem == ProblemBusy && u.Thread == th1 && u.Turn == 1 }) {
		t.Fatalf("no busy update: %+v", ups)
	}
	if got := lastStop(ups); got != values.StopUser {
		t.Fatalf("stop reason = %q, want user", got)
	}

	w.lib.onCreate = nil
	if err := d.Continue(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); rep.continues != 1 {
		t.Fatalf("after the loop ended: report %+v", rep)
	}
}

// «Повторить» of a stopped request while its loop still finishes an action: refused as busy (a repeat would start
// while the old step is open); once the loop has closed the step the repeat runs as the next request.
func TestRerunDuringAnActionCutByStop(t *testing.T) {
	w := newWorld(t)
	turns := &stopTurns{first: step(thread.StatusExecuting, []agentturn.Call{
		writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`),
		writeCall(t, "c2", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`),
	})}
	sink := &chanSink{}
	d, err := New(w.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	w.lib.onCreate = func() {
		if err := d.Stop(context.Background(), ref1); err != nil {
			t.Error(err)
		}
		d.mu.Lock()
		m := d.stops[ref1]
		d.mu.Unlock()
		<-m.done
		if err := d.Rerun(context.Background(), ref1); err != nil {
			t.Error(err)
		}
	}

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "добавь химию и папку"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if rep.reruns != 0 || len(rep.submitted) != 0 || len(rep.interrupted) != 1 || !slices.Equal(w.lib.calls, []string{"CreateSubject"}) {
		t.Fatalf("report %+v, library %v", rep, w.lib.calls)
	}
	if !slices.ContainsFunc(sink.all(), func(u Update) bool { return u.Problem == ProblemBusy && u.Thread == th1 && u.Turn == 1 }) {
		t.Fatalf("no busy update: %+v", sink.all())
	}

	w.lib.onCreate = nil
	if err := d.Rerun(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	ups := sink.all()
	if last := ups[len(ups)-1]; turns.report().reruns != 1 || last.Turn != 2 || last.Status != thread.StatusDone || last.Problem != ProblemNone {
		t.Fatalf("after the loop ended: reruns %d, last %+v", turns.report().reruns, last)
	}
}

// «Стоп» while an action starts a paid job: the job keeps running (nothing is cancelled), the action keeps its real
// outcome and the call after it never runs.
func TestStopKeepsAPaidJob(t *testing.T) {
	w := newWorld(t)
	turns := &stopTurns{first: step(thread.StatusExecuting, []agentturn.Call{
		writeCall(t, "c1", values.ToolMakeSummary, `{"recording":"r1"}`),
		writeCall(t, "c2", values.ToolCreateSubject, `{"name":"Химия"}`),
	})}
	d, err := New(w.deps(turns, &chanSink{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	w.jobs.onStart = func() {
		if err := d.Stop(context.Background(), ref1); err != nil {
			t.Error(err)
		}
	}

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "сделай конспект"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if len(rep.interrupted) != 1 || len(rep.interrupted[0].Outcomes) != 1 ||
		!strings.Contains(rep.interrupted[0].Outcomes[0].Result.ResultJSON, `"job":"job1"`) {
		t.Fatalf("report %+v", rep)
	}
	if w.jobs.cancels != 0 || len(w.activity.jobs) != 1 || w.activity.jobs[0].Status != job.StatusQueued {
		t.Fatalf("job after «Стоп»: cancels %d, jobs %+v", w.jobs.cancels, w.activity.jobs)
	}
	if len(w.lib.calls) != 0 {
		t.Fatalf("the call after the paid action ran: %v", w.lib.calls)
	}
}

// «Стоп» while the Hub step of a new request is in flight: the step is not cancelled (its ctx stays alive), calls it
// returns before the stop was stored never run, the reason is the student's; «Продолжить» clears the mark and runs.
func TestStopDuringTheHubStep(t *testing.T) {
	r := newReads()
	call := []agentturn.Call{readCall(t, "c1", values.ToolJobStatus, `{"ids":["j1"]}`)}
	turns := &stopTurns{
		first: step(thread.StatusExecuting, call), resumed: step(thread.StatusExecuting, call), askGate: make(chan struct{}),
	}
	sink := &chanSink{}
	d, err := New(r.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "как дела с работой"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	close(turns.askGate)
	d.Wait()

	rep := turns.report()
	if rep.askCtx != nil || rep.stops != 1 || len(rep.submitted) != 0 || len(rep.interrupted) != 1 ||
		len(rep.interrupted[0].Outcomes) != 0 || len(r.activity.asked) != 0 {
		t.Fatalf("report %+v, asked %v", rep, r.activity.asked)
	}
	if got := lastStop(sink.all()); got != values.StopUser {
		t.Fatalf("stop reason = %q, want user", got)
	}

	if err := d.Continue(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.submitted) != 1 || len(r.activity.asked) != 1 {
		t.Fatalf("after Continue: report %+v, asked %v", rep, r.activity.asked)
	}
}

// A Stop the use case refuses (the request moved on) does not end the calls of the loop.
func TestRefusedStopLetsTheCallsRun(t *testing.T) {
	r := newReads()
	turns := (&scriptedTurns{}).
		then(step(thread.StatusExecuting, []agentturn.Call{readCall(t, "c1", values.ToolJobStatus, `{"ids":["j1"]}`)}), nil).
		then(agentturn.Step{}, thread.ErrInvalidTransition). // the Stop
		then(step(thread.StatusDone, nil), nil)
	d, sink := newDriver(t, r, turns)
	fired := false // beforeAnswer runs under the script's lock
	turns.beforeAnswer = func() {
		if fired {
			return
		}
		fired = true
		// «Стоп» pressed while Ask answers: its mark is set before the calls come back.
		if err := d.Stop(context.Background(), ref1); err != nil {
			t.Error(err)
		}
	}
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "как дела"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if rep := turns.report(); len(rep.submitted) != 1 || len(rep.interrupted) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if !slices.ContainsFunc(sink.all(), func(u Update) bool { return u.Problem == ProblemInvalid }) {
		t.Fatalf("updates %+v", sink.all())
	}
}

// A local failure in the middle of a step comes back with the stored, stopped request: the panel shows that state
// with the problem, not the request as the button saw it.
func TestFailedStepShowsTheStoredRequest(t *testing.T) {
	r := newReads()
	stored := agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusStopped, StopReason: values.StopFailed,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopFailed}}}
	turns := (&scriptedTurns{}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListSubjects, `{}`)}), nil).
		then(stored, errors.New("disk write failed"))
	d, sink := newDriver(t, r, turns)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "мои предметы"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	ups := sink.all()
	last := ups[len(ups)-1]
	if last.Status != thread.StatusStopped || last.StopReason != values.StopFailed || last.Problem != ProblemFailed ||
		len(last.Cards) != 1 || last.Cards[0] != (values.StoppedCard{Reason: values.StopFailed}) {
		t.Fatalf("last update = %+v", last)
	}
}

// Marks of a thread stay bounded: a «Стоп» of a later request drops the marks of the earlier ones.
func TestStopMarksOfAThreadAreBounded(t *testing.T) {
	r := newReads()
	turns := &stopTurns{}
	d, err := New(r.deps(turns, &chanSink{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	for turn := 1; turn <= 3; turn++ {
		if err := d.Stop(t.Context(), agentturn.TurnRef{Thread: th1, Turn: turn}); err != nil {
			t.Fatal(err)
		}
	}
	d.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.stops[agentturn.TurnRef{Thread: th1, Turn: 3}]; len(d.stops) != 1 || !ok {
		t.Fatalf("marks = %v", d.stops)
	}
}

// Progress publishes the request of a step that goes to the Hub, with no cards.
func TestProgressPublishes(t *testing.T) {
	if _, err := NewProgress(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewProgress(nil) = %v", err)
	}
	sink := &chanSink{}
	p, err := NewProgress(sink)
	if err != nil {
		t.Fatal(err)
	}
	scope := subjectScope(t, "s1")
	p.Started(t.Context(), agentturn.Step{Thread: th1, Turn: 2, Scope: scope, Status: thread.StatusPlanning,
		Cards: []values.Card{values.ReplyCard{Text: "не для панели"}}})
	ups := sink.all()
	if len(ups) != 1 || !ups[0].Progress || ups[0].Thread != th1 || ups[0].Turn != 2 || ups[0].Scope != scope ||
		ups[0].Status != thread.StatusPlanning || len(ups[0].Cards) != 0 || ups[0].Problem != ProblemNone {
		t.Fatalf("updates %+v", ups)
	}
}
