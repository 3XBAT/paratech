package agentturn

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// opCurrent is the operation name of Current, for error texts.
const opCurrent = "agentturn: current"

// errNoCard is a waiting request whose card call is not stored: a broken invariant, not a missing chat.
var errNoCard = errors.New("the card the request waits on is not stored")

// Current reads the current request of chat id as the panel shows it (ADR-0028, the live card after a restart): its
// state and the card it waits on — the plan for «Да», the questions, «Продолжить» of a stopped request — built from the
// stored call by the code of the step that showed it. It only reads: no Hub call, no step, no change; it does not wait
// for a running step, so the updates of that step follow by thread. A running request comes without cards. ok is false
// when the chat has no request or its request ended. A chat that does not exist wraps shared.ErrNotFound.
func (s *Service) Current(ctx context.Context, id shared.ThreadID) (Step, bool, error) {
	if err := id.Validate(); err != nil {
		return Step{}, false, fmt.Errorf("%s: %w: %w", opCurrent, ErrInvalidInput, err)
	}
	th, err := s.threads.Get(ctx, id)
	if err != nil {
		return Step{}, false, fmt.Errorf("%s: %w", opCurrent, err)
	}
	turn, ok := th.Turn()
	if !ok || turn.Status().IsTerminal() {
		return Step{}, false, nil
	}

	c := &change{th: th}
	switch turn.Status() {
	case thread.StatusAwaitingPlan:
		call, err := s.waitingCall(ctx, th, turn, values.ToolShowPlan)
		if err != nil {
			return Step{}, false, err
		}
		c.cards = append(c.cards, planCard(call.Args()))
	case thread.StatusAwaitingAnswers:
		call, err := s.waitingCall(ctx, th, turn, values.ToolAskQuestions)
		if err != nil {
			return Step{}, false, err
		}
		// The open round is the last one counted (thread.AskQuestions).
		c.cards = append(c.cards, questionsCard(turn.Budget().QuestionRounds, call.Args()))
	}
	return stepOf(c), true, nil
}

// waitingCall is the stored card call of tool that the request waits on: its ID from the open calls, its arguments
// from the sealed echo of the request's history.
func (s *Service) waitingCall(ctx context.Context, th *thread.Thread, turn thread.Turn, tool values.Tool) (values.Call, error) {
	id := ""
	for _, oc := range turn.OpenCalls() {
		if oc.Tool == tool && !oc.Answered {
			id = oc.ID
			break
		}
	}
	if id == "" {
		return values.Call{}, fmt.Errorf("%s: %w: no open %s call", opCurrent, errNoCard, tool)
	}
	entries, err := s.history.Since(ctx, th.ID(), turn.Number(), historyLimit)
	if err != nil {
		return values.Call{}, fmt.Errorf("%s: %w", opCurrent, err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Turn != turn.Number() || e.Entry.Calls == nil {
			continue
		}
		for _, raw := range e.Entry.Calls.Calls {
			if raw.CallID != id {
				continue
			}
			call, err := values.ParseCall(raw.CallID, raw.Tool, raw.ArgumentsJSON)
			if err != nil {
				return values.Call{}, fmt.Errorf("%s: %w", opCurrent, err)
			}
			return call, nil
		}
	}
	return values.Call{}, fmt.Errorf("%s: %w: no echo of %s", opCurrent, errNoCard, tool)
}
