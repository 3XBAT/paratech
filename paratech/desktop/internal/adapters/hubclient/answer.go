package hubclient

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const opAnswer = "hubclient: answer question"

// AnswerQuestion runs the quarantined reader of a question about the student's lessons (OPERATION_TYPE_ANSWER,
// ADR-0022; the answering.Reader port) with the URL, key, limits and error classes of Complete. It sends no
// instructions and no model alias: the Hub uses its own prompt and alias. The returned output is untrusted model
// text; the only thing to do with it is answer.Request.Parse. A permanent ErrProvider (the Hub's charged schema
// refusal or an uncharged model refusal) also matches answer.ErrInvalidAnswer; the same key replays it.
func (c *Client) AnswerQuestion(ctx context.Context, req answer.Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", opAnswer, err)
	}
	if req.IsZero() {
		return "", permanent(opAnswer, ErrInvalidRequest)
	}
	base, err := c.baseURL(ctx, opAnswer)
	if err != nil {
		return "", err
	}
	key, err := c.apiKey(ctx, opAnswer)
	if err != nil {
		return "", err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_ANSWER,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: answer.MaxOutputTokens,
	}})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return "", fmt.Errorf("%s: %w", opAnswer, perr)
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", unavailable(opAnswer, ErrTimeout)
		}
		e := mapCallError(opAnswer, err)
		if errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
			// A permanent provider error counts as a refused answer (charged or not).
			e.extra = append(e.extra, answer.ErrInvalidAnswer)
		}
		return "", e
	}
	res := resp.Msg.GetResult()
	if res == nil {
		return "", permanent(opAnswer, ErrBadResponse)
	}
	return res.GetOutput(), nil
}
