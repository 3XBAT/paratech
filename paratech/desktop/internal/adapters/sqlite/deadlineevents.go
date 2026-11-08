package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the deadline event updates, used in error messages.
const (
	opHasDeadlineEvent = "sqlite: whether an assignment has a deadline event"
	opListUpdates      = "sqlite: list deadline events to update"
	opPurgeUpdate      = "sqlite: purge an updated deadline event"
	opQueueUpdate      = "sqlite: queue a deadline event to update"
)

// DeadlineEventUpdates are the Google deadline events to move to a changed deadline (TASK-276, ADR-0029 addendum 2),
// a queue as DeadlineEventRemovals, with the fact whether an assignment has an event at all.
type DeadlineEventUpdates struct{ db *DB }

// NewDeadlineEventUpdates returns the deadline event updates of db.
func NewDeadlineEventUpdates(db *DB) *DeadlineEventUpdates { return &DeadlineEventUpdates{db: db} }

// HasDeadlineEvent reports whether assignment id has a Google event: its create_deadline_event proposal was executed
// and no removal of the event is waiting. An unknown assignment has none.
func (u *DeadlineEventUpdates) HasDeadlineEvent(ctx context.Context, id shared.AssignmentID) (bool, error) {
	n, err := u.db.Queries(ctx).AssignmentHasDeadlineEvent(ctx, string(id))
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opHasDeadlineEvent, id, MapError(ctx, err))
	}
	return n != 0, nil
}

// QueueDeadlineUpdate queues the update of the deadline event of assignment id at at, in the caller's transaction or
// its own; one already queued keeps one row and takes the later time.
func (u *DeadlineEventUpdates) QueueDeadlineUpdate(ctx context.Context, id shared.AssignmentID, at time.Time) error {
	err := u.db.Do(ctx, func(ctx context.Context) error {
		return MapError(ctx, u.db.Queries(ctx).UpsertDeadlineEventUpdate(ctx, sqlcgen.UpsertDeadlineEventUpdateParams{
			AssignmentID: string(id), CreatedAt: jobMs(at),
		}))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opQueueUpdate, id, err)
	}
	return nil
}

// ListDeadlineUpdates returns up to limit assignments whose deadline event is to be updated, oldest first; a limit
// below 1 returns none.
func (u *DeadlineEventUpdates) ListDeadlineUpdates(ctx context.Context, limit int) ([]shared.AssignmentID, error) {
	if limit < 1 {
		return []shared.AssignmentID{}, nil // SQLite reads a negative LIMIT as no limit
	}
	ids, err := u.db.Queries(ctx).ListDeadlineEventUpdates(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListUpdates, MapError(ctx, err))
	}
	out := make([]shared.AssignmentID, len(ids))
	for i, id := range ids {
		out[i] = shared.AssignmentID(id)
	}
	return out, nil
}

// PurgeDeadlineUpdate forgets the update once the event follows the deadline (or is not to); a missing row is not an
// error.
func (u *DeadlineEventUpdates) PurgeDeadlineUpdate(ctx context.Context, id shared.AssignmentID) error {
	err := u.db.Do(ctx, func(ctx context.Context) error {
		return MapError(ctx, u.db.Queries(ctx).DeleteDeadlineEventUpdate(ctx, string(id)))
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opPurgeUpdate, id, err)
	}
	return nil
}
