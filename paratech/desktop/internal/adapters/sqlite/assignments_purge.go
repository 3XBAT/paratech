package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the purge of deleted assignments (ADR-0029 addendum 2).
const (
	opListDeletedAssignments = "sqlite: list deleted assignments"
	opPurgeAssignment        = "sqlite: purge deleted assignment"
)

// ListDeletedBefore returns at most limit assignments deleted before cutoff, oldest delete first (the purge after the
// restore window); none is an empty list. limit must be at least 1.
func (r *AssignmentRepository) ListDeletedBefore(ctx context.Context, cutoff time.Time, limit int) ([]*assignment.Assignment, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s: limit %d, want at least 1", opListDeletedAssignments, limit)
	}
	rows, err := r.db.Queries(ctx).ListAssignmentsDeletedBefore(ctx, sqlcgen.ListAssignmentsDeletedBeforeParams{
		Cutoff: cutoffMs(cutoff), MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListDeletedAssignments, MapError(ctx, err))
	}
	out := make([]*assignment.Assignment, 0, len(rows))
	for _, row := range rows {
		a, err := assignmentFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// PurgeDeleted removes the row of assignment id if it is still deleted before cutoff; false when no such row is left
// (gone already, restored, or deleted later). A draft or a delivery still referencing it (ON DELETE RESTRICT) keeps
// the row and yields assignment.ErrInUse; its works cascade.
func (r *AssignmentRepository) PurgeDeleted(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	n, err := r.db.Queries(ctx).PurgeDeletedAssignment(ctx, sqlcgen.PurgeDeletedAssignmentParams{
		ID: string(id), Cutoff: cutoffMs(cutoff),
	})
	if err != nil {
		err = MapError(ctx, err)
		if errors.Is(err, shared.ErrConflict) {
			// A delete can only break the foreign key of a referencing row.
			return false, fmt.Errorf("%s %s: %w", opPurgeAssignment, id, assignment.ErrInUse)
		}
		return false, fmt.Errorf("%s %s: %w", opPurgeAssignment, id, err)
	}
	return n > 0, nil
}

// cutoffMs is the cutoff in the milliseconds of deleted_at, rounded up: a stored time (whole milliseconds) is before
// the result exactly when it is before cutoff, so the query agrees with assignment.PurgeDue. A zero cutoff is NULL,
// which matches nothing.
func cutoffMs(cutoff time.Time) sql.NullInt64 {
	v := jobNullMs(cutoff)
	if v.Valid && cutoff.After(time.UnixMilli(v.Int64)) {
		v.Int64++
	}
	return v
}
