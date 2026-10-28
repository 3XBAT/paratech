package sqlite

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/shared"
)

const opPurgeWorks = "sqlite: purge works of an assignment"

// DeleteOfAssignment removes the works of assignment a with their rounds (CASCADE), in any status, for the purge of a
// deleted assignment (ADR-0029 addendum 2), only while a is still deleted before cutoff, and returns how many; none is
// not an error. The finished file of a work is a library material and stays in the library.
func (r *WorkRepository) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	n, err := r.db.Queries(ctx).DeleteWorksOfAssignment(ctx, sqlcgen.DeleteWorksOfAssignmentParams{
		AssignmentID: jobNullString(string(a)), Cutoff: cutoffMs(cutoff),
	})
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeWorks, a, MapError(ctx, err))
	}
	return int(n), nil
}
