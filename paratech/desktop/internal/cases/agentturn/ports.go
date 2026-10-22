package agentturn

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// AgentModel runs one planner step on the Hub (GatewayService.AgentStep, ADR-0023). Implemented by the hubclient
// adapter. Errors are those of Complete: shared.ErrNetworkUnavailable, shared.ErrServiceNotConfigured,
// shared.ErrServiceUnavailable, shared.ErrQuotaExceeded. A repeated key returns the stored reply (Replayed), so
// «Продолжить» after a failure does not pay twice. The reply is untrusted: every call goes through values.ParseCall
// and the thread's Admit before anything runs (hard rule 3).
type AgentModel interface {
	Step(ctx context.Context, req values.StepRequest) (values.StepReply, error)
}

// ThreadRepository stores agent threads (one per subject key) in the local database. Implemented by the sqlite
// adapter.
type ThreadRepository interface {
	// Get loads a thread. It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.ThreadID) (*thread.Thread, error)

	// GetByKey loads the thread of a subject key. It returns an error wrapping shared.ErrNotFound if there is none.
	GetByKey(ctx context.Context, key thread.Key) (*thread.Thread, error)

	// Save inserts a new thread (version 0) or updates a stored one with a compare-and-swap on its version; a stale
	// version is shared.ErrVersionConflict, a second thread for the same key is shared.ErrDuplicate.
	Save(ctx context.Context, t *thread.Thread) error
}

// History reads and appends the history entries of a thread (student texts, sealed calls, results), oldest first,
// with their sizes in bytes for thread.WindowStart.
type History interface {
	Append(ctx context.Context, id shared.ThreadID, turn int, entry values.HistoryTurn) error
	Recent(ctx context.Context, id shared.ThreadID, limit int) ([]values.HistoryTurn, []int, error)
}

// Clock returns the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}

// IDGenerator creates thread IDs.
type IDGenerator interface {
	NewThreadID() shared.ThreadID
}
