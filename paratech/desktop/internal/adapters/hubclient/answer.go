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
//
// Like a streamed Step (ADR-0026 addendum 2026-11-08, items 5 and 6) the stream asks for the Hub's heartbeat and has
// no total cap of RequestTimeout: it ends as ErrTimeout when the Hub sends nothing for RequestTimeout before the first
// message or for Config.StepIdleTimeout after one, or after Config.AnswerTimeout. The unary fallback is bounded by
// RequestTimeout.
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

	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	spec := &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_ANSWER,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: answer.MaxOutputTokens,
	}

	out, older, err := c.streamAnswer(ctx, gateway, spec, key, partial)
	if !older {
		return out, err
	}

	// The unary read reports no progress, so it keeps the total cap of a call, on a context of its own.
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err = completeAnswer(callCtx, gateway, spec, key)
	if err != nil {
		return "", answerError(ctx, callCtx, err)
	}
	return out, nil
}

// streamAnswer reads CompleteStream within c.answerTO, cut when the Hub sends nothing for c.timeout before the first
// message or for c.stepIdle after one (watchStream). older reports an unimplemented stream while ctx lives (an older
// Hub); any other error is classified by answerError.
func (c *Client) streamAnswer(ctx context.Context, gateway hubv1connect.GatewayServiceClient, spec *hubv1.CompletionSpec,
	key string, partial func(string),
) (out string, older bool, err error) {
	callCtx, seen, release := watchStream(ctx, c.timeout, c.stepIdle, c.answerTO)
	defer release()

	out, err = readAnswerStream(callCtx, gateway, spec, key, partial, seen)
	switch {
	case err == nil:
		return out, false, nil
	case connect.CodeOf(err) == connect.CodeUnimplemented && ctx.Err() == nil:
		return "", true, err
	}
	return "", false, answerError(ctx, callCtx, err)
}

// readAnswerStream reads CompleteStream with the Hub's heartbeat asked for, calling seen on every message: a heartbeat
// or an event this client does not know only does that. Errors are those of the generated client, or an *Error of a
// broken stream.
func readAnswerStream(ctx context.Context, gateway hubv1connect.GatewayServiceClient, spec *hubv1.CompletionSpec,
	key string, partial func(string), seen func(),
) (string, error) {
	sreq := connect.NewRequest(&hubv1.CompleteStreamRequest{Spec: spec, Heartbeat: true})
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
		seen()
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

// answerError classifies a failed read like Complete; a permanent provider error is a refused answer. The caller's ctx
// going away stays its error; a silent or overlong stream (the cause of callCtx) is a timeout.
func answerError(ctx, callCtx context.Context, err error) error {
	if perr := callerEnd(ctx); perr != nil {
		return fmt.Errorf("%s: %w", opAnswer, perr)
	}
	if e := watchTimeout(callCtx, opAnswer); e != nil {
		return e
	}
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return unavailable(opAnswer, ErrTimeout)
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	e := mapCallError(opAnswer, err)
	if errors.Is(e, ErrModelAnswer) || errors.Is(e, ErrProvider) && errors.Is(e, backoff.ErrPermanent) {
		// A permanent provider error counts as a refused answer (charged or not). A model answer the Hub refused by
		// the schema is refused whatever its retryable flag: asking the same key again replays the refusal, so a
		// ticket run must record it instead of waiting as for a lost connection.
		e.extra = append(e.extra, answer.ErrInvalidAnswer)
	}
	if errors.Is(e, ErrIdempotencyConflict) {
		// The key is derived from the content, so a conflict here only comes from a Hub or desktop upgrade that changed
		// the request fingerprint (output cap, prompt version). The same key keeps conflicting: a ticket run records one
		// refused ticket instead of failing the whole job. The text still names the conflict for desktop.log.
		e.extra = append(e.extra, answer.ErrInvalidAnswer)
	}
	return e
}
