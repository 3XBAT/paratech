package connect

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

	// authTimeout bounds one key-store lookup, so a hanging store answers unavailable instead of hanging the RPC.
	authTimeout = 3 * time.Second

	// maxLoggedProcedure bounds the client-chosen path in a log line.
	maxLoggedProcedure = 128

	// One message for every rejected key, so the answer never tells whether a key exists or was revoked.
	msgRejected    = "invalid API key"
	msgUnavailable = "authentication is temporarily unavailable"
	msgCanceled    = "request canceled"
)

// Authenticator returns the id of a valid, active key; a rejection matches keyauth.ErrInvalidKey, other errors are store failures.
type Authenticator interface {
	Authenticate(ctx context.Context, presented string) (shared.APIKeyID, error)
}

type apiKeyContextKey struct{}

// APIKeyFrom returns the id of the API key that authenticated the request; ok is false outside an authenticated request.
func APIKeyFrom(ctx context.Context) (id shared.APIKeyID, ok bool) {
	id, ok = ctx.Value(apiKeyContextKey{}).(shared.APIKeyID)
	return id, ok
}

// requireAPIKey checks "Authorization: Bearer <key>" before the Connect handler reads the body (AC-25).
type requireAPIKey struct {
	auth Authenticator
	log  *slog.Logger
	errs *connectrpc.ErrorWriter
	next http.Handler
}

func (m requireAPIKey) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := m.authenticate(r.Context(), r.URL.Path, r.Header)
	if err != nil {
		// The writer formats the error for Connect, gRPC and gRPC-Web; any other request gets it as Connect JSON (401).
		if werr := m.errs.Write(w, r, err); werr != nil {
			m.log.DebugContext(r.Context(), "writing the auth error failed", "err", werr)
		}
		return
	}
	m.next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey{}, id)))
}

// authenticate never logs the presented key, any part of it, its hash or its id.
func (m requireAPIKey) authenticate(ctx context.Context, procedure string, h http.Header) (shared.APIKeyID, *connectrpc.Error) {
	if len(procedure) > maxLoggedProcedure {
		procedure = procedure[:maxLoggedProcedure]
	}
	presented, ok := bearerToken(h)
	if !ok {
		m.log.InfoContext(ctx, "API key rejected", "procedure", procedure, "reason", "missing or malformed Authorization header")
		return "", rejectedError()
	}

	lookup, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()
	id, err := m.auth.Authenticate(lookup, presented)
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, keyauth.ErrInvalidKey):
		m.log.InfoContext(ctx, "API key rejected", "procedure", procedure, "reason", rejectionReason(err))
		return "", rejectedError()
	case ctx.Err() != nil: // the client went away; a timeout of the lookup alone is unavailable below
		return "", connectrpc.NewError(connectrpc.CodeCanceled, errors.New(msgCanceled))
	default:
		m.log.WarnContext(ctx, "API key check failed", "procedure", procedure, "err", err)
		return "", connectrpc.NewError(connectrpc.CodeUnavailable, errors.New(msgUnavailable))
	}
}

// requireIdentity is the second check: an RPC runs only with the key id set by requireAPIKey, so it fails closed if the
// middleware is ever dropped.
type requireIdentity struct {
	log *slog.Logger
}

var _ connectrpc.Interceptor = requireIdentity{}

// WrapUnary implements connect.Interceptor.
func (i requireIdentity) WrapUnary(next connectrpc.UnaryFunc) connectrpc.UnaryFunc {
	return func(ctx context.Context, req connectrpc.AnyRequest) (connectrpc.AnyResponse, error) {
		if err := i.check(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor; the Hub is no client, so it changes nothing.
func (requireIdentity) WrapStreamingClient(next connectrpc.StreamingClientFunc) connectrpc.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements connect.Interceptor.
func (i requireIdentity) WrapStreamingHandler(next connectrpc.StreamingHandlerFunc) connectrpc.StreamingHandlerFunc {
	return func(ctx context.Context, conn connectrpc.StreamingHandlerConn) error {
		if err := i.check(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (i requireIdentity) check(ctx context.Context, procedure string) error {
	if id, ok := APIKeyFrom(ctx); ok && id != "" {
		return nil
	}
	i.log.ErrorContext(ctx, "RPC without an authenticated API key: the auth middleware is not mounted", "procedure", procedure)
	return rejectedError()
}

// bearerToken extracts the key of exactly one "Authorization: Bearer <key>" header; the scheme is case-insensitive (RFC 9110).
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

// rejectedError is unauthenticated with ERROR_CODE_INVALID_API_KEY, not retryable; new per call, as its metadata is mutable.
func rejectedError() *connectrpc.Error {
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
