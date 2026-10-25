package agentturn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

func running(turn thread.Turn) bool {
	return turn.Status() == thread.StatusPlanning || turn.Status() == thread.StatusExecuting
}

// drive runs planner steps while the request can go on by itself, then saves the change. It stops when calls wait
// for the Driver, the request waits for the student (plan, questions), has ended or has stopped. A step whose calls
// were all answered by the case (refusals, a refused third question round) is followed by the next step at once.
func (s *Service) drive(ctx context.Context, c *change, op string) (Step, error) {
	silent := true // an expired first step restarts once without asking the student
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
				return Step{}, fmt.Errorf("%s: %w", op, err)
			}
		}

		key, err := c.th.StepKey()
		if err != nil {
			return Step{}, fmt.Errorf("%s: %w", op, err)
		}
		base, err := s.base(ctx)
		if err != nil {
			return Step{}, fmt.Errorf("%s: base: %w", op, err)
		}
		// The open step is stored before the call: a crash or a lost answer leaves a step «Продолжить» can repeat.
		if err := s.save(ctx, c); err != nil {
			return s.conflict(ctx, c, op, err)
		}
		turn, _ = c.th.Turn()
		entries, err := s.history.Since(ctx, c.th.ID(), c.th.HistoryFrom(), historyLimit)
		if err != nil {
			return s.abort(ctx, c, op, values.StopOffline, err)
		}
		c.context = values.NewRefSet(base.Refs()...)
		c.context.Add(turn.Scope().Refs()...)
		req := values.StepRequest{
			Key: key, Thread: c.th.ID(), Base: base, Scope: turn.Scope(), History: thread.Window(entries, turn.Number()),
			MaxOutputTokens: MaxOutputTokens,
		}

		if s.progress != nil {
			s.progress.Started(ctx, progressOf(c.th))
		}
		reply, err := s.model.Step(ctx, req)
		if err == nil {
			err = reply.Validate()
		}
		if err != nil {
			again, ferr := s.fail(ctx, c, err, silent)
			if ferr != nil {
				return Step{}, fmt.Errorf("%s: %w", op, ferr)
			}
			if again {
				silent = false
				continue
			}
			return s.finish(ctx, c, op)
		}
		if err := s.handle(ctx, c, reply); err != nil {
			return s.abort(ctx, c, op, values.StopFailed, err)
		}
	}
}

// stopReasonOf sorts a failed step (ADR-0021, TASK-116 decision d): an answer that may exist or may come later
// (network, timeout, a busy or overloaded Hub) and a used-up quota keep the same key for «Продолжить»; a failure the
// Hub refused for good or answered unusably needs «Повторить» with a new key; an expired model turn a restart.
func stopReasonOf(err error) values.StopReason {
	switch {
	case errors.Is(err, values.ErrTurnGone):
		return values.StopExpired
	case errors.Is(err, values.ErrInvalidResult), errors.Is(err, backoff.ErrPermanent):
		return values.StopFailed
	case errors.Is(err, shared.ErrQuotaExceeded):
		return values.StopQuota
	}
	return values.StopOffline
}

// fail stops the request after a failed Hub call, the step left open. It reports true when an expired first step
// was restarted from the student's text and the loop goes on.
func (s *Service) fail(ctx context.Context, c *change, cause error, silent bool) (bool, error) {
	reason := stopReasonOf(cause)
	if ctx.Err() != nil {
		reason = values.StopOffline
	}
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

// abort stores the request stopped (step open, so it can be repeated) after a local failure in the middle of a step;
// the work of the step is dropped. The stored request comes back with the error, so the panel shows what is stored.
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
	if err := s.save(ctx, c); err != nil {
		return s.conflict(ctx, c, op, err)
	}
	return stepOf(c), nil
}

// conflict handles a failed save. A version conflict means «Стоп» saved meanwhile: the stored, stopped request wins
// and the reply of the step is dropped (its step stays open, so «Продолжить» replays it for free). Another stored
// state comes back with the error, so the panel shows what is stored.
func (s *Service) conflict(ctx context.Context, c *change, op string, err error) (Step, error) {
	if !errors.Is(err, shared.ErrVersionConflict) {
		return Step{}, fmt.Errorf("%s: %w", op, err)
	}
	th, gerr := s.threads.Get(ctx, c.th.ID())
	if gerr != nil {
		return Step{}, fmt.Errorf("%s: %w", op, errors.Join(err, gerr))
	}
	if turn, ok := th.Turn(); ok && turn.Status() == thread.StatusStopped {
		return stepOf(&change{th: th}), nil
	}
	return stepOf(&change{th: th}), fmt.Errorf("%s: %w", op, err)
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
		summary, _ := args.Text("summary")
		card := values.PlanCard{Summary: summary, Steps: args.PlanSteps("steps"), Notes: args.TextList("notes")}
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
		c.cards = append(c.cards, values.QuestionsCard{Round: round, Questions: args.Questions("questions")})
	case values.ToolReply, values.ToolCannotDo:
		var card values.Card
		if text, ok := args.Text("text"); ok {
			card = values.ReplyCard{Text: text}
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
		c.calls = append(c.calls, Call{Call: call, Class: adm.Class, Paid: adm.Paid, PlanStep: adm.PlanStep})
	}
	return nil
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
