package agentretention

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Requests reads and purges the details of earlier requests. Implemented by the sqlite adapter.
type Requests interface {
	// EndedBefore returns at most limit requests that are final, not the current one of their chat, ended before
	// cutoff and still have calls or results, in (EndedAt, Thread, Number) order after the key of after (zero: from
	// the start).
	EndedBefore(ctx context.Context, cutoff time.Time, after thread.EndedRequest, limit int) ([]thread.EndedRequest, error)

	// Entries returns the stored entries of request number of chat id, oldest first; one that cannot be read is an
	// error.
	Entries(ctx context.Context, id shared.ThreadID, number int) ([]values.HistoryTurn, error)

	// PurgeDetails freezes line as the reply line of the request while it has none ("" freezes nothing), then
	// deletes its calls and results; both only while the request still is final, not current and ended before cutoff,
	// and the delete only once the request has a reply line. It reports whether anything was deleted.
	PurgeDetails(ctx context.Context, id shared.ThreadID, number int, line string, cutoff time.Time) (bool, error)
}

// Tx runs fn in one write transaction (the sqlite adapter: BEGIN IMMEDIATE); the Requests called with the context of
// fn take part in it.
type Tx interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock supplies the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}
