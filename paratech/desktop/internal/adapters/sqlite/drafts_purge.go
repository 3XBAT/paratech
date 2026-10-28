package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

const opPurgeDraft = "sqlite: purge draft of an assignment"

// DeleteOfAssignment removes the draft of assignment a with all its versions (CASCADE), for the purge of a deleted
// assignment (ADR-0029 addendum 2); none is not an error.
func (r *DraftRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID) error {
	if err := r.db.Queries(ctx).DeleteDraftOfAssignment(ctx, string(a)); err != nil {
		return fmt.Errorf("%s %s: %w", opPurgeDraft, a, MapError(ctx, err))
	}
	return nil
}
