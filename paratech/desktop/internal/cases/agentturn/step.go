package agentturn

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// errPanic wraps a panic of a step, which drive turns into a stop.
var errPanic = errors.New("agentturn: step panicked")

func running(turn thread.Turn) bool {
	return turn.Status() == thread.StatusPlanning || turn.Status() == thread.StatusExecuting
}

// drive runs planner steps while the request can go on by itself, then saves the change. It stops when calls wait
// for the Driver, the request waits for the student (plan, questions), has ended or has stopped. A step whose calls
// were all answered by the case (refusals, a refused third question round) is followed by the next step at once.
// Every failure, a panic included, returns after the stored request was stopped (abort): a request must never stay
// PLANNING or EXECUTING without a Driver loop, or every next request of the thread is busy (TASK-171).
func (s *Service) drive(ctx context.Context, c *change, op string) (st Step, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.ErrorContext(ctx, opPanic, "thread", c.th.ID().String(), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			st, err = s.abort(ctx, c, op, values.StopOffline, fmt.Errorf("%w: %v", errPanic, r))
		}
	}()

	silent := true // an expired first step restarts once without asking the student
	retry := 0     // automatic retries of the open step so far (backoff.Interactive)
	for {
		c.takeResults(s)
		now := s.clock.Now()
		turn, _ := c.th.Turn()
		if !running(turn) || (!turn.StepOpen() && len(turn.OpenCalls()) > 0) {
			return s.finish(ctx, c, op)
		}
		if !turn.StepOpen() {
			if _, err := c.th.BeginStep(now); err != nil {
				if errors.Is(err, thread.ErrBudgetExceeded) {
					return s.finish(ctx, c, op)
				}
				return s.abort(ctx, c, op, values.StopOffline, err)
			}
		}

		key, err := c.th.StepKey()
		if err != nil {
			return s.abort(ctx, c, op, values.StopOffline, err)
		}
		base, err := s.base(ctx)
		if err != nil {
			return s.abort(ctx, c, op, values.StopOffline, fmt.Errorf("base: %w", err))
		}
		// The open step is stored before the call: after a crash or a lost answer «Повторить» (or «Продолжить» after
		// «Стоп») can send it again.
		if again, err := s.store(ctx, c); again {
			continue
		} else if err != nil {
			return s.conflict(ctx, c, op, err)
		}
		turn, _ = c.th.Turn()
		entries, err := s.history.Since(ctx, c.th.ID(), c.th.HistoryFrom(), historyLimit)
		if err != nil {
			return s.abort(ctx, c, op, values.StopOffline, err)
		}
		mentions, err := s.present(ctx, thread.StepMentions(entries, turn.Number(), c.th.Summary(), values.MentionRefs(turn.Mentions())))
		if err != nil {
			return s.abort(ctx, c, op, values.StopOffline, err)
		}
		// What the step sends is what it accepts: the base, the scope and the mentions of the step. (Thread.known also
		// keeps the current request's mentions whose object was deleted mid-request; no longer sent, harmless.)
		c.context = values.NewRefSet(base.Refs()...)
		c.context.Add(turn.Scope().Refs()...)
		c.context.Add(values.StepMentionRefs(mentions)...)
		req := values.StepRequest{
			Key: key, Thread: c.th.ID(), Base: base, Scope: turn.Scope(), Mentions: mentions,
			History: thread.Window(entries, turn.Number(), c.th.Summary()), MaxOutputTokens: MaxOutputTokens,
		}

		if s.progress != nil && retry > 0 {
			s.progress.Retrying(ctx, progressOf(c.th), retry)
		} else if s.progress != nil {
			s.progress.Started(ctx, progressOf(c.th))
		}
		reply, cut, err := s.callModel(ctx, c, req)
		if err != nil {
			if wait, ok := s.retryable(ctx, err, cut, retry+1); ok {
				retry++
				st, done, werr := s.pause(ctx, c, op, err, retry, wait)
				if done {
					return st, werr
				}
				if ctx.Err() == nil {
					continue // the same step under the same key
				}
			}
			reason := reasonOf(ctx, err)
			again, ferr := s.fail(ctx, c, err, reason, silent)
			if ferr != nil {
				return s.abort(ctx, c, op, reason, ferr)
			}
			if again {
				silent, retry = false, 0
				continue
			}
			return s.finish(ctx, c, op)
		}
		retry = 0
		if err := s.handle(ctx, c, reply); err != nil {
			return s.abort(ctx, c, op, values.StopFailed, err)
		}
	}
}

// retryable tells whether a failed Hub call of the open step is retried by itself (ADR-0026 addendum 2026-11-08) and
// after which wait: a failure that may go away (transport, timeout, unavailable, the Hub's rate limit, a retryable
// provider failure or a cut stream), while retries of the step are left. Never the quota, a permanent failure, an
// unusable or expired answer, a Hub that is not set up, a call cut by «Стоп» or a caller gone; never without Sleeper.
func (s *Service) retryable(ctx context.Context, err error, cut bool, attempt int) (time.Duration, bool) {
	if s.sleeper == nil || cut || ctx.Err() != nil || stopReasonOf(err) == values.StopQuota ||
		errors.Is(err, values.ErrTurnGone) || errors.Is(err, values.ErrInvalidResult) ||
		errors.Is(err, shared.ErrServiceNotConfigured) || backoff.Classify(err) != backoff.ClassTransient {
		return 0, false
	}
	return backoff.Interactive(attempt)
}

// pause waits before automatic retry attempt of the open step of c, WITHOUT s.mu: Ask, Stop and other chats go on
// meanwhile, and a «Стоп» of the request (Stop wakes the wait) ends it at once. The panel learns the attempt first.
// done is true when the request no longer waits for this retry: a stop or another change stored meanwhile wins and its
// stored state comes back (see conflict). A done ctx returns with done false; the caller stops the request as for
// the failure.
func (s *Service) pause(ctx context.Context, c *change, op string, cause error, attempt int, wait time.Duration) (Step, bool, error) {
	s.log.WarnContext(ctx, "agent step retried", "thread", c.th.ID().String(), "attempt", attempt, "wait", wait, "err", cause)
	wctx, w := s.waiting(ctx, c.th.ID())
	defer s.waited(c.th.ID(), w)
	if s.progress != nil {
		s.progress.Retrying(ctx, progressOf(c.th), attempt)
	}

	// A «Стоп» stored between the failed call and the registration would not wake the wait.
	if st, done, err := s.recheck(ctx, c, op, cause); done {
		return st, true, err
	}
	s.sleepUnlocked(wctx, wait)
	if ctx.Err() != nil {
		return Step{}, false, nil
	}
	return s.recheck(ctx, c, op, cause)
}

// recheck reads the stored request of c during a retry wait. A change saved meanwhile wins (see conflict); a failed
// read stops the request as the failure cause would, so no PLANNING request is left without a loop (TASK-171).
func (s *Service) recheck(ctx context.Context, c *change, op string, cause error) (Step, bool, error) {
	changed, err := s.changed(ctx, c)
	if err != nil {
		st, err := s.abort(ctx, c, op, reasonOf(ctx, cause), errors.Join(cause, err))
		return st, true, err
	}
	if changed {
		st, err := s.conflict(ctx, c, op, shared.ErrVersionConflict)
		return st, true, err
	}
	return Step{}, false, nil
}

// sleepUnlocked sleeps with s.mu released; s.mu is held again on return, a panic included. An error of Sleep is a
// woken or cancelled wait, which the caller reads from the stored request and ctx.
func (s *Service) sleepUnlocked(ctx context.Context, d time.Duration) {
	s.mu.Unlock()
	defer s.mu.Lock()
	_ = s.sleeper.Sleep(ctx, d)
}

// changed tells whether the stored request of c is no longer the one c holds: something was saved since.
func (s *Service) changed(ctx context.Context, c *change) (bool, error) {
	th, err := s.threads.Get(ctx, c.th.ID())
	if err != nil {
		return false, err
	}
	return th.Version() != c.th.Version(), nil
}

// waiter is a registered retry wait of a thread; Stop ends it.
type waiter struct{ cancel context.CancelFunc }

// waiting registers a retry wait of thread id; its ctx ends when Stop of the thread is stored.
func (s *Service) waiting(ctx context.Context, id shared.ThreadID) (context.Context, *waiter) {
	wctx, cancel := context.WithCancel(ctx)
	w := &waiter{cancel: cancel}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.waits == nil {
		s.waits = map[shared.ThreadID]*waiter{}
	}
	s.waits[id] = w
	return wctx, w
}

// waited ends the wait w of thread id.
func (s *Service) waited(id shared.ThreadID, w *waiter) {
	w.cancel()
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.waits[id] == w {
		delete(s.waits, id)
	}
}

// wake ends the retry wait of thread id, if one runs.
func (s *Service) wake(id shared.ThreadID) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if w := s.waits[id]; w != nil {
		w.cancel()
	}
}

// callModel is the Hub call of the open step of c, with its reply draft when Drafts gives one. The draft is ended
// whatever happens (a panic too) and only ever shown: nothing here reads it. A call cut by the draft's ctx (a «Стоп»
// was stored) fails like a lost connection, and the save of that failure finds the stored stop (see conflict); cut
// reports it, so that failure is not retried by itself.
func (s *Service) callModel(ctx context.Context, c *change, req values.StepRequest) (_ values.StepReply, cut bool, _ error) {
	callCtx, draft := ctx, Draft(nil)
	if s.drafts != nil {
		dctx, d := s.drafts.Reply(ctx, progressOf(c.th))
		if d != nil && dctx != nil {
			callCtx, draft = dctx, d
		}
	}
	var text func(string)
	answered := false
	if draft != nil {
		text = draft.Text
		defer func() { draft.End(answered) }()
	}

	reply, err := s.model.Step(callCtx, req, text)
	if err == nil {
		err = reply.Validate()
	}
	answered = err == nil
	return reply, err != nil && callCtx.Err() != nil, err
}

// stopReasonOf sorts a failed step (ADR-0021, TASK-116 decision d, ADR-0026 addendum 2026-11-08): a used-up quota
// keeps the same key for «Продолжить»; a failure the Hub answered (values.ErrHubFailure: a provider failure, a
// refusal, a rate limit), one it refused for good or an unusable answer stops failed; a request that never reached the
// Hub (network, timeout, unavailable) stops offline; both need «Повторить» with a new key; an expired model turn a
// restart.
func stopReasonOf(err error) values.StopReason {
	switch {
	case errors.Is(err, values.ErrTurnGone):
		return values.StopExpired
	case errors.Is(err, values.ErrInvalidResult), errors.Is(err, backoff.ErrPermanent):
		return values.StopFailed
	case errors.Is(err, shared.ErrQuotaExceeded):
		return values.StopQuota // the quota error of the Hub carries ErrHubFailure too
	case errors.Is(err, values.ErrHubFailure):
		return values.StopFailed
	}
	return values.StopOffline
}

// reasonOf is the stop reason of a failed Hub call; the caller going away is a lost connection whatever the error.
func reasonOf(ctx context.Context, err error) values.StopReason {
	if ctx.Err() != nil {
		return values.StopOffline
	}
	return stopReasonOf(err)
}

// fail stops the request with reason after a failed Hub call, the step left open. It reports true when an expired
// first step was restarted from the student's text and the loop goes on.
func (s *Service) fail(ctx context.Context, c *change, cause error, reason values.StopReason, silent bool) (bool, error) {
	s.log.WarnContext(ctx, "agent step failed", "thread", c.th.ID().String(), "reason", string(reason), "err", cause)
	if err := c.th.Stop(reason, s.clock.Now()); err != nil {
		return false, err
	}
	if turn, _ := c.th.Turn(); reason == values.StopExpired && silent && turn.CanRestartSilently() {
		if err := s.restart(ctx, c); err != nil {
			return false, err
		}
		return true, nil
	}
	if ctx.Err() != nil {
		// The caller went away; the stop is still stored so the request does not stay «running».
		if err := s.save(context.WithoutCancel(ctx), c); err != nil {
			return false, err
		}
		return false, ctx.Err()
	}
	return false, nil
}

// progressOf is the request state a Progress gets: no calls, no cards.
func progressOf(th *thread.Thread) Step {
	turn, _ := th.Turn()
	return Step{Thread: th.ID(), Turn: turn.Number(), Scope: turn.Scope(), Status: turn.Status(), StopReason: turn.StopReason()}
}

// abort stores the request stopped (an open step stays open, so it can be repeated) after a failure once a step may
// have begun; the work of the change is dropped. A request that was never stored or is not running stays as stored.
// The stored request comes back with the error, so the panel shows what is stored.
func (s *Service) abort(ctx context.Context, c *change, op string, reason values.StopReason, cause error) (Step, error) {
	ctx = context.WithoutCancel(ctx)
	th, err := s.threads.Get(ctx, c.th.ID())
	if err != nil {
		return Step{}, fmt.Errorf("%s: %w", op, errors.Join(cause, err))
	}
	stored := &change{th: th}
	if turn, _ := th.Turn(); running(turn) {
		if err := th.Stop(reason, s.clock.Now()); err != nil {
			return Step{}, fmt.Errorf("%s: %w", op, errors.Join(cause, err))
		}
		if err := s.save(ctx, stored); err != nil {
			return Step{}, fmt.Errorf("%s: %w", op, errors.Join(cause, err))
		}
	}
	return stepOf(stored), fmt.Errorf("%s: %w", op, cause)
}

// finish saves the change and returns the step.
func (s *Service) finish(ctx context.Context, c *change, op string) (Step, error) {
	if again, err := s.store(ctx, c); again {
		return s.drive(ctx, c, op)
	} else if err != nil {
		return s.conflict(ctx, c, op, err)
	}
	return stepOf(c), nil
}

// store saves c. The first save of an Ask or a card answer that meets a version conflict (its chat was saved after the
// load, nothing of the change is stored yet) rebuilds c on the stored chat once and reports again: the caller goes on
// with it.
func (s *Service) store(ctx context.Context, c *change) (again bool, err error) {
	err = s.save(ctx, c)
	redo := c.redo
	c.redo = nil
	if err == nil || redo == nil || !errors.Is(err, shared.ErrVersionConflict) {
		return false, err
	}
	fresh, rerr := redo()
	if rerr != nil {
		return false, errors.Join(err, rerr)
	}
	*c = *fresh
	return true, nil
}

// conflict handles a failed save. A version conflict means «Стоп» saved meanwhile: the stored, stopped request wins
// and the reply of the step is dropped (its step stays open, so «Продолжить» replays it for free); so does an
// «Отмена» stored during a retry wait. Another stored state comes back with the error, so the panel shows what is
// stored. Any other failure stops the stored request.
func (s *Service) conflict(ctx context.Context, c *change, op string, err error) (Step, error) {
	if !errors.Is(err, shared.ErrVersionConflict) {
		return s.abort(ctx, c, op, values.StopOffline, err)
	}
	th, gerr := s.threads.Get(ctx, c.th.ID())
	if gerr != nil {
		return Step{}, fmt.Errorf("%s: %w", op, errors.Join(err, gerr))
	}
	if turn, ok := th.Turn(); ok && (turn.Status() == thread.StatusStopped || turn.Status() == thread.StatusCancelled) {
		return stepOf(&change{th: th}), nil
	}
	return stepOf(&change{th: th}), fmt.Errorf("%s: %w", op, err)
}

// present keeps the step mentions whose objects still exist: a deleted one is dropped (not replaced), so it is neither
// sent nor accepted by the provenance check. Every step checks them again, as an earlier request may name an object
// deleted since. Without the Mentions port no earlier mention is kept.
func (s *Service) present(ctx context.Context, ms []values.StepMention) ([]values.StepMention, error) {
	var out []values.StepMention
	for _, m := range ms {
		if s.mentions == nil {
			if m.Current {
				out = append(out, m)
			}
			continue
		}
		ok, err := s.mentions.Exists(ctx, m.Ref)
		if err != nil {
			return nil, fmt.Errorf("mention %s: %w", m.Ref, err)
		}
		if ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// base is the shared context of every step: the subjects, today and the time zone of the student.
func (s *Service) base(ctx context.Context) (values.Base, error) {
	subjects, err := s.subjects.BaseSubjects(ctx)
	if err != nil {
		return values.Base{}, err
	}
	if len(subjects) > values.MaxBaseSubjects {
		subjects = subjects[:values.MaxBaseSubjects]
	}
	now := s.clock.Now()
	today, err := values.NewDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return values.Base{}, err
	}
	return values.NewBase(subjects, today, values.TimeZoneName(now))
}

// handle records a reply: the step ends, its calls are echoed in the history and each call is decided in order.
func (s *Service) handle(ctx context.Context, c *change, reply values.StepReply) error {
	now := s.clock.Now()
	calls := reply.Calls.Calls
	if err := c.th.EndStep(len(calls), reply.InputTokens, now); err != nil && !errors.Is(err, thread.ErrBudgetExceeded) {
		return err
	}
	if err := c.th.Expect(calls, now); err != nil {
		return err
	}
	sealed := values.SealedCalls{Calls: append([]values.RawCall(nil), calls...), Seal: append([]byte(nil), reply.Calls.Seal...)}
	c.add(values.HistoryTurn{Calls: &sealed})
	for _, raw := range calls {
		if err := s.decide(ctx, c, raw); err != nil {
			return err
		}
	}
	return nil
}

// decide handles one call: a refusal is answered at once (is_error for the planner, never run); a card tool moves
// the request; an R, U or C call that the thread admits goes to the Driver.
func (s *Service) decide(ctx context.Context, c *change, raw values.RawCall) error {
	now := s.clock.Now()
	call, err := values.ParseCall(raw.CallID, raw.Tool, raw.ArgumentsJSON)
	if err != nil {
		return c.refuse(raw.CallID, err, now)
	}
	if turn, _ := c.th.Turn(); !turn.Accepting() {
		why := values.RefuseNotNow
		if turn.StopReason() == values.StopBudget {
			why = values.RefuseBudget
		}
		return c.answer(values.RefusalResult(call.ID(), why), now)
	}

	args := call.Args()
	switch call.Tool() {
	case values.ToolShowPlan:
		card := planCard(args)
		if err := card.Validate(); err != nil {
			return c.answer(values.RefusalResult(call.ID(), values.RefuseInvalidPlan), now)
		}
		if err := c.th.ProposePlan(card.Steps, c.context, now); err != nil {
			return c.refuse(call.ID(), err, now)
		}
		c.cards = append(c.cards, card) // show_plan waits for «Да» or «Изменить»
	case values.ToolAskQuestions:
		round, err := c.th.AskQuestions(now)
		if err != nil {
			// A third round is refused with a note: the planner goes on with stated assumptions (decision c).
			return c.refuse(call.ID(), err, now)
		}
		c.cards = append(c.cards, questionsCard(round, args))
	case values.ToolReply, values.ToolCannotDo:
		// The assignments a reply lists pass the provenance check as any ID (ADR-0022); a refused reply shows nothing and
		// the planner goes on.
		if err := c.th.CheckRefs(call.Refs(), c.context); err != nil {
			return c.refuse(call.ID(), err, now)
		}
		var card values.Card
		if text, ok := args.Text("text"); ok {
			card = values.ReplyCard{Text: text, Assignments: args.CardIDs("assignments")}
		} else {
			card = values.CannotDoCard{YourPart: args.TextList("your_part"), OurPart: args.TextList("our_part")}
		}
		c.cards = append(c.cards, card)
		if err := c.answer(values.ShownResult(call.ID()), now); err != nil {
			return err
		}
		return c.th.Complete(now)
	default:
		facts, err := s.facts.Facts(ctx, call)
		if err != nil {
			s.log.WarnContext(ctx, "agent call facts failed", "tool", call.Tool().String(), "err", err)
			return c.answer(values.RefusalResult(call.ID(), values.RefuseFailed), now)
		}
		facts.Context = c.context
		adm, err := c.th.Admit(call, facts, now)
		if err != nil {
			return c.refuse(call.ID(), err, now)
		}
		c.calls = append(c.calls, Call{
			Call: call, Class: adm.Class, Paid: adm.Paid, PlanStep: adm.PlanStep, Card: adm.Card, NoUndo: adm.NoUndo,
		})
	}
	return nil
}

// planCard is the card of a show_plan call; the step that shows it and Current build it here.
func planCard(args values.Args) values.PlanCard {
	summary, _ := args.Text("summary")
	return values.PlanCard{Summary: summary, Steps: args.PlanSteps("steps"), Notes: args.TextList("notes")}
}

// questionsCard is the card of question round round of an ask_questions call; the step that shows it and Current
// build it here.
func questionsCard(round int, args values.Args) values.QuestionsCard {
	return values.QuestionsCard{Round: round, Questions: args.Questions("questions")}
}

func (c *change) answer(r values.ToolResult, now time.Time) error {
	return c.th.Answer(r, values.Ref{}, now)
}

// refuse answers a call with the refusal its error means; an error that is not a refusal is returned.
func (c *change) refuse(callID string, cause error, now time.Time) error {
	why, ok := thread.RefusalOf(cause)
	if !ok {
		return cause
	}
	return c.answer(values.RefusalResult(callID, why), now)
}
