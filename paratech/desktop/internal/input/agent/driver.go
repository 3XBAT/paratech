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
	opAbandon = "agent: abandon calls"
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
// only IDs, enums and the short checked texts of show_plan, ask_questions, reply and cannot_do (no HTML).
type Update struct {
	Thread     shared.ThreadID
	Turn       int
	Status     thread.Status
	StopReason values.StopReason
	Cards      []values.Card
	Problem    Problem
}

// Sink receives the updates of every request, from the Driver's goroutines; it must not block for long.
type Sink interface {
	Publish(u Update)
}

// Clock gives today and the student's time zone for the dates of the read results.
type Clock interface {
	Now() time.Time
}

// Deps are the dependencies of the Driver; every one except Log is required.
type Deps struct {
	Turns       Turns
	Sink        Sink
	Library     SubjectLibrary
	Knowledge   Knowledge
	Activity    Activity
	Assignments Assignments
	Deliveries  Deliveries
	Clock       Clock
	Log         *slog.Logger
}

// Driver runs agent requests: each operation runs in its own goroutine until the request waits for the student or
// ends. The ctx of an operation bounds the whole loop it starts; when it is cancelled while calls wait to run, the
// request is cancelled (its calls answered as not done), so it never stays running with calls nobody runs.
type Driver struct {
	turns Turns
	sink  Sink
	tools tools
	log   *slog.Logger

	mu      sync.Mutex
	closed  bool
	next    uint64
	cancels map[uint64]context.CancelFunc
	wg      sync.WaitGroup
}

// New checks the dependencies.
func New(d Deps) (*Driver, error) {
	if d.Turns == nil || d.Sink == nil || d.Library == nil || d.Knowledge == nil || d.Activity == nil ||
		d.Assignments == nil || d.Deliveries == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every dependency except Log is required", opNew, ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Driver{
		turns: d.Turns, sink: d.Sink, log: log, cancels: map[uint64]context.CancelFunc{},
		tools: tools{
			library: d.Library, knowledge: d.Knowledge, activity: d.Activity, assignments: d.Assignments,
			deliveries: d.Deliveries, clock: d.Clock, log: log,
		},
	}, nil
}

// Ask starts a request typed in the panel.
func (d *Driver) Ask(ctx context.Context, in agentturn.AskInput) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Ask(ctx, in) })
}

// ApprovePlan is «Да» on the plan card.
func (d *Driver) ApprovePlan(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.ApprovePlan(ctx, ref) })
}

// RevisePlan is «Изменить» on the plan card with the change the student typed.
func (d *Driver) RevisePlan(ctx context.Context, ref agentturn.TurnRef, wish string) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.RevisePlan(ctx, ref, wish) })
}

// AnswerQuestions sends the answers of a Questions card.
func (d *Driver) AnswerQuestions(ctx context.Context, ref agentturn.TurnRef, answers []values.Answer) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) {
		return d.turns.AnswerQuestions(ctx, ref, answers)
	})
}

// Stop is «Стоп»; a running step's reply is dropped and «Продолжить» replays it.
func (d *Driver) Stop(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Stop(ctx, ref) })
}

// Continue is «Продолжить» after a stop, a lost connection or the quota: the same step key, a free replay.
func (d *Driver) Continue(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Continue(ctx, ref) })
}

// Retry is «Повторить» after a step failed for good.
func (d *Driver) Retry(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Retry(ctx, ref) })
}

// Restart starts an expired request again from its text.
func (d *Driver) Restart(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Restart(ctx, ref) })
}

// Cancel is «Отмена»: the request ends.
func (d *Driver) Cancel(ctx context.Context, ref agentturn.TurnRef) error {
	return d.start(ctx, func(ctx context.Context) (agentturn.Step, error) { return d.turns.Cancel(ctx, ref) })
}

// Wait blocks until every running operation has ended.
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

func (d *Driver) start(ctx context.Context, op func(ctx context.Context) (agentturn.Step, error)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	ctx, cancel := context.WithCancel(ctx)
	id := d.next
	d.next++
	d.cancels[id] = cancel
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer d.forget(id)
		d.run(ctx, op)
	}()
	return nil
}

func (d *Driver) forget(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel, ok := d.cancels[id]; ok {
		cancel()
		delete(d.cancels, id)
	}
}

// run is the loop of one operation: show the step, run its calls, report their results, until no call is left.
func (d *Driver) run(ctx context.Context, op func(ctx context.Context) (agentturn.Step, error)) {
	st, err := op(ctx)
	for submits := 0; ; submits++ {
		if ctx.Err() != nil {
			// A failed Hub call already stored the request stopped; only calls handed to us need closing.
			if err == nil {
				d.abandon(ctx, st)
			}
			return
		}
		if err != nil {
			d.log.WarnContext(ctx, opRequest, "thread", st.Thread.String(), "err", err)
			d.publish(st, problemOf(err))
			return
		}
		d.publish(st, ProblemNone)
		if len(st.Calls) == 0 {
			return
		}
		if submits == maxSubmits {
			d.log.ErrorContext(ctx, opTooLong, "thread", st.Thread.String())
			d.abandon(ctx, st)
			return
		}

		outcomes := make([]agentturn.Outcome, 0, len(st.Calls))
		for _, c := range st.Calls {
			outcomes = append(outcomes, d.tools.run(ctx, st.Scope, c))
		}
		if ctx.Err() != nil {
			d.abandon(ctx, st)
			return
		}
		st, err = d.turns.Submit(ctx, agentturn.SubmitInput{Turn: refOf(st), Outcomes: outcomes})
	}
}

// abandon cancels a request whose calls will not run, so they are answered as not done and the thread is free.
func (d *Driver) abandon(ctx context.Context, st agentturn.Step) {
	if len(st.Calls) == 0 {
		return
	}
	if _, err := d.turns.Cancel(context.WithoutCancel(ctx), refOf(st)); err != nil {
		d.log.WarnContext(ctx, opAbandon, "thread", st.Thread.String(), "err", err)
	}
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
		Thread: st.Thread, Turn: st.Turn, Status: st.Status, StopReason: st.StopReason, Cards: cards, Problem: p,
	})
}

func refOf(st agentturn.Step) agentturn.TurnRef {
	return agentturn.TurnRef{Thread: st.Thread, Turn: st.Turn}
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
