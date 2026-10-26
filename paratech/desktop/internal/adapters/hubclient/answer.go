package hubclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
//
// It reads CompleteStream (ADR-0026) and hands each accumulated output to a non-nil partial, in order; a Hub without
// it (unimplemented) is asked through Complete with the same spec, so the same key. The result is
// CompletionResult.output, never the deltas; a stream without a result is ErrBadResponse. The deltas together are
// bounded like one answer.
func (c *Client) AnswerQuestion(ctx context.Context, req answer.Request, partial func(soFar string)) (string, error) {
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
	spec := &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_ANSWER,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: answer.MaxOutputTokens,
	}

	out, err := streamAnswer(callCtx, gateway, spec, key, partial)
	if connect.CodeOf(err) == connect.CodeUnimplemented && ctx.Err() == nil {
		out, err = completeAnswer(callCtx, gateway, spec, key)
	}
	if err != nil {
		return "", answerError(ctx, callCtx, err)
	}
	return out, nil
}

// streamAnswer reads CompleteStream. Errors are those of the generated client, or an *Error of a broken stream.
func streamAnswer(ctx context.Context, gateway hubv1connect.GatewayServiceClient, spec *hubv1.CompletionSpec, key string,
	partial func(string),
) (string, error) {
	sreq := connect.NewRequest(&hubv1.CompleteStreamRequest{Spec: spec})
	sreq.Header().Set("Authorization", "Bearer "+key)
	stream, err := gateway.CompleteStream(ctx, sreq)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	var (
		sb  strings.Builder
		res *hubv1.CompletionResult
	)
	for stream.Receive() {
		if res != nil {
			return "", permanent(opAnswer, ErrBadResponse) // the result is the last message
		}
		switch ev := stream.Msg().GetEvent().(type) {
		case *hubv1.CompleteStreamResponse_Delta:
			if ev.Delta == "" {
				continue
			}
			if sb.Len()+len(ev.Delta) > maxResponseBytes {
				return "", permanent(opAnswer, ErrBadResponse)
			}
			sb.WriteString(ev.Delta)
			if partial != nil {
				partial(sb.String())
			}
		case *hubv1.CompleteStreamResponse_Result:
			res = ev.Result
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	if res == nil {
		return "", permanent(opAnswer, ErrBadResponse)
	}
	return res.GetOutput(), nil
}

// completeAnswer is the unary read of a Hub without CompleteStream.
func completeAnswer(ctx context.Context, gateway hubv1connect.GatewayServiceClient, spec *hubv1.CompletionSpec, key string) (string, error) {
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: spec})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(ctx, creq)
	if err != nil {
		return "", err
	}
	res := resp.Msg.GetResult()
	if res == nil {
		return "", permanent(opAnswer, ErrBadResponse)
	}
	return res.GetOutput(), nil
}

// answerError classifies a failed read like Complete; a permanent provider error is a refused answer.
func answerError(ctx, callCtx context.Context, err error) error {
	if perr := ctx.Err(); perr != nil {
		return fmt.Errorf("%s: %w", opAnswer, perr)
	}
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return unavailable(opAnswer, ErrTimeout)
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	e := mapCallError(opAnswer, err)
	if errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
		// A permanent provider error counts as a refused answer (charged or not).
		e.extra = append(e.extra, answer.ErrInvalidAnswer)
	}
	return e
}
