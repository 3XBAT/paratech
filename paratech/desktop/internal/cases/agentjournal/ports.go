package agentjournal

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// ActionRepository stores journal entries in the local database. Implemented by the sqlite adapter.
type ActionRepository interface {
	// Get loads an entry. It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.ActionID) (*action.Action, error)

	// GetByCall loads the entry of a call of a request; none wraps shared.ErrNotFound.
	GetByCall(ctx context.Context, th shared.ThreadID, turn int, callID string) (*action.Action, error)

	// Save inserts a new entry (version 0) or updates a stored one with a compare-and-swap on its version; a stale
	// version is shared.ErrVersionConflict, a second entry for the same call of a request too.
	Save(ctx context.Context, a *action.Action) error

	// ListByThread returns the entries of a thread, newest first, at most limit.
	ListByThread(ctx context.Context, thread shared.ThreadID, limit int) ([]*action.Action, error)

	// ListRecent returns the newest entries of every thread, at most limit.
	ListRecent(ctx context.Context, limit int) ([]*action.Action, error)

	// ListUnfinished returns PENDING and UNDOING entries for startup recovery.
	ListUnfinished(ctx context.Context) ([]*action.Action, error)
}

// Compensator reads and applies compensations; implemented by input/agent over the use cases that own the targets.
// Check returns the state of the target (a target that no longer exists is shared.ErrNotFound): its version, whether
// it is already in the state Undo leads to, and whether a paid result is final. Apply must compare-and-swap on
// expectedVersion in the same transaction as the change: a target that changed after Check is an error wrapping
// action.ErrTargetChanged, never an overwrite of the newer change; a job that finished meanwhile action.ErrResultReady,
// one that cannot be stopped action.ErrCannotStop. Both are idempotent.
type Compensator interface {
	Check(ctx context.Context, c action.Compensation) (action.Target, error)
	Apply(ctx context.Context, c action.Compensation, expectedVersion int64) error
}

// Resolver tells Recover what happened to the target of a PENDING entry after a crash between its two transactions:
// the current state compared with the intent recorded before the use case ran. Implemented by input/agent.
type Resolver interface {
	Observe(ctx context.Context, tool values.Tool, in action.Intent, since time.Time) (action.Observation, error)
}

// Threads reads a thread for the subject of its journal lines. Implemented by the sqlite adapter.
type Threads interface {
	Get(ctx context.Context, id shared.ThreadID) (*thread.Thread, error)
}

// Processing lists the jobs of the processing history for the journal (every type, the newest terminal ones).
// Implemented by the sqlite adapter.
type Processing interface {
	ListActivity(ctx context.Context, maxTerminal int, hidden []job.Type) (jobs []*job.Job, unreadable []shared.JobID, err error)
}

// Clock returns the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}

// IDGenerator creates action IDs.
type IDGenerator interface {
	NewActionID() shared.ActionID
}
