package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNew     = "agent: new"
	opRequest = "agent: request"
	opAbandon = "agent: close calls"
	opTool    = "agent: tool"
	opTooLong = "agent: request loop over its step limit"
	opBadCard = "agent: card dropped"
)

// maxSubmits bounds the results one loop reports. Every Submit of a request is followed by at most one new step, so
// a request ends within its step budget; the bound only guards against a broken invariant.
const maxSubmits = thread.MaxSteps

// Errors of the Driver.
var (
	// ErrInvalidConfig means a required dependency is missing.
	ErrInvalidConfig = errors.New("agent: invalid configuration")

	// ErrClosed means the Driver was closed; no new request runs.
	ErrClosed = errors.New("agent: the driver is closed")
)

// Turns is the agent step use case (cases/agentturn) as the Driver uses it.
type Turns interface {
	Ask(ctx context.Context, in agentturn.AskInput) (agentturn.Step, error)
	Submit(ctx context.Context, in agentturn.SubmitInput) (agentturn.Step, error)
	ApprovePlan(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	RevisePlan(ctx context.Context, ref agentturn.TurnRef, wish string) (agentturn.Step, error)
	AnswerQuestions(ctx context.Context, ref agentturn.TurnRef, answers []values.Answer) (agentturn.Step, error)
	Stop(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	Continue(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	Retry(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	Restart(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	Cancel(ctx context.Context, ref agentturn.TurnRef) (agentturn.Step, error)
	Interrupt(ctx context.Context, in agentturn.SubmitInput) (agentturn.Step, error)
}

// Problem is why an operation on a request did not go through, as an enum (no error text reaches the panel).
type Problem string

// Problems.
const (
	ProblemNone    Problem = ""
	ProblemBusy    Problem = "busy"             // the thread runs another request
	ProblemStale   Problem = "stale"            // the button belongs to an older request
	ProblemInvalid Problem = "invalid"          // the request text, an answer or the move is not allowed
	ProblemBudget  Problem = "budget"           // the request used its budget
	ProblemRetry   Problem = "retry_required"   // the step failed for good: «Повторить»
	ProblemRestart Problem = "restart_required" // the Hub forgot the request: start it again
	ProblemFailed  Problem = "failed"           // a local failure
)

// Update is what the panel gets after each move of a request: its state and the cards to show, in order. Cards hold
// only IDs, enums and the short checked texts of show_plan, ask_questions, reply and cannot_do (no HTML). An update of
// a failed operation names the request it acted on: Thread and Turn for a button, only Scope for an Ask whose thread
// may not exist yet. A Progress update (a step went to the Hub) has no cards: the panel applies its status only.
type Update struct {
	Thread     shared.ThreadID
	Turn       int
	Scope      values.Scope
	Status     thread.Status
	StopReason values.StopReason
	Cards      []values.Card
	Problem    Problem
	Progress   bool
}

// Sink receives the updates of every request, from the Driver's goroutines; it must not block for long.
type Sink interface {
	Publish(u Update)
}

// Clock gives today and the student's time zone for the dates of the read results.
type Clock interface {
	Now() time.Time
}

// Deps are the dependencies of the Driver; every one except Journal, Actions, Inspector, Answerer, Proposals,
// Deletions, Drafts and Log is required. Without Journal and Actions the actions (class U) answer "unavailable",
// without Inspector inspect_upload does, without Answerer answer_question does, without Journal and Proposals or
// Deletions the proposals (class C) do.
type Deps struct {
	Turns       Turns
	Sink        Sink
	Library     SubjectLibrary
	Inspector   Inspector
	Answerer    Answerer
	Knowledge   Knowledge
	Activity    Activity
	Assignments Assignments
	Deliveries  Deliveries
	Journal     Journal
	Actions     *Actions
	Proposals   Proposals
	Deletions   DeletionPreviews
	Drafts      DraftVersions
	Clock       Clock
	Log         *slog.Logger
}

// Driver runs agent requests: each operation runs in its own goroutine until the request waits for the student or
// ends. The ctx of an operation bounds the whole loop it starts, so it must live as long as the app (TASK-119: not the
// ctx of a Wails call, which ends with the call). When it ends while calls wait to run, the calls are answered as not
// done and the request stops continuable, so it never stays running with calls nobody runs; only Cancel ends it.
type Driver struct {
	turns Turns
	sink  Sink
	tools tools
	log   *slog.Logger

	mu      sync.Mutex
	closed  bool
	next    uint64
	cancels map[uint64]context.CancelFunc
	// stops are the «Стоп» marks of requests; the next operation that resumes the thread clears them.
	stops map[agentturn.TurnRef]*stopping
	// inflight cancels the calls a loop (by operation id) runs for a request, for «Стоп».
	inflight map[agentturn.TurnRef]map[uint64]context.CancelFunc
	wg       sync.WaitGroup
}

// stopping is a «Стоп» of a request: once it is set no call of the request starts. When Turns.Stop has stored the
// request stopped (ok) the calls in flight are cut and the loop closes the step with Interrupt, which keeps the
// student's reason; a refused Stop (ok false) lets the loop go on.
type stopping struct {
	done chan struct{} // closed when Turns.Stop returned
	ok   bool          // written before done is closed
}

// New checks the dependencies.
func New(d Deps) (*Driver, error) {
	if d.Turns == nil || d.Sink == nil || d.Library == nil || d.Knowledge == nil || d.Activity == nil ||
		d.Assignments == nil || d.Deliveries == nil || d.Clock == nil || (d.Journal == nil) != (d.Actions == nil) {
		return nil, fmt.Errorf("%s: %w: every dependency except Log is required (Journal with Actions)", opNew, ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Driver{
		turns: d.Turns, sink: d.Sink, log: log, cancels: map[uint64]context.CancelFunc{},
		stops: map[agentturn.TurnRef]*stopping{}, inflight: map[agentturn.TurnRef]map[uint64]context.CancelFunc{},
		tools: tools{
			library: d.Library, inspector: d.Inspector, answerer: d.Answerer, knowledge: d.Knowledge, activity: d.Activity, assignments: d.Assignments,
			deliveries: d.Deliveries, journal: d.Journal, actions: d.Actions, proposals: d.Proposals,
			deletions: d.Deletions, drafts: d.Drafts, clock: d.Clock, log: log,
		},
	}, nil
}

// opMarks is what an operation does with the «Стоп» marks: an operation that resumes a request clears the marks of
// its thread, Stop replaces them with its own; Ask (its thread is not known yet) and Cancel (the request ends) keep them.
type opMarks struct {
	clear bool
	stop  *stopping
}

// resumes clears the «Стоп» marks of the thread.
var resumes = opMarks{clear: true}

// Ask starts a request typed in the panel.
func (d *Driver) Ask(ctx context.Context, in agentturn.AskInput) error {
	at := agentturn.Step{Scope: in.Scope}
	return d.start(ctx, at, opMarks{}, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Ask(ctx, in) })
}

// ApprovePlan is «Да» on the plan card.
func (d *Driver) ApprovePlan(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.ApprovePlan(ctx, ref)
	})
}

// RevisePlan is «Изменить» on the plan card with the change the student typed.
func (d *Driver) RevisePlan(ctx context.Context, ref agentturn.TurnRef, wish string) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.RevisePlan(ctx, ref, wish)
	})
}

// AnswerQuestions sends the answers of a Questions card.
func (d *Driver) AnswerQuestions(ctx context.Context, ref agentturn.TurnRef, answers []values.Answer) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.AnswerQuestions(ctx, ref, answers)
	})
}

// Stop is «Стоп». A Hub step in flight is not cancelled: its reply is dropped and «Продолжить» replays its key. From
// now on the loop of the request starts no further call; once the stop is stored, a read in flight is cut (not done)
// and an action in flight finishes with its real outcome, then the loop closes the step (ADR-0026). Paid jobs the
// request started keep running.
func (d *Driver) Stop(ctx context.Context, ref agentturn.TurnRef) error {
	m := &stopping{done: make(chan struct{})}
	err := d.start(ctx, stepAt(ref), opMarks{stop: m}, func(ctx context.Context) (agentturn.Step, error) {
		st, err := d.turns.Stop(ctx, ref)
		d.stopped(ref, m, err == nil)
		return st, err
	})
	if err != nil {
		d.stopped(ref, m, false)
	}
	return err
}

// Continue is «Продолжить» after a stop, a lost connection or the quota: the same step key, a free replay.
func (d *Driver) Continue(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.Continue(ctx, ref)
	})
}

// Retry is «Повторить» after a step failed for good.
func (d *Driver) Retry(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Retry(ctx, ref) })
}

// Restart starts an expired request again from its text.
func (d *Driver) Restart(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, stepAt(ref), resumes, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.Restart(ctx, ref)
	})
}

// Cancel is «Отмена»: the request ends.
func (d *Driver) Cancel(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, stepAt(ref), opMarks{}, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.Cancel(ctx, ref)
	})
}

// Wait blocks until every operation started so far has ended. It is for tests: no operation may start while it waits
// (the sync.WaitGroup rule); the app uses Close.
func (d *Driver) Wait() { d.wg.Wait() }

// Close cancels the running operations, waits for them and refuses new ones.
func (d *Driver) Close() {
	d.mu.Lock()
	d.closed = true
	for _, cancel := range d.cancels {
		cancel()
	}
	d.mu.Unlock()
	d.wg.Wait()
}

// start runs op and its loop in a goroutine; at is the request it acts on, for an update when op fails. The marks
// change before op runs, so a call that starts after start returns sees them.
func (d *Driver) start(ctx context.Context, at agentturn.Step, marks opMarks, op func(ctx context.Context) (agentturn.Step, error)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	// A «Стоп» drops the older marks of its thread too: Ask keeps them, so they would pile up otherwise.
	if marks.clear || marks.stop != nil {
		for ref := range d.stops {
			if ref.Thread == at.Thread {
				delete(d.stops, ref)
			}
		}
	}
	if marks.stop != nil {
		d.stops[refOf(at)] = marks.stop
	}
	ctx, cancel := context.WithCancel(ctx)
	id := d.next
	d.next++
	d.cancels[id] = cancel
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer d.forget(id)
		d.run(ctx, id, at, op)
	}()
	return nil
}

// stopped settles a «Стоп» of ref: when it is stored the calls in flight of the request are cut.
func (d *Driver) stopped(ref agentturn.TurnRef, m *stopping, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m.ok = ok
	if ok {
		for _, cancel := range d.inflight[ref] {
			cancel()
		}
	}
	close(m.done)
}

// stopRequested reports whether a stored «Стоп» ends the calls of ref. A mark whose Stop still runs is waited for
// (Turns.Stop does not wait for a running step); false when ctx ends meanwhile, which the loop handles itself.
func (d *Driver) stopRequested(ctx context.Context, ref agentturn.TurnRef) bool {
	d.mu.Lock()
	m := d.stops[ref]
	d.mu.Unlock()
	if m == nil {
		return false
	}
	select {
	case <-m.done:
		return m.ok
	case <-ctx.Done():
		return false
	}
}

// callsContext is the context of the calls loop id runs for ref; «Стоп» cancels it. release must be called after.
func (d *Driver) callsContext(ctx context.Context, id uint64, ref agentturn.TurnRef) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	if d.inflight[ref] == nil {
		d.inflight[ref] = map[uint64]context.CancelFunc{}
	}
	d.inflight[ref][id] = cancel
	d.mu.Unlock()
	return ctx, func() {
		d.mu.Lock()
		delete(d.inflight[ref], id)
		if len(d.inflight[ref]) == 0 {
			delete(d.inflight, ref)
		}
		d.mu.Unlock()
		cancel()
	}
}

func (d *Driver) forget(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel, ok := d.cancels[id]; ok {
		cancel()
		delete(d.cancels, id)
	}
}

// run is the loop of operation id: show the step, run its calls, report their results, until no call is left. at is
// the request the operation acts on; it names the request in an update of a failed operation.
func (d *Driver) run(ctx context.Context, id uint64, at agentturn.Step, op func(ctx context.Context) (agentturn.Step, error)) {
	st, err := op(ctx)
	for submits := 0; ; submits++ {
		if ctx.Err() != nil {
			// A failed Hub call already stored the request stopped; only calls handed to us need closing.
			switch {
			case err == nil && len(st.Calls) > 0:
				d.interrupt(ctx, st, nil)
			case err == nil:
				d.publish(st, ProblemNone) // the step is stored; the panel still shows it
			}
			return
		}
		if err != nil {
			if st.Thread == "" {
				st = at
			}
			d.log.WarnContext(ctx, opRequest, "thread", st.Thread.String(), "err", err)
			d.publish(st, problemOf(err))
			return
		}
		d.publish(st, ProblemNone)
		if len(st.Calls) == 0 {
			return
		}
		if submits == maxSubmits {
			// A broken invariant of the step use case, not a lost context: the request ends for good.
			d.log.ErrorContext(ctx, opTooLong, "thread", st.Thread.String())
			if _, err := d.turns.Cancel(context.WithoutCancel(ctx), refOf(st)); err != nil {
				d.log.WarnContext(ctx, opAbandon, "thread", st.Thread.String(), "err", err)
			}
			return
		}

		outcomes, cards, stopped := d.runCalls(ctx, id, st)
		if len(cards) > 0 {
			d.publish(agentturn.Step{Thread: st.Thread, Turn: st.Turn, Scope: st.Scope, Status: st.Status, Cards: cards}, ProblemNone)
		}
		if ctx.Err() != nil || stopped {
			d.interrupt(ctx, st, outcomes)
			return
		}
		at = agentturn.Step{Thread: st.Thread, Turn: st.Turn, Scope: st.Scope}
		st, err = d.turns.Submit(ctx, agentturn.SubmitInput{Turn: refOf(st), Outcomes: outcomes})
	}
}

// runCalls runs the calls of step st in order and returns their outcomes, the cards they add and whether a stored
// «Стоп» ended them. No call starts after «Стоп»; a read cut by «Стоп» or by ctx may report a false failure, so it
// gets no outcome («not done»); an action runs to its end and keeps its real outcome.
func (d *Driver) runCalls(ctx context.Context, id uint64, st agentturn.Step) ([]agentturn.Outcome, []values.Card, bool) {
	ref := refOf(st)
	callCtx, release := d.callsContext(ctx, id, ref)
	defer release()

	outcomes := make([]agentturn.Outcome, 0, len(st.Calls))
	var cards []values.Card
	for _, c := range st.Calls {
		if ctx.Err() != nil {
			return outcomes, cards, false
		}
		if d.stopRequested(ctx, ref) {
			return outcomes, cards, true
		}
		r := d.tools.run(callCtx, st, c)
		if callCtx.Err() != nil && !r.write {
			continue
		}
		outcomes = append(outcomes, r.out)
		cards = append(cards, r.cards...)
	}
	// A «Стоп» during the last call closes the step too: Submit would start the next Hub step.
	return outcomes, cards, ctx.Err() == nil && d.stopRequested(ctx, ref)
}

// interrupt closes a request whose calls will not run because the ctx ended or «Стоп» was stored: the outcomes of the
// calls that ran (the actions always, journaled DONE or FAILED) are kept, the others are answered as not done, and the
// request stops continuable («Продолжить»), as when the ctx ends during a Hub call; a «Стоп» keeps its reason.
func (d *Driver) interrupt(ctx context.Context, st agentturn.Step, outcomes []agentturn.Outcome) {
	stopped, err := d.turns.Interrupt(context.WithoutCancel(ctx), agentturn.SubmitInput{Turn: refOf(st), Outcomes: outcomes})
	if err != nil {
		d.log.WarnContext(ctx, opAbandon, "thread", st.Thread.String(), "err", err)
		return
	}
	d.publish(stopped, ProblemNone)
}

// publish hands the step to the panel; a card that breaks its rules is dropped, never shown.
func (d *Driver) publish(st agentturn.Step, p Problem) {
	cards := make([]values.Card, 0, len(st.Cards))
	for _, c := range st.Cards {
		if c == nil || c.Validate() != nil {
			d.log.Warn(opBadCard, "thread", st.Thread.String())
			continue
		}
		cards = append(cards, c)
	}
	d.sink.Publish(Update{
		Thread: st.Thread, Turn: st.Turn, Scope: st.Scope, Status: st.Status, StopReason: st.StopReason, Cards: cards, Problem: p,
	})
}

func refOf(st agentturn.Step) agentturn.TurnRef {
	return agentturn.TurnRef{Thread: st.Thread, Turn: st.Turn}
}

func stepAt(ref agentturn.TurnRef) agentturn.Step {
	return agentturn.Step{Thread: ref.Thread, Turn: ref.Turn}
}

// problemOf sorts an error of the step use case into the enum the panel shows.
func problemOf(err error) Problem {
	switch {
	case errors.Is(err, thread.ErrTurnInProgress):
		return ProblemBusy
	case errors.Is(err, agentturn.ErrStaleTurn):
		return ProblemStale
	case errors.Is(err, thread.ErrBudgetExceeded):
		return ProblemBudget
	case errors.Is(err, thread.ErrRetryRequired):
		return ProblemRetry
	case errors.Is(err, thread.ErrRestartRequired):
		return ProblemRestart
	case errors.Is(err, agentturn.ErrInvalidInput), errors.Is(err, thread.ErrInvalidTransition):
		return ProblemInvalid
	}
	return ProblemFailed
}
