package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

const opPurgeDraft = "sqlite: purge draft of an assignment"

// DeleteOfAssignment removes the draft of assignment a with all its versions (CASCADE), for the purge of a deleted
// assignment (ADR-0029 addendum 2), only while a is still deleted before cutoff; it returns how many drafts went (0 or
// 1).
func (r *DraftRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	n, err := r.db.Queries(ctx).PurgeDraftOfAssignment(ctx, sqlcgen.PurgeDraftOfAssignmentParams{
		AssignmentID: string(a), Cutoff: cutoffMs(cutoff),
	})
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeDraft, a, MapError(ctx, err))
	}
	return int(n), nil
}
