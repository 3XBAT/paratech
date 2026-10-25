package agentturn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts.
const (
	opNew       = "agentturn: new"
	opAsk       = "agentturn: ask"
	opSubmit    = "agentturn: submit"
	opApprove   = "agentturn: approve plan"
	opRevise    = "agentturn: revise plan"
	opAnswer    = "agentturn: answer questions"
	opStop      = "agentturn: stop"
	opContinue  = "agentturn: continue"
	opRetry     = "agentturn: retry"
	opRestart   = "agentturn: restart"
	opRerun     = "agentturn: rerun"
	opCancel    = "agentturn: cancel"
	opInterrupt = "agentturn: interrupt"
	opRecover   = "agentturn: recover"
)

// MaxOutputTokens is the output cap of one planner step; the Hub raises it to its floor and clamps it to its cap.
const MaxOutputTokens = 1024

// historyLimit is the most entries read for a step: enough whole requests (a text, then an echo and results per
// step) for the texts of the earlier window plus the current request.
const historyLimit = (thread.MaxHistoryTurns + 1) * (2*thread.MaxSteps + 1)

// Service runs the steps of agent requests. Operations on threads run one at a time; Stop does not wait for a running
// step (see Stop).
type Service struct {
	model    AgentModel
	threads  ThreadRepository
	history  History
	subjects Subjects
	facts    CallFacts
	tx       Tx
	clock    Clock
	ids      IDGenerator
	progress Progress // nil: none
	effects  Effects  // nil: no Rerun
	log      *slog.Logger

	mu sync.Mutex
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Model == nil || d.Threads == nil || d.History == nil || d.Subjects == nil || d.Facts == nil || d.Tx == nil ||
		d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%s: %w: every port except Progress, Effects and Log is required", opNew, ErrInvalidConfig)
	}
	s := &Service{
		model: d.Model, threads: d.Threads, history: d.History, subjects: d.Subjects, facts: d.Facts, tx: d.Tx,
		clock: d.Clock, ids: d.IDs, progress: d.Progress, effects: d.Effects, log: d.Log,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// change is the work of one operation: the thread, the history entries to append with it, and what the operation
// produced for the Driver and the panel.
type change struct {
	th      *thread.Thread
	entries []values.HistoryEntry
	context values.RefSet // IDs of the base and the scope of the current step
	calls   []Call
	cards   []values.Card
}

func (c *change) add(entry values.HistoryTurn) {
	turn, _ := c.th.Turn()
	c.entries = append(c.entries, values.HistoryEntry{Turn: turn.Number(), Entry: entry})
}

// takeResults moves a complete result set of the last calls into the history.
func (c *change) takeResults(s *Service) {
	if res, ok := c.th.TakeResults(s.clock.Now()); ok {
		c.add(values.HistoryTurn{Results: res})
	}
}

// Ask starts a request in the thread of the scope and runs its steps until the request needs the Driver (calls to
// run) or the student (a plan, questions), or ends. A previous request that waits for the student is cancelled.
func (s *Service) Ask(ctx context.Context, in AskInput) (Step, error) {
	key, err := thread.KeyOf(in.Scope)
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
	}
	text, err := values.NewStudentText(in.Text, in.Uploads)
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	th, err := s.threads.GetByKey(ctx, key)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		if th, err = thread.New(s.ids.NewThreadID(), key, now); err != nil {
			return Step{}, fmt.Errorf("%s: %w", opAsk, err)
		}
	case err != nil:
		return Step{}, fmt.Errorf("%s: %w", opAsk, err)
	}

	c := &change{th: th}
	err = th.StartRequest(in.Scope, now)
	if errors.Is(err, thread.ErrResultsPending) {
		// Calls of the previous request that were never answered (a plan left open, a late Driver) are closed first,
		// so the history keeps every echo followed by its results.
		th.Abandon(values.RefuseNotDone, now)
		c.takeResults(s)
		err = th.StartRequest(in.Scope, now)
	}
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w", opAsk, err)
	}
	if err := th.Observe(in.Uploads...); err != nil {
		return Step{}, fmt.Errorf("%s: %w", opAsk, err)
	}
	c.add(values.HistoryTurn{StudentText: text})
	return s.drive(ctx, c, opAsk)
}

// maxSubmitSaves bounds the saves of the Driver's results when «Стоп» keeps saving in between.
const maxSubmitSaves = 3

// Submit records the results of the calls the Driver ran and goes on when every call of the step is answered. The
// results are stored before the next step: they describe work already done, so a «Стоп» saved in between (Stop does
// not wait for s.mu) must not drop them; on a version conflict they are applied again to the stored, stopped request.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 1; ; attempt++ {
		c, err := s.load(ctx, in.Turn, opSubmit)
		if err != nil {
			return Step{}, err
		}
		if err := s.applyOutcomes(c, in.Outcomes); err != nil {
			return Step{}, fmt.Errorf("%s: %w", opSubmit, err)
		}
		c.takeResults(s)
		err = s.save(ctx, c)
		if err == nil {
			return s.drive(ctx, c, opSubmit)
		}
		if !errors.Is(err, shared.ErrVersionConflict) || attempt == maxSubmitSaves {
			return Step{}, fmt.Errorf("%s: %w", opSubmit, err)
		}
	}
}

// applyOutcomes answers the Driver's calls with their results; Answer works in a stopped request too.
func (s *Service) applyOutcomes(c *change, outcomes []Outcome) error {
	now := s.clock.Now()
	turn, _ := c.th.Turn()
	for _, o := range outcomes {
		if !driverCall(turn, o.Result.CallID) {
			return fmt.Errorf("%w: %q is not a call the Driver runs", ErrInvalidInput, o.Result.CallID)
		}
		if err := c.th.Observe(o.Seen...); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
		if err := c.th.Answer(o.Result, o.Created, now); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	return nil
}

// driverCall reports whether id is an admitted call of the step that waits for its result.
func driverCall(turn thread.Turn, id string) bool {
	for _, oc := range turn.OpenCalls() {
		if oc.ID == id {
			return oc.Admitted && !oc.Answered
		}
	}
	return false
}

// ApprovePlan is «Да» on the plan card: the planner learns it and goes on; later writes must match the plan.
func (s *Service) ApprovePlan(ctx context.Context, ref TurnRef) (Step, error) {
	return s.answerUI(ctx, ref, values.ToolShowPlan, opApprove, func(c *change, id string) (values.ToolResult, error) {
		return values.PlanResult(id, true, ""), c.th.ApprovePlan(s.clock.Now())
	})
}

// RevisePlan is «Изменить»: the change the student typed goes to the planner, which proposes again.
func (s *Service) RevisePlan(ctx context.Context, ref TurnRef, wish string) (Step, error) {
	var label values.Label
	if wish != "" {
		l, err := values.ParseLabel(wish)
		if err != nil {
			return Step{}, fmt.Errorf("%s: %w: %w", opRevise, ErrInvalidInput, err)
		}
		label = l
	}
	return s.answerUI(ctx, ref, values.ToolShowPlan, opRevise, func(c *change, id string) (values.ToolResult, error) {
		return values.PlanResult(id, false, label), c.th.RevisePlan(s.clock.Now())
	})
}

// AnswerQuestions sends the student's answers (option indexes or their own text) and goes on.
func (s *Service) AnswerQuestions(ctx context.Context, ref TurnRef, answers []values.Answer) (Step, error) {
	return s.answerUI(ctx, ref, values.ToolAskQuestions, opAnswer, func(c *change, id string) (values.ToolResult, error) {
		res, err := values.AnswersResult(id, answers)
		if err != nil {
			return values.ToolResult{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
		return res, c.th.AnswerQuestions(s.clock.Now())
	})
}

// answerUI answers the open card call of tool with the result of decide, which also moves the request.
func (s *Service) answerUI(ctx context.Context, ref TurnRef, tool values.Tool, op string,
	decide func(c *change, id string) (values.ToolResult, error)) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, ref, op)
	if err != nil {
		return Step{}, err
	}
	turn, _ := c.th.Turn()
	id := ""
	for _, oc := range turn.OpenCalls() {
		if oc.Tool == tool && !oc.Answered {
			id = oc.ID
			break
		}
	}
	if id == "" {
		return Step{}, fmt.Errorf("%s: %w: no open %s card", op, thread.ErrInvalidTransition, tool)
	}
	res, err := decide(c, id)
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := c.th.Answer(res, values.Ref{}, s.clock.Now()); err != nil {
		return Step{}, fmt.Errorf("%s: %w", op, err)
	}
	return s.drive(ctx, c, op)
}

// maxStopSaves bounds the saves of «Стоп» when steps keep saving in between.
const maxStopSaves = 3

// Stop is «Стоп». It does not wait for a running step: the step's save then finds the stopped request and drops the
// reply, and «Продолжить» repeats the step under the same key (a free replay on the Hub).
func (s *Service) Stop(ctx context.Context, ref TurnRef) (Step, error) {
	for attempt := 1; ; attempt++ {
		c, err := s.load(ctx, ref, opStop)
		if err != nil {
			return Step{}, err
		}
		if err := c.th.Stop(values.StopUser, s.clock.Now()); err != nil {
			return Step{}, fmt.Errorf("%s: %w", opStop, err)
		}
		err = s.save(ctx, c)
		if errors.Is(err, shared.ErrVersionConflict) && attempt < maxStopSaves {
			continue // a step saved in between; stop the newer state
		}
		if err != nil {
			return Step{}, fmt.Errorf("%s: %w", opStop, err)
		}
		return stepOf(c), nil
	}
}

// Continue is «Продолжить» after a stop by the student, a lost connection or the quota: an open step is sent again
// under the SAME key, so a reply the Hub already produced is replayed for free. A step that failed for good needs
// Retry, an expired one Restart.
func (s *Service) Continue(ctx context.Context, ref TurnRef) (Step, error) {
	return s.resume(ctx, ref, opContinue, (*thread.Thread).Continue)
}

// Retry is «Повторить» after a step failed for good (the Hub refused it or the provider failed without retry): the
// Hub keeps that failure under its key for a day, so the step is sent under a NEW deterministic key
// ag:<thread>:<turn>:<step>:r<N>. After thread.MaxRetries the request stops (budget).
func (s *Service) Retry(ctx context.Context, ref TurnRef) (Step, error) {
	return s.resume(ctx, ref, opRetry, (*thread.Thread).Retry)
}

func (s *Service) resume(ctx context.Context, ref TurnRef, op string, move func(*thread.Thread, time.Time) error) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, ref, op)
	if err != nil {
		return Step{}, err
	}
	if err := move(c.th, s.clock.Now()); err != nil {
		if errors.Is(err, thread.ErrBudgetExceeded) {
			if serr := s.save(ctx, c); serr != nil {
				return Step{}, fmt.Errorf("%s: %w", op, serr)
			}
			return stepOf(c), fmt.Errorf("%s: %w", op, err)
		}
		return Step{}, fmt.Errorf("%s: %w", op, err)
	}
	return s.drive(ctx, c, op)
}

// Restart starts an expired request again from the student's text: the Hub no longer keeps the model turn of its
// last echo, so the new request sends only its own history.
func (s *Service) Restart(ctx context.Context, ref TurnRef) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, ref, opRestart)
	if err != nil {
		return Step{}, err
	}
	if err := s.restart(ctx, c); err != nil {
		return Step{}, fmt.Errorf("%s: %w", opRestart, err)
	}
	return s.drive(ctx, c, opRestart)
}

func (s *Service) restart(ctx context.Context, c *change) error {
	turn, _ := c.th.Turn()
	text, err := s.studentText(ctx, c, turn.Number())
	if err != nil {
		return err
	}
	if err := c.th.Restart(s.clock.Now()); err != nil {
		return err
	}
	c.add(values.HistoryTurn{StudentText: text})
	return nil
}

// Rerun is «Повторить» of the last finished or stopped request (ADR-0026): unless the request left effects
// (Effects.Live), a new request starts in its scope with its stored text and runs under new step keys
// ag:<thread>:<turn+1>:<step>, paid again. A button of the old request after that is ErrStaleTurn.
func (s *Service) Rerun(ctx context.Context, ref TurnRef) (Step, error) {
	if s.effects == nil {
		return Step{}, fmt.Errorf("%s: %w: no Effects port", opRerun, ErrInvalidConfig)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, ref, opRerun)
	if err != nil {
		return Step{}, err
	}
	live, err := s.effects.Live(ctx, ref.Thread, ref.Turn)
	if err != nil {
		return Step{}, fmt.Errorf("%s: effects: %w", opRerun, err)
	}
	text, err := s.studentText(ctx, c, ref.Turn)
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w", opRerun, err)
	}
	results, err := c.th.Rerun(live, s.clock.Now())
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w", opRerun, err)
	}
	if len(results) > 0 {
		// The old request's echo is followed by its results, under the old turn.
		c.entries = append(c.entries, values.HistoryEntry{Turn: ref.Turn, Entry: values.HistoryTurn{Results: results}})
	}
	c.add(values.HistoryTurn{StudentText: text})
	return s.drive(ctx, c, opRerun)
}

// studentText finds the text of request turn, in the entries of this change or in the stored history.
func (s *Service) studentText(ctx context.Context, c *change, turn int) (string, error) {
	for _, e := range c.entries {
		if e.Turn == turn && e.Entry.StudentText != "" {
			return e.Entry.StudentText, nil
		}
	}
	entries, err := s.history.Since(ctx, c.th.ID(), turn, historyLimit)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Turn == turn && e.Entry.StudentText != "" {
			return e.Entry.StudentText, nil
		}
	}
	return "", fmt.Errorf("%w: the text of request %d is not stored", shared.ErrNotFound, turn)
}

// Cancel is «Отмена»: the request ends; calls without results are reported to the planner as not done.
func (s *Service) Cancel(ctx context.Context, ref TurnRef) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, ref, opCancel)
	if err != nil {
		return Step{}, err
	}
	if err := c.th.Cancel(s.clock.Now()); err != nil {
		return Step{}, fmt.Errorf("%s: %w", opCancel, err)
	}
	return s.drive(ctx, c, opCancel)
}

// Interrupt closes a request whose calls the Driver will not run because its context ended (the app closes). The
// outcomes of calls that did run (an action that is journaled DONE or FAILED) are recorded as they are; the other
// calls are answered not done and the request stops as «нет связи» (thread.Interrupt), so «Продолжить» goes on as
// after Recover. A request the student already stopped keeps its reason. Only «Отмена» cancels a request.
func (s *Service) Interrupt(ctx context.Context, in SubmitInput) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 1; ; attempt++ {
		c, err := s.load(ctx, in.Turn, opInterrupt)
		if err != nil {
			return Step{}, err
		}
		if err := s.applyOutcomes(c, in.Outcomes); err != nil {
			return Step{}, fmt.Errorf("%s: %w", opInterrupt, err)
		}
		if err := c.th.Interrupt(s.clock.Now()); err != nil {
			return Step{}, fmt.Errorf("%s: %w", opInterrupt, err)
		}
		c.takeResults(s)
		err = s.save(ctx, c)
		if errors.Is(err, shared.ErrVersionConflict) && attempt < maxStopSaves {
			continue // «Стоп» saved in between (it does not wait for s.mu)
		}
		if err != nil {
			return Step{}, fmt.Errorf("%s: %w", opInterrupt, err)
		}
		return stepOf(c), nil
	}
}

// Recover runs at start, before the panel works: a request left running by a crash stops as «нет связи» with its
// step open (so «Продолжить» replays the same key), and calls that were handed to the Driver get the result
// outcome_unknown (the journal shows them UNKNOWN). It returns the number of requests stopped.
func (s *Service) Recover(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	running, err := s.threads.ListRunning(ctx)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", opRecover, err)
	}
	n := 0
	var errs []error
	for _, th := range running {
		c := &change{th: th}
		now := s.clock.Now()
		th.Abandon(values.RefuseUnknown, now)
		c.takeResults(s)
		if err := th.Stop(values.StopOffline, now); err != nil {
			errs = append(errs, fmt.Errorf("thread %s: %w", th.ID(), err))
			continue
		}
		if err := s.save(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("thread %s: %w", th.ID(), err))
			continue
		}
		n++
	}
	if len(errs) > 0 {
		return n, fmt.Errorf("%s: %w", opRecover, errors.Join(errs...))
	}
	return n, nil
}

// load reads the thread of ref and checks that ref is its current request.
func (s *Service) load(ctx context.Context, ref TurnRef, op string) (*change, error) {
	th, err := s.threads.Get(ctx, ref.Thread)
	if errors.Is(err, shared.ErrNotFound) {
		return nil, fmt.Errorf("%s: %w: %w", op, ErrStaleTurn, err)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if turn, ok := th.Turn(); !ok || turn.Number() != ref.Turn {
		return nil, fmt.Errorf("%s: %w", op, ErrStaleTurn)
	}
	return &change{th: th}, nil
}

// save writes the thread and its new history entries in one transaction.
func (s *Service) save(ctx context.Context, c *change) error {
	// The thread first: a new thread must exist before its history refers to it.
	before := c.th.Version()
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.threads.Save(ctx, c.th); err != nil {
			return err
		}
		for _, e := range c.entries {
			if err := s.history.Append(ctx, c.th.ID(), e.Turn, e.Entry); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Save set the new version before the transaction rolled back (a later Append failed): keep the stored one.
		c.th.Persisted(before)
		return err
	}
	c.entries = nil
	return nil
}

// stepOf is what the panel and the Driver get: the request state, the calls to run and the cards to show.
func stepOf(c *change) Step {
	turn, _ := c.th.Turn()
	st := Step{
		Thread: c.th.ID(), Turn: turn.Number(), Scope: turn.Scope(), Status: turn.Status(), StopReason: turn.StopReason(),
		Calls: c.calls, Cards: c.cards,
	}
	if turn.Status() == thread.StatusStopped {
		st.Cards = append(st.Cards, values.StoppedCard{Reason: turn.StopReason()})
	}
	return st
}
