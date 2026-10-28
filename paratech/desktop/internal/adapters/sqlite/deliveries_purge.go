package sqlite

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

const opPurgeDeliveries = "sqlite: purge deliveries of an assignment"

// finalDeliveryStatuses are the statuses no transition leaves (SENT, FAILED): only such a delivery may be purged.
var finalDeliveryStatuses = func() []string {
	var out []string
	for _, s := range delivery.Statuses() {
		if s.IsTerminal() {
			out = append(out, string(s))
		}
	}
	return out
}()

// DeleteFinalOfAssignment removes the deliveries of assignment a that are SENT or FAILED (the purge of a deleted
// assignment, ADR-0029 addendum 2) and returns how many; a PENDING, SENDING or UNKNOWN one is never touched (hard
// rule 7). None is not an error.
func (r *DeliveryRepository) DeleteFinalOfAssignment(ctx context.Context, a shared.AssignmentID) (int, error) {
	n, err := r.db.Queries(ctx).DeleteFinalDeliveriesOfAssignment(ctx, sqlcgen.DeleteFinalDeliveriesOfAssignmentParams{
		AssignmentID: string(a), Statuses: finalDeliveryStatuses,
	})
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", opPurgeDeliveries, a, MapError(ctx, err))
	}
	return int(n), nil
}
