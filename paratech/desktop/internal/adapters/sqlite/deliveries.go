package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the delivery repository, used in its error messages.
const (
	opGetDelivery      = "sqlite: get delivery"
	opGetDeliveryByKey = "sqlite: get delivery by idempotency key"
	opListDeliveries   = "sqlite: list deliveries"
	opSaveDelivery     = "sqlite: save delivery"
)

// DeliveryRepository stores deliveries (the senddelivery.DeliveryRepository port). Validation and the status machine
// stay in delivery.Delivery: the repository only converts rows. No credential is ever part of a row (hard rule 4).
type DeliveryRepository struct{ db *DB }

// NewDeliveryRepository returns the delivery repository of db.
func NewDeliveryRepository(db *DB) *DeliveryRepository { return &DeliveryRepository{db: db} }

// Get loads a delivery; a missing one wraps shared.ErrNotFound.
func (r *DeliveryRepository) Get(ctx context.Context, id shared.DeliveryID) (*delivery.Delivery, error) {
	row, err := r.db.Queries(ctx).GetDelivery(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetDelivery, id, MapError(ctx, err))
	}
	return deliveryFromRow(row)
}

// GetByIdempotencyKey loads the delivery created under key; a missing one wraps shared.ErrNotFound. The key is not
// part of the error text.
func (r *DeliveryRepository) GetByIdempotencyKey(ctx context.Context, key string) (*delivery.Delivery, error) {
	row, err := r.db.Queries(ctx).GetDeliveryByIdempotencyKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opGetDeliveryByKey, MapError(ctx, err))
	}
	return deliveryFromRow(row)
}

// ListByStatus returns the deliveries in any of the given statuses, oldest first; no status or no match is an empty
// list.
func (r *DeliveryRepository) ListByStatus(ctx context.Context, statuses ...delivery.Status) ([]*delivery.Delivery, error) {
	if len(statuses) == 0 {
		return []*delivery.Delivery{}, nil
	}
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}
	rows, err := r.db.Queries(ctx).ListDeliveriesByStatus(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListDeliveries, MapError(ctx, err))
	}
	out := make([]*delivery.Delivery, 0, len(rows))
	for _, row := range rows {
		d, err := deliveryFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Save writes the delivery: an INSERT for version 0, otherwise a version compare-and-swap (ADR-0004(d)). An id that
// exists already, or a stale version, yields shared.ErrVersionConflict; a second delivery with an existing idempotency
// key (or Message-ID) yields shared.ErrDuplicate; an unknown assignment yields an error wrapping shared.ErrConflict
// (foreign key).
func (r *DeliveryRepository) Save(ctx context.Context, d *delivery.Delivery) error {
	if d.Version() < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveDelivery, d.Version())
	}

	st := d.State()
	next := st.Version + 1
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			return DuplicateAsVersionConflict(MapError(ctx, q.InsertDelivery(ctx, sqlcgen.InsertDeliveryParams{
				ID: string(st.ID), AssignmentID: string(st.AssignmentID), Recipient: st.Recipient, Subject: st.Subject,
				ArtifactRef: st.Attachment.String(), AttachmentSha256: st.SHA256, AttachmentSize: st.Size,
				MessageID: st.MessageID, IdempotencyKey: st.Key, Status: string(st.Status), Attempts: int64(st.Attempts),
				LastError: jobNullString(st.LastError), ConfirmedAt: jobMs(st.ConfirmedAt), SentAt: jobNullMs(st.SentAt),
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			})))
		}
		n, err := q.UpdateDelivery(ctx, sqlcgen.UpdateDeliveryParams{
			Status: string(st.Status), Attempts: int64(st.Attempts), LastError: jobNullString(st.LastError),
			SentAt: jobNullMs(st.SentAt), UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
		})
		return VersionCAS(ctx, n, err)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveDelivery, st.ID, err)
	}

	d.Persisted(next)
	return nil
}

func deliveryFromRow(row sqlcgen.Delivery) (*delivery.Delivery, error) {
	ref, err := shared.ParseArtifactRef(row.ArtifactRef)
	if err != nil {
		return nil, fmt.Errorf("stored delivery %s: %w", row.ID, err)
	}
	d, err := delivery.Restore(delivery.State{
		Version: row.Version, ID: shared.DeliveryID(row.ID), Status: delivery.Status(row.Status),
		AssignmentID: shared.AssignmentID(row.AssignmentID), Recipient: row.Recipient, Subject: row.Subject,
		Attachment: ref, SHA256: row.AttachmentSha256, Size: row.AttachmentSize, MessageID: row.MessageID,
		Key: row.IdempotencyKey, Attempts: int(row.Attempts), LastError: nullText(row.LastError),
		ConfirmedAt: jobFromMs(row.ConfirmedAt), SentAt: jobFromNullMs(row.SentAt),
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("stored delivery %s: %w", row.ID, err)
	}
	return d, nil
}

func nullText(s sql.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.String
}
