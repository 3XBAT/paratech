package app

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

// ticketAnswers is the Answerer port of the tickets over the answers with citations (TASK-133): mapping only. It
// gives the errors the port names, so a run records a refused ticket and waits for the network or the quota.
type ticketAnswers struct {
	answers *answering.Service
}

// ticketScope is the answer reuse scope of every ticket: the same ticket in the same subject reuses its paid answer.
const ticketScope = "tk:v1"

// Ask answers one ticket from the lessons of the subject; nil when no passage matches.
func (t ticketAnswers) Ask(ctx context.Context, question string, sub shared.SubjectID) (*answer.Answer, error) {
	asked, err := t.answers.Ask(ctx, question, sub, ticketScope)
	switch {
	case err == nil:
		return asked.Answer, nil
	case errors.Is(err, answering.ErrReaderRefused) && !errors.Is(err, answer.ErrInvalidAnswer):
		return nil, errors.Join(err, answer.ErrInvalidAnswer)
	case errors.Is(err, answering.ErrInvalidQuestion) && !errors.Is(err, answer.ErrInvalidQuestion):
		return nil, errors.Join(err, answer.ErrInvalidQuestion)
	case errors.Is(err, answering.ErrQuota) && !errors.Is(err, shared.ErrQuotaExceeded):
		return nil, errors.Join(err, shared.ErrQuotaExceeded)
	case errors.Is(err, answering.ErrNoConnection) && !errors.Is(err, shared.ErrNetworkUnavailable):
		// Not reached or not set up: the run waits like any Hub job and asks the same key again, free.
		return nil, errors.Join(err, shared.ErrNetworkUnavailable)
	}
	return nil, err
}

// Answer loads a stored answer; none wraps shared.ErrNotFound.
func (t ticketAnswers) Answer(ctx context.Context, id shared.AnswerID) (*answer.Answer, error) {
	a, err := t.answers.Answer(ctx, id)
	if errors.Is(err, answering.ErrNotFound) {
		return nil, fmt.Errorf("%w: %w", err, shared.ErrNotFound)
	}
	return a, err
}

// runnerCanceller cancels a job through the core's runner, which is built after the use cases whose jobs it runs.
type runnerCanceller struct {
	c *core
}

// Cancel cancels the job; before the runner exists there is nothing running to cancel.
func (r runnerCanceller) Cancel(ctx context.Context, id shared.JobID) error {
	if r.c.runner == nil {
		return errors.New("app: no job runner yet")
	}
	return r.c.runner.Cancel(ctx, id)
}
