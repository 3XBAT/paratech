package connect

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const (
	headerAuthorization = "Authorization"
	authScheme          = "Bearer"

	// maxAuthorizationLen bounds the header before it is parsed; an issued key is much shorter.
	maxAuthorizationLen = 256

	// Client-facing messages. msgRejected is the same for every rejected key, so the answer never tells whether a
	// key exists or was revoked.
	msgRejected    = "invalid API key"
	msgUnavailable = "authentication is temporarily unavailable"
)

// Authenticator checks a presented API key and returns the id of a valid, active key. A rejection matches
// keyauth.ErrInvalidKey; any other error is a failure of the key store.
type Authenticator interface {
	Authenticate(ctx context.Context, presented string) (shared.APIKeyID, error)
}

type apiKeyContextKey struct{}

// APIKeyFrom returns the id of the API key that authenticated the request. Handlers behind the router always find
// it; ok is false only outside an authenticated request.
func APIKeyFrom(ctx context.Context) (id shared.APIKeyID, ok bool) {
	id, ok = ctx.Value(apiKeyContextKey{}).(shared.APIKeyID)
	return id, ok
}

// authInterceptor authenticates every unary and server-streaming RPC by the "Authorization: Bearer <key>" header
// (shared/proto common.proto) before the handler runs. A unary request body is already read (bounded by
// maxRequestBytes) when it runs; a stream's request is read after it.
type authInterceptor struct {
	auth Authenticator
	log  *slog.Logger
}

var _ connectrpc.Interceptor = authInterceptor{}

// WrapUnary implements connect.Interceptor.
func (i authInterceptor) WrapUnary(next connectrpc.UnaryFunc) connectrpc.UnaryFunc {
	return func(ctx context.Context, req connectrpc.AnyRequest) (connectrpc.AnyResponse, error) {
		ctx, err := i.authenticate(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor; the Hub is no client, so it changes nothing.
func (authInterceptor) WrapStreamingClient(next connectrpc.StreamingClientFunc) connectrpc.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements connect.Interceptor.
func (i authInterceptor) WrapStreamingHandler(next connectrpc.StreamingHandlerFunc) connectrpc.StreamingHandlerFunc {
	return func(ctx context.Context, conn connectrpc.StreamingHandlerConn) error {
		ctx, err := i.authenticate(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// authenticate never logs the presented key, any part of it or its hash; a rejection is logged with its reason only.
func (i authInterceptor) authenticate(ctx context.Context, procedure string, h http.Header) (context.Context, error) {
	presented, ok := bearerToken(h)
	if !ok {
		i.log.DebugContext(ctx, "API key rejected", "procedure", procedure, "reason", "missing or malformed Authorization header")
		return ctx, rejectedError()
	}

	id, err := i.auth.Authenticate(ctx, presented)
	switch {
	case err == nil:
		return context.WithValue(ctx, apiKeyContextKey{}, id), nil
	case errors.Is(err, keyauth.ErrInvalidKey):
		i.log.DebugContext(ctx, "API key rejected", "procedure", procedure, "reason", rejectionReason(err))
		return ctx, rejectedError()
	case ctx.Err() != nil:
		return ctx, ctx.Err()
	default:
		i.log.WarnContext(ctx, "API key check failed", "procedure", procedure, "err", err)
		return ctx, connectrpc.NewError(connectrpc.CodeUnavailable, errors.New(msgUnavailable))
	}
}

// bearerToken extracts the key of exactly one "Authorization: Bearer <key>" header. The scheme is case-insensitive
// (RFC 9110); the key itself is checked by the use case.
func bearerToken(h http.Header) (string, bool) {
	values := h.Values(headerAuthorization)
	if len(values) != 1 || len(values[0]) > maxAuthorizationLen {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, authScheme) || token == "" {
		return "", false
	}
	return token, true
}

// rejectedError is the one answer to every rejected key: unauthenticated with ERROR_CODE_INVALID_API_KEY, not
// retryable (errors.proto). A new value per call, because a connect.Error carries mutable metadata.
func rejectedError() error {
	e := connectrpc.NewError(connectrpc.CodeUnauthenticated, errors.New(msgRejected))
	detail, err := connectrpc.NewErrorDetail(&hubv1.ErrorDetail{
		Code:      hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY,
		Message:   msgRejected,
		Retryable: false,
	})
	if err == nil { // marshalling a fixed message cannot fail; without the detail the client still sees the code
		e.AddDetail(detail)
	}
	return e
}

func rejectionReason(err error) string {
	switch {
	case errors.Is(err, apikey.ErrMalformed):
		return "malformed"
	case errors.Is(err, apikey.ErrUnknown):
		return "unknown"
	case errors.Is(err, apikey.ErrRevoked):
		return "revoked"
	case errors.Is(err, apikey.ErrSecretMismatch):
		return "wrong secret"
	default:
		return "rejected"
	}
}
