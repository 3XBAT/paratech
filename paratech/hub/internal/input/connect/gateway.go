package connect

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	connectrpc "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// Fixed client messages (errors.proto: never request text, model output, provider text or internals).
const (
	msgInvalidRequest  = "invalid completion request"
	msgPayloadTooLarge = "instructions and untrusted data exceed 1 MiB"
	msgModelNotAllowed = "model alias is not allowed"
	msgRateLimited     = "too many requests, retry later"
	msgQuotaExceeded   = "quota exceeded"
	msgProviderFailed  = "the LLM provider failed"
	msgConflict        = "the idempotency key was used with a different request"
	msgRunning         = "a call with this idempotency key is still running"
	msgInternal        = "internal error"
)

// Completer is what the gateway handler needs from the gateway use case (ARCH-42).
type Completer interface {
	Complete(ctx context.Context, cmd gateway.Command) (gateway.Result, error)
}

// Gateway serves GatewayService.Complete. CompleteStream stays unimplemented: the provider port has no streaming yet
// (ADR-0010).
type Gateway struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	svc Completer
	log *slog.Logger
}

var _ hubv1connect.GatewayServiceHandler = (*Gateway)(nil)

// NewGateway builds the handler.
func NewGateway(svc Completer, log *slog.Logger) (*Gateway, error) {
	if svc == nil || log == nil {
		return nil, errors.New("connect: gateway service and logger are required")
	}
	return &Gateway{svc: svc, log: log}, nil
}

// Complete runs one completion for the authenticated caller. The caller comes only from the API key; the request has
// no field for a URL, host or provider (gateway.proto).
func (g *Gateway) Complete(ctx context.Context, req *connectrpc.Request[hubv1.CompleteRequest]) (*connectrpc.Response[hubv1.CompleteResponse], error) {
	owner, ok := APIKeyFrom(ctx)
	if !ok || owner == "" {
		return nil, rejectedError() // the auth middleware is missing; fail closed
	}
	spec := req.Msg.GetSpec()
	res, err := g.svc.Complete(ctx, gateway.Command{
		Owner: owner, IdempotencyKey: spec.GetIdempotencyKey(), Operation: operationOf(spec.GetOperationType()),
		ModelAlias: spec.GetModelAlias(), Instructions: spec.GetInstructions(), UntrustedData: spec.GetUntrustedData(),
		MaxOutputTokens: int64(spec.GetMaxOutputTokens()),
	})
	if err != nil {
		return nil, g.completeError(ctx, err)
	}

	quota, err := quotaToProto(res.Quota.Unit, res.Quota.Remaining, res.Quota.Reserved, res.Quota.Limit)
	if err != nil {
		g.log.ErrorContext(ctx, "completion quota cannot be encoded", "err", err)
		return nil, connectrpc.NewError(connectrpc.CodeInternal, errors.New(msgInternal))
	}
	return connectrpc.NewResponse(&hubv1.CompleteResponse{Result: &hubv1.CompletionResult{
		// Protobuf strings must be UTF-8; the output stays untrusted text for the client to validate.
		Output:        strings.ToValidUTF8(res.Output, "�"),
		Usage:         &hubv1.Usage{InputTokens: res.InputTokens, OutputTokens: res.OutputTokens},
		Quota:         quota,
		ReservationId: res.ReservationID,
		Replayed:      res.Replayed,
	}}), nil
}

// completeError maps a gateway error to its Connect code and ErrorDetail (ADR-0005(c), errors.proto).
func (g *Gateway) completeError(ctx context.Context, err error) error {
	var (
		limited  *gateway.RateLimitedError
		exceeded *gateway.QuotaExceededError
		failed   *gateway.ProviderFailedError
	)
	switch {
	case errors.As(err, &limited):
		return detailedError(connectrpc.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Message: msgRateLimited, Retryable: true,
			RetryAfter: durationpb.New(max(limited.RetryAfter, time.Millisecond)), // always set for RATE_LIMITED
		})
	case errors.As(err, &exceeded):
		detail := &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: msgQuotaExceeded}
		q := exceeded.Quota
		if snapshot, err := quotaToProto(q.Unit, q.Remaining, q.Reserved, q.Limit); err == nil {
			detail.Quota = snapshot
		}
		return detailedError(connectrpc.CodeResourceExhausted, detail)
	case errors.As(err, &failed):
		detail := &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: msgProviderFailed, Retryable: failed.Transient}
		if failed.Transient && failed.RetryAfter > 0 {
			detail.RetryAfter = durationpb.New(failed.RetryAfter)
		}
		return detailedError(connectrpc.CodeUnavailable, detail)
	case errors.Is(err, gateway.ErrIdempotencyConflict):
		return detailedError(connectrpc.CodeFailedPrecondition, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, Message: msgConflict,
		})
	case errors.Is(err, gateway.ErrAttemptRunning):
		return connectrpc.NewError(connectrpc.CodeAborted, errors.New(msgRunning))
	case errors.Is(err, values.ErrPayloadTooLarge):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgPayloadTooLarge))
	case errors.Is(err, modelallowlist.ErrModelNotAllowed):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgModelNotAllowed))
	case errors.Is(err, gateway.ErrInvalidRequest):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgInvalidRequest))
	case ctx.Err() != nil:
		return connectrpc.NewError(connectrpc.CodeCanceled, errors.New(msgCanceled))
	default:
		g.log.ErrorContext(ctx, "completion failed", "err", err)
		return connectrpc.NewError(connectrpc.CodeInternal, errors.New(msgInternal))
	}
}

// detailedError is code with an ErrorDetail whose message is also the error message; new per call, as its metadata is
// mutable.
func detailedError(code connectrpc.Code, detail *hubv1.ErrorDetail) *connectrpc.Error {
	e := connectrpc.NewError(code, errors.New(detail.GetMessage()))
	if d, err := connectrpc.NewErrorDetail(detail); err == nil { // without the detail the code still classifies
		e.AddDetail(d)
	}
	return e
}

// operationOf maps the contract enum to the domain operation; UNSPECIFIED and unknown values map to "", which the
// domain rejects.
func operationOf(op hubv1.OperationType) string {
	switch op {
	case hubv1.OperationType_OPERATION_TYPE_SUMMARY:
		return string(values.OperationSummary)
	case hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES:
		return string(values.OperationExtractHomework)
	case hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT:
		return string(values.OperationHomeworkDraft)
	case hubv1.OperationType_OPERATION_TYPE_DETAILED_NOTES:
		return string(values.OperationDetailedNotes)
	default:
		return ""
	}
}
