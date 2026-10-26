package hubclient

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/services/backoff"
)

const opAdvise = "hubclient: advise step"

// StepAdvisor answers «Спросить по шагу» (the turnkey.StepAdvisor port, TASK-179) through Complete on the
// homework_draft operation with the desktop's prompt (prompts.BuildStepAdvice); the Hub has no operation of its own
// for it (ADR-0025 addendum).
type StepAdvisor struct {
	c     *Client
	alias string
}

// StepAdvisor returns the advisor that asks with model alias modelAlias, the alias of the work writing.
func (c *Client) StepAdvisor(modelAlias string) *StepAdvisor {
	return &StepAdvisor{c: c, alias: modelAlias}
}

// AdviseStep makes one paid call per question; the key follows the question (work.StepQuestion.Key) and the prompt
// version, so the same question again is a free replay on the Hub. The output is untrusted model text for a card only
// (work.CheckAdvice reads it). Errors are those of Complete; a permanent ErrProvider (a model refusal) also matches
// work.ErrInvalidAnswer, as for the reader.
func (a *StepAdvisor) AdviseStep(ctx context.Context, q work.StepQuestion) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", opAdvise, err)
	}
	if q.IsZero() {
		return "", permanent(opAdvise, ErrInvalidRequest)
	}
	req, err := prompts.BuildStepAdvice(q.Data(), a.alias, q.Key())
	if err != nil {
		return "", permanent(opAdvise, ErrInvalidRequest)
	}

	res, err := a.c.Complete(ctx, req)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
			e.extra = append(e.extra, work.ErrInvalidAnswer)
		}
		return "", err
	}
	return res.Output.Unvalidated(), nil
}
