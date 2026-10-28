package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

const opPurgeWorks = "sqlite: purge works of an assignment"

// DeleteOfAssignment removes the works of assignment a with their rounds (CASCADE), in any status, for the purge of a
// deleted assignment (ADR-0029 addendum 2), and returns how many; none is not an error. The finished file of a work is
// a library material and stays in the library.
func (r *WorkRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID) (int, error) {
	n, err := r.db.Queries(ctx).DeleteWorksOfAssignment(ctx, jobNullString(string(a)))
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeWorks, a, MapError(ctx, err))
	}
	return int(n), nil
}
