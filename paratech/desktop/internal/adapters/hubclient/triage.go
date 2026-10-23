package hubclient

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const opTriage = "hubclient: triage assignment"

// TriageAssignment runs the quarantined reader of an assignment (OPERATION_TYPE_TRIAGE_ASSIGNMENT, ADR-0022; the
// turnkey.Reader port) with the URL, key, limits and error classes of Complete. It sends no instructions and no model
// alias: the Hub uses its own prompt and alias. The returned output is untrusted model text; the only thing to do with
// it is work.Request.Parse. A permanent ErrProvider (the Hub's charged schema refusal or an uncharged model refusal)
// also matches work.ErrInvalidAnswer and uses up an attempt; the next attempt has a new key.
func (c *Client) TriageAssignment(ctx context.Context, req work.Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", opTriage, err)
	}
	if req.IsZero() {
		return "", permanent(opTriage, ErrInvalidRequest)
	}
	base, err := c.baseURL(ctx, opTriage)
	if err != nil {
		return "", err
	}
	key, err := c.apiKey(ctx, opTriage)
	if err != nil {
		return "", err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: work.MaxOutputTokens,
	}})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return "", fmt.Errorf("%s: %w", opTriage, perr)
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", unavailable(opTriage, ErrTimeout)
		}
		e := mapCallError(opTriage, err)
		if errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
			// A permanent provider error counts as a refused answer (charged or not, the attempt is used).
			e.extra = append(e.extra, work.ErrInvalidAnswer)
		}
		return "", e
	}
	res := resp.Msg.GetResult()
	if res == nil {
		return "", permanent(opTriage, ErrBadResponse)
	}
	return res.GetOutput(), nil
}
