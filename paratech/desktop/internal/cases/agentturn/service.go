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
	opIdle      = "agentturn: recover idle request"
	opPanic     = "agentturn: step panicked"
)

// MaxOutputTokens is the output cap of one planner step; the Hub raises it to its floor and clamps it to its cap.
// 4096 leaves room for a plan or a reply with a list in one step (TASK-172 raised the Hub's planner cap to match). The
// Hub's planner alias is still capped at 4096, so a reply of catalog v7 (up to 4000 characters) is not asked more here.
const MaxOutputTokens = 4096

// historyLimit is the most entries read for a step: enough whole requests (a text, then an echo and results per
// step) for the texts of the earlier window plus the current request, which may run a plan of MaxPlanRunSteps steps.
// An earlier request that ran a long plan can still push older texts out of the window; the loss stays bounded.
const historyLimit = thread.MaxHistoryTurns*(2*thread.MaxSteps+1) + 2*thread.MaxPlanRunSteps + 1

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
	progress Progress  // nil: none
	effects  Effects   // nil: no Rerun
	mentions Mentioned // nil: mentions are dropped
	log      *slog.Logger

	mu sync.Mutex
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Model == nil || d.Threads == nil || d.History == nil || d.Subjects == nil || d.Facts == nil || d.Tx == nil ||
		d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%s: %w: every port except Progress, Effects, Mentions and Log is required", opNew, ErrInvalidConfig)
	}
	s := &Service{
		model: d.Model, threads: d.Threads, history: d.History, subjects: d.Subjects, facts: d.Facts, tx: d.Tx,
		clock: d.Clock, ids: d.IDs, progress: d.Progress, effects: d.Effects, mentions: d.Mentions, log: d.Log,
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
	// redo rebuilds the change of an Ask or a card answer on the stored chat when its first save meets a version
	// conflict; nil once stored.
	redo func() (*change, error)
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

// Ask starts a request in the chat of in (the current chat when none is named, a new one when there is none) with the
// scope of the context chip and the mentions that still exist, and runs its steps until the request needs the Driver
// (calls to run) or the student (a plan, questions), or ends. A previous request that waits for the student is
// cancelled; an archived chat comes back from the archive (thread.Ask). A named chat that does not exist wraps
// shared.ErrNotFound.
func (s *Service) Ask(ctx context.Context, in AskInput) (Step, error) {
	text, err := values.NewStudentText(in.Text, in.Uploads)
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
	}
	if in.ThreadID != "" {
		if err := in.ThreadID.Validate(); err != nil {
			return Step{}, fmt.Errorf("%s: %w: %w", opAsk, ErrInvalidInput, err)
		}
	}
	mentions, err := s.existing(ctx, in.Mentions)
	if err != nil {
		return Step{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.asked(ctx, in, text, mentions)
	if err != nil {
		return Step{}, err
	}
	// A chat change (rename, archive, restore) does not take s.mu; one saved before the request is stored would make
	// the first save a version conflict and lose the student's message, so the request is asked again once on the
	// stored chat (see store).
	c.redo = func() (*change, error) { return s.asked(ctx, in, text, mentions) }
	return s.drive(ctx, c, opAsk)
}

// asked loads the chat of in and starts the request in it, not yet stored.
func (s *Service) asked(ctx context.Context, in AskInput, text string, mentions []values.Mention) (*change, error) {
	now := s.clock.Now()
	th, err := s.chat(ctx, in.ThreadID, now)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opAsk, err)
	}

	c := &change{th: th}
	req := thread.Request{Scope: in.Scope, Mentions: mentions, Text: in.Text}
	err = th.Ask(req, now)
	if errors.Is(err, thread.ErrResultsPending) {
		// Calls of the previous request that were never answered (a plan left open, a late Driver) are closed first,
		// so the history keeps every echo followed by its results.
		th.Abandon(values.RefuseNotDone, now)
		c.takeResults(s)
		err = th.Ask(req, now)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opAsk, err)
	}
	if err := th.Observe(in.Uploads...); err != nil {
		return nil, fmt.Errorf("%s: %w", opAsk, err)
	}
	c.add(values.HistoryTurn{StudentText: text})
	return c, nil
}

// chat loads the chat named id, or the current chat for an empty id, created when there is none.
func (s *Service) chat(ctx context.Context, id shared.ThreadID, now time.Time) (*thread.Thread, error) {
	if id != "" {
		return s.threads.Get(ctx, id)
	}
	th, err := s.threads.Current(ctx)
	if errors.Is(err, shared.ErrNotFound) {
		return thread.New(s.ids.NewThreadID(), now)
	}
	return th, err
}

// existing keeps the mentions whose objects still exist; a missing one is dropped without an error (ADR-0028), so the
// planner never gets its ID. Without the Mentions port none is kept.
func (s *Service) existing(ctx context.Context, ms []values.Mention) ([]values.Mention, error) {
	if len(ms) > values.MaxMentions {
		return nil, fmt.Errorf("%s: %w: at most %d mentions", opAsk, ErrInvalidInput, values.MaxMentions)
	}
	var out []values.Mention
	for _, m := range ms {
		if m.IsZero() {
			return nil, fmt.Errorf("%s: %w: an empty mention", opAsk, ErrInvalidInput)
		}
		if s.mentions == nil {
			continue
		}
		ok, err := s.mentions.Exists(ctx, m.Ref())
		if err != nil {
			return nil, fmt.Errorf("%s: mention %s: %w", opAsk, m.Ref(), err)
		}
		if ok {
			out = append(out, m)
		}
	}
	return out, nil
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

// keepOutcomes is applyOutcomes for Interrupt, which must close the request whatever the report holds: an outcome of a
// call that no longer waits for the Driver (a failed Submit that stored it) is skipped, and one the thread refuses
// answers its call outcome_unknown, since the call ran and must never read as not done (hard rule 7).
func (s *Service) keepOutcomes(ctx context.Context, c *change, outcomes []Outcome) {
	now := s.clock.Now()
	for _, o := range outcomes {
		id := o.Result.CallID
		if turn, _ := c.th.Turn(); !driverCall(turn, id) {
			continue
		}
		err := c.th.Answer(o.Result, o.Created, now)
		if err == nil {
			// IDs the planner sees but the check does not know only make later calls refused.
			err = c.th.Observe(o.Seen...)
		} else if aerr := c.th.Answer(values.RefusalResult(id, values.RefuseUnknown), values.Ref{}, now); aerr != nil {
			err = errors.Join(err, aerr)
		}
		if err != nil {
			s.log.WarnContext(ctx, opInterrupt, "thread", c.th.ID().String(), "call", id, "err", err)
		}
	}
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

// answerUI answers the open card call of tool with the result of decide, which also moves the request. A rename saved
// between the load and the first save (allowed while the request waits for the student) would make that save a
// version conflict and drop the click, so the answer is applied again once on the stored chat, as for Ask.
func (s *Service) answerUI(ctx context.Context, ref TurnRef, tool values.Tool, op string,
	decide func(c *change, id string) (values.ToolResult, error)) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.answered(ctx, ref, tool, op, decide)
	if err != nil {
		return Step{}, err
	}
	c.redo = func() (*change, error) { return s.answered(ctx, ref, tool, op, decide) }
	return s.drive(ctx, c, op)
}

// answered loads the request of ref and answers its open card call of tool, not yet stored.
func (s *Service) answered(ctx context.Context, ref TurnRef, tool values.Tool, op string,
	decide func(c *change, id string) (values.ToolResult, error)) (*change, error) {
	c, err := s.load(ctx, ref, op)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("%s: %w: no open %s card", op, thread.ErrInvalidTransition, tool)
	}
	res, err := decide(c, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if err := c.th.Answer(res, values.Ref{}, s.clock.Now()); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return c, nil
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
// outcomes of calls that did run (an action that is journaled DONE or FAILED) are recorded as they are (see
// keepOutcomes: a report that does not fit never refuses the close); the other calls are answered not done and the request stops as «нет связи» (thread.Interrupt), so «Продолжить» goes on as
// after Recover. A request the student already stopped keeps its reason. Only «Отмена» cancels a request.
func (s *Service) Interrupt(ctx context.Context, in SubmitInput) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 1; ; attempt++ {
		c, err := s.load(ctx, in.Turn, opInterrupt)
		if err != nil {
			return Step{}, err
		}
		s.keepOutcomes(ctx, c, in.Outcomes)
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
		if err := s.stopLeft(ctx, th); err != nil {
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

// stopLeft stops a request no loop runs: calls handed to the Driver get outcome_unknown, the request stops as
// «нет связи» with its step as it was.
func (s *Service) stopLeft(ctx context.Context, th *thread.Thread) error {
	c := &change{th: th}
	now := s.clock.Now()
	th.Abandon(values.RefuseUnknown, now)
	c.takeResults(s)
	if err := th.Stop(values.StopOffline, now); err != nil {
		return err
	}
	return s.save(ctx, c)
}

// RecoverIdle is Recover for one thread while the app runs (TASK-171): its request, PLANNING or EXECUTING, is stopped
// as at start unless live reports that a loop of this process still drives the thread. live runs under s.mu, so no
// operation that takes it moves the request meanwhile; Stop does not take it, and a «Стоп» saved in between makes the
// save a version conflict: the request no longer runs, so that is false too. It reports whether a request was
// stopped; a missing thread or a request that does not run is false.
func (s *Service) RecoverIdle(ctx context.Context, in IdleInput, live func(shared.ThreadID) bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var th *thread.Thread
	var err error
	if in.Thread != "" {
		th, err = s.threads.Get(ctx, in.Thread)
	} else {
		th, err = s.threads.Current(ctx)
	}
	if errors.Is(err, shared.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", opIdle, err)
	}
	if turn, ok := th.Turn(); !ok || !running(turn) || (live != nil && live(th.ID())) {
		return false, nil
	}
	err = s.stopLeft(ctx, th)
	if errors.Is(err, shared.ErrVersionConflict) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", opIdle, err)
	}
	s.log.WarnContext(ctx, opIdle, "thread", th.ID().String())
	return true, nil
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
