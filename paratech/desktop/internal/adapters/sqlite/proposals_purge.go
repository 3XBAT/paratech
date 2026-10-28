package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

const opPurgeProposals = "sqlite: purge proposals of an assignment"

// DeleteOfAssignment removes every proposal naming assignment a, whatever its status (the purge of a deleted
// assignment, ADR-0029 addendum 2), only while a is still deleted before cutoff, and returns how many; none is not an
// error.
func (r *ProposalRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	n, err := r.db.Queries(ctx).DeleteProposalsOfAssignment(ctx, sqlcgen.DeleteProposalsOfAssignmentParams{
		AssignmentID: string(a), Cutoff: cutoffMs(cutoff),
	})
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeProposals, a, MapError(ctx, err))
	}
	return int(n), nil
}
