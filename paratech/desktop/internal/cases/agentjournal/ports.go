package agentjournal

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/shared"
)

// ActionRepository stores journal entries in the local database. Implemented by the sqlite adapter.
type ActionRepository interface {
	// Get loads an entry. It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.ActionID) (*action.Action, error)

	// Save inserts a new entry (version 0) or updates a stored one with a compare-and-swap on its version; a stale
	// version is shared.ErrVersionConflict.
	Save(ctx context.Context, a *action.Action) error

	// ListByThread returns the entries of a thread, newest first, at most limit.
	ListByThread(ctx context.Context, thread shared.ThreadID, limit int) ([]*action.Action, error)

	// ListUnfinished returns PENDING and UNDOING entries for startup recovery.
	ListUnfinished(ctx context.Context) ([]*action.Action, error)
}

// Compensator applies a compensation idempotently. TargetVersion returns the current version of the target, so the
// journal can refuse Undo when it changed since the action; a target that no longer exists is shared.ErrNotFound.
type Compensator interface {
	TargetVersion(ctx context.Context, c action.Compensation) (int64, error)
	Apply(ctx context.Context, c action.Compensation) error
}

// Clock returns the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}

// IDGenerator creates action IDs.
type IDGenerator interface {
	NewActionID() shared.ActionID
}
