package connect

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	connectrpc "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
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
	msgInvalidAnswer   = "the model answered, but its answer does not match the schema of the operation"
	msgConflict        = "the idempotency key was used with a different request"
	msgRunning         = "a call with this idempotency key is still running"
	msgInternal        = "internal error"
	msgNoInstructions  = "the operation takes no instructions: the Hub uses its own prompt"
	msgServerAlias     = "the operation runs on the Hub's model alias: send none or that one"
	msgUnavailableOp   = "the operation is not available on this Hub"
	msgNotStreamable   = "only the answer operation is streamed: use Complete"
	msgStreamDisabled  = "CompleteStream is not served by this Hub"
)

// Completer is what the gateway handler needs from the gateway use case (ARCH-42).
type Completer interface {
	Complete(ctx context.Context, cmd gateway.Command) (gateway.Result, error)
}

// Streamer is what the CompleteStream handler needs from the gateway use case. onDelta must not block.
type Streamer interface {
	CompleteStream(ctx context.Context, cmd gateway.Command, onDelta func(chunk string)) (gateway.Result, error)
}

// Gateway serves GatewayService.Complete and, when given, CompleteStream (ADR-0026) and AgentStep (ADR-0023).
type Gateway struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	svc    Completer
	stream Streamer
	agent  AgentStepper
	log    *slog.Logger
}

var _ hubv1connect.GatewayServiceHandler = (*Gateway)(nil)

// GatewayOption changes a default of NewGateway.
type GatewayOption func(*Gateway)

// WithAgentStep serves AgentStep with a. Without it AgentStep answers unimplemented.
func WithAgentStep(a AgentStepper) GatewayOption {
	return func(g *Gateway) { g.agent = a }
}

// WithCompleteStream serves CompleteStream with s. Without it CompleteStream answers unimplemented.
func WithCompleteStream(s Streamer) GatewayOption {
	return func(g *Gateway) { g.stream = s }
}

// NewGateway builds the handler.
func NewGateway(svc Completer, log *slog.Logger, opts ...GatewayOption) (*Gateway, error) {
	if svc == nil || log == nil {
		return nil, errors.New("connect: gateway service and logger are required")
	}
	g := &Gateway{svc: svc, log: log}
	for _, opt := range opts {
		opt(g)
	}
	return g, nil
}

// Complete runs one completion for the authenticated caller. The caller comes only from the API key; the request has
// no field for a URL, host or provider (gateway.proto).
func (g *Gateway) Complete(ctx context.Context, req *connectrpc.Request[hubv1.CompleteRequest]) (*connectrpc.Response[hubv1.CompleteResponse], error) {
	owner, ok := APIKeyFrom(ctx)
	if !ok || owner == "" {
		return nil, rejectedError() // the auth middleware is missing; fail closed
	}
	res, err := g.svc.Complete(ctx, commandOf(owner, req.Msg.GetSpec()))
	if err != nil {
		return nil, g.completeError(ctx, err)
	}
	result, err := g.resultOf(ctx, res)
	if err != nil {
		return nil, err
	}
	return connectrpc.NewResponse(&hubv1.CompleteResponse{Result: result}), nil
}

// CompleteStream runs one ANSWER completion and streams it (ADR-0026, gateway.proto): deltas, then the result. The
// use case never waits for the client: chunks are queued and sent by a separate goroutine, so a slow or gone client
// does not hold the provider call. After an error no queued delta is sent.
func (g *Gateway) CompleteStream(ctx context.Context, req *connectrpc.Request[hubv1.CompleteStreamRequest],
	stream *connectrpc.ServerStream[hubv1.CompleteStreamResponse]) error {
	if g.stream == nil {
		return connectrpc.NewError(connectrpc.CodeUnimplemented, errors.New(msgStreamDisabled))
	}
	owner, ok := APIKeyFrom(ctx)
	if !ok || owner == "" {
		return rejectedError() // the auth middleware is missing; fail closed
	}

	out := newDeltaSender(func(chunk string) error {
		return stream.Send(&hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Delta{Delta: strings.ToValidUTF8(chunk, "�")}})
	})
	res, err := g.stream.CompleteStream(ctx, commandOf(owner, req.Msg.GetSpec()), out.push)
	if err != nil {
		_ = out.finish(true)
		return g.completeError(ctx, err)
	}
	if err := out.finish(false); err != nil {
		// The client is gone; the call is settled and stored, a retry with the key replays it.
		return connectrpc.NewError(connectrpc.CodeCanceled, errors.New(msgCanceled))
	}
	result, err := g.resultOf(ctx, res)
	if err != nil {
		return err
	}
	return stream.Send(&hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Result{Result: result}})
}

// commandOf maps a spec to the use case command of owner.
func commandOf(owner shared.APIKeyID, spec *hubv1.CompletionSpec) gateway.Command {
	return gateway.Command{
		Owner: owner, IdempotencyKey: spec.GetIdempotencyKey(), Operation: operationOf(spec.GetOperationType()),
		ModelAlias: spec.GetModelAlias(), Instructions: spec.GetInstructions(), UntrustedData: spec.GetUntrustedData(),
		MaxOutputTokens: int64(spec.GetMaxOutputTokens()),
	}
}

// resultOf encodes a result of the use case.
func (g *Gateway) resultOf(ctx context.Context, res gateway.Result) (*hubv1.CompletionResult, error) {
	quota, err := quotaToProto(res.Quota.Unit, res.Quota.Remaining, res.Quota.Reserved, res.Quota.Limit)
	if err != nil {
		g.log.ErrorContext(ctx, "completion quota cannot be encoded", "err", err)
		return nil, connectrpc.NewError(connectrpc.CodeInternal, errors.New(msgInternal))
	}
	return &hubv1.CompletionResult{
		// Protobuf strings must be UTF-8; the output stays untrusted text for the client to validate.
		Output:        strings.ToValidUTF8(res.Output, "�"),
		Usage:         &hubv1.Usage{InputTokens: res.InputTokens, OutputTokens: res.OutputTokens},
		Quota:         quota,
		ReservationId: res.ReservationID,
		Replayed:      res.Replayed,
	}, nil
}

// deltaSender sends the chunks of one stream from its own goroutine. push never blocks: chunks that arrive while a
// send is in flight are joined into the next delta. The first send error stops it (the client is gone).
type deltaSender struct {
	send func(string) error
	wake chan struct{}
	done chan struct{}

	mu      sync.Mutex
	pending strings.Builder
	closed  bool
	err     error
}

func newDeltaSender(send func(string) error) *deltaSender {
	d := &deltaSender{send: send, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go d.loop()
	return d
}

// push queues chunk; after finish or a send error it is dropped.
func (d *deltaSender) push(chunk string) {
	d.mu.Lock()
	if !d.closed && d.err == nil {
		d.pending.WriteString(chunk)
	}
	d.mu.Unlock()
	d.signal()
}

// finish stops the sender after it has sent what is queued, or dropped it when discard is set, and returns the send
// error. It waits for a send in flight, so nothing is sent after it returns.
func (d *deltaSender) finish(discard bool) error {
	d.mu.Lock()
	d.closed = true
	if discard {
		d.pending.Reset()
	}
	d.mu.Unlock()
	d.signal()
	<-d.done

	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *deltaSender) signal() {
	select {
	case d.wake <- struct{}{}:
	default: // a wake-up is already due
	}
}

func (d *deltaSender) loop() {
	defer close(d.done)
	for range d.wake {
		d.mu.Lock()
		chunk, closed := d.pending.String(), d.closed
		d.pending.Reset()
		d.mu.Unlock()

		if chunk != "" {
			if err := d.send(chunk); err != nil {
				d.mu.Lock()
				d.err = err
				d.mu.Unlock()
				return
			}
		}
		if closed {
			return
		}
	}
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
		msg := msgProviderFailed
		if errors.Is(err, gateway.ErrInvalidAnswer) {
			msg = msgInvalidAnswer
		}
		detail := &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: msg, Retryable: failed.Transient}
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
	case errors.Is(err, values.ErrClientInstructions):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgNoInstructions))
	case errors.Is(err, values.ErrAliasMismatch):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgServerAlias))
	case errors.Is(err, gateway.ErrNotStreamable):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgNotStreamable))
	case errors.Is(err, gateway.ErrOperationUnavailable):
		return connectrpc.NewError(connectrpc.CodeInvalidArgument, errors.New(msgUnavailableOp))
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

// operationOf maps the contract enum to the domain operation; UNSPECIFIED, AGENT_STEP (its own RPC) and unknown values
// map to "", which the domain rejects.
func operationOf(op hubv1.OperationType) string {
	switch op {
	case hubv1.OperationType_OPERATION_TYPE_SUMMARY:
		return string(values.OperationSummary)
	case hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES:
		return string(values.OperationExtractHomework)
	case hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT:
		return string(values.OperationHomeworkDraft)
	case hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT:
		return string(values.OperationLectureText)
	case hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL:
		return string(values.OperationInspectMaterial)
	case hubv1.OperationType_OPERATION_TYPE_ANSWER:
		return string(values.OperationAnswer)
	case hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT:
		return string(values.OperationTriageAssignment)
	default:
		return ""
	}
}
