package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

const opPurgeProposals = "sqlite: purge proposals of an assignment"

// DeleteOfAssignment removes every proposal naming assignment a, whatever its status (the purge of a deleted
// assignment, ADR-0029 addendum 2), and returns how many; none is not an error.
func (r *ProposalRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID) (int, error) {
	n, err := r.db.Queries(ctx).DeleteProposalsOfAssignment(ctx, string(a))
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeProposals, a, MapError(ctx, err))
	}
	return int(n), nil
}
