package hubclient

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/library/inspection"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const opInspect = "hubclient: inspect material"

// InspectMaterial runs the quarantined reader of a library material (OPERATION_TYPE_INSPECT_MATERIAL, ADR-0022; the
// library.MaterialReader port) with the URL, key, limits and error classes of Complete. It sends no instructions and
// no model alias: the Hub uses its own prompt and alias. The returned output is untrusted model text; the only thing
// to do with it is inspection.Request.Parse. A permanent ErrProvider (the Hub's charged schema refusal or an uncharged model refusal) also matches
// inspection.ErrInvalidAnswer and uses up an attempt; the next attempt has a new key.
func (c *Client) InspectMaterial(ctx context.Context, req inspection.Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", opInspect, err)
	}
	if req.IsZero() {
		return "", permanent(opInspect, ErrInvalidRequest)
	}
	base, err := c.baseURL(ctx, opInspect)
	if err != nil {
		return "", err
	}
	key, err := c.apiKey(ctx, opInspect)
	if err != nil {
		return "", err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: inspection.MaxOutputTokens,
	}})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return "", fmt.Errorf("%s: %w", opInspect, perr)
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", unavailable(opInspect, ErrTimeout)
		}
		e := mapCallError(opInspect, err)
		if errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
			// A permanent provider error counts as a refused answer (charged or not, the attempt is used).
			e.extra = append(e.extra, inspection.ErrInvalidAnswer)
		}
		return "", e
	}
	res := resp.Msg.GetResult()
	if res == nil {
		return "", permanent(opInspect, ErrBadResponse)
	}
	return res.GetOutput(), nil
}
