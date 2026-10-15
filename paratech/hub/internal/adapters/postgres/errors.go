package postgres

import (
	"context"
	"errors"
	"strings"
)

// Typed errors of the adapter (ARCH-32), checked with errors.Is.
var (
	// ErrInvalidURL means the database URL could not be parsed. The error carries no detail: the parser's message may
	// quote the URL with its password.
	ErrInvalidURL = errors.New("invalid database URL")

	// ErrUnavailable means the database could not be reached or refused the connection (down, wrong host, wrong
	// credentials). Retrying later may help.
	ErrUnavailable = errors.New("database is unavailable")

	// ErrMigration means applying the embedded migrations failed.
	ErrMigration = errors.New("database migration failed")

	// ErrClosed means the DB was closed.
	ErrClosed = errors.New("database is closed")

	// ErrQuery means a query or statement failed.
	ErrQuery = errors.New("database query failed")

	// ErrCorrupt means a stored row does not form a valid domain object.
	ErrCorrupt = errors.New("stored row is invalid")
)

// Error is a translated database error. errors.Is matches Kind; the driver error is deliberately not in the chain so
// callers cannot depend on the driver.
type Error struct {
	Kind   error  // one of the sentinels above
	Detail string // driver message with the password removed; never the URL
}

// Error implements error.
func (e *Error) Error() string {
	if e.Detail == "" {
		return "postgres: " + e.Kind.Error()
	}
	return "postgres: " + e.Kind.Error() + ": " + e.Detail
}

// Unwrap lets errors.Is see the sentinel.
func (e *Error) Unwrap() error { return e.Kind }

// translate maps err to kind. Context errors pass through so that callers can tell a cancelled call from a failure.
// pgx messages contain user, database and host but not the password; secret is removed anyway as a second line of
// defence (for example a password that the server echoes in some message).
func translate(ctx context.Context, kind error, err error, secret string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return ctx.Err()
	}
	return &Error{Kind: kind, Detail: redact(err.Error(), secret)}
}

func redact(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "xxxxx")
}
