package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

var dlvT0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func newDeliveryEnv(t *testing.T) (*DB, *DeliveryRepository) {
	t.Helper()
	db := openTest(t, nil)
	mustExec(t, db, `INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a1', 'hw', 0, 0)`)
	return db, NewDeliveryRepository(db)
}

func newTestDelivery(t *testing.T, id, key string) *delivery.Delivery {
	t.Helper()
	return newTestDeliveryFor(t, id, key, "a1")
}

func newTestDeliveryFor(t *testing.T, id, key string, a shared.AssignmentID) *delivery.Delivery {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactExports, string(a)+"/homework.md")
	if err != nil {
		t.Fatal(err)
	}
	att, err := delivery.NewAttachment(ref, strings.Repeat("0f", 32), 11)
	if err != nil {
		t.Fatal(err)
	}
	d, err := delivery.New(delivery.Spec{
		ID: shared.DeliveryID(id), AssignmentID: a, Recipient: "teacher@example.org", Subject: "Homework",
		Attachment: att, MessageID: "<" + id + "." + strings.ReplaceAll(key, ":", ".") + "@paratech.local>", Key: key, ConfirmedAt: dlvT0.Add(-time.Minute),
	}, dlvT0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDeliverySaveGetUpdate(t *testing.T) {
	_, repo := newDeliveryEnv(t)
	ctx := context.Background()
	d := newTestDelivery(t, "d1", "proposal:p1")
	if err := repo.Save(ctx, d); err != nil || d.Version() != 1 {
		t.Fatalf("insert: %v, version %d", err, d.Version())
	}

	// The same instance is saved again after each transition (ADR-0004(d)).
	if err := d.StartSending(dlvT0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, d); err != nil || d.Version() != 2 {
		t.Fatalf("update: %v, version %d", err, d.Version())
	}
	if _, err := d.RecordResult(nil, dlvT0.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, d); err != nil {
		t.Fatal(err)
	}

	for name, get := range map[string]func() (*delivery.Delivery, error){
		"by id":  func() (*delivery.Delivery, error) { return repo.Get(ctx, "d1") },
		"by key": func() (*delivery.Delivery, error) { return repo.GetByIdempotencyKey(ctx, "proposal:p1") },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.State() != d.State() {
			t.Fatalf("%s\n got %+v\nwant %+v", name, got.State(), d.State())
		}
	}
}

func TestDeliverySaveConflicts(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(t *testing.T, repo *DeliveryRepository) error
		want error
	}{
		{"same idempotency key is a duplicate", func(t *testing.T, repo *DeliveryRepository) error {
			return repo.Save(ctx, newTestDelivery(t, "d2", "proposal:p1"))
		}, shared.ErrDuplicate},
		{"same id is a version conflict", func(t *testing.T, repo *DeliveryRepository) error {
			return repo.Save(ctx, newTestDelivery(t, "d1", "proposal:p2"))
		}, shared.ErrVersionConflict},
		{"stale version", func(t *testing.T, repo *DeliveryRepository) error {
			a, _ := repo.Get(ctx, "d1")
			b, _ := repo.Get(ctx, "d1")
			if err := a.StartSending(dlvT0.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, a); err != nil {
				t.Fatal(err)
			}
			if err := b.StartSending(dlvT0.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			return repo.Save(ctx, b)
		}, shared.ErrVersionConflict},
		{"unknown assignment", func(t *testing.T, repo *DeliveryRepository) error {
			return repo.Save(ctx, newTestDeliveryFor(t, "d3", "proposal:p3", "nope"))
		}, shared.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, repo := newDeliveryEnv(t)
			if err := repo.Save(ctx, newTestDelivery(t, "d1", "proposal:p1")); err != nil {
				t.Fatal(err)
			}
			if err := tt.run(t, repo); !errors.Is(err, tt.want) {
				t.Fatalf("Save = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDeliveryGetMissing(t *testing.T) {
	_, repo := newDeliveryEnv(t)
	ctx := context.Background()
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := repo.GetByIdempotencyKey(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("GetByIdempotencyKey = %v", err)
	}
}

func TestDeliveryListByStatus(t *testing.T) {
	_, repo := newDeliveryEnv(t)
	ctx := context.Background()
	pending := newTestDelivery(t, "d1", "k1")
	sending := newTestDelivery(t, "d2", "k2")
	if err := sending.StartSending(dlvT0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*delivery.Delivery{pending, sending} {
		if err := repo.Save(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		statuses []delivery.Status
		want     []shared.DeliveryID
	}{
		{nil, nil},
		{[]delivery.Status{delivery.StatusUnknown}, nil},
		{[]delivery.Status{delivery.StatusSending}, []shared.DeliveryID{"d2"}},
		{[]delivery.Status{delivery.StatusPending, delivery.StatusSending}, []shared.DeliveryID{"d1", "d2"}},
	}
	for _, tt := range tests {
		got, err := repo.ListByStatus(ctx, tt.statuses...)
		if err != nil {
			t.Fatal(err)
		}
		var ids []shared.DeliveryID
		for _, d := range got {
			ids = append(ids, d.ID())
		}
		if len(ids) != len(tt.want) || (len(ids) > 0 && strings.Join(idStrings(ids), ",") != strings.Join(idStrings(tt.want), ",")) {
			t.Errorf("ListByStatus(%v) = %v, want %v", tt.statuses, ids, tt.want)
		}
	}
}

func idStrings(ids []shared.DeliveryID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

// A row that breaks the aggregate invariants (here: a delivery without the confirmed digest, which the migration
// default would produce) is refused on load instead of being sent.
func TestDeliveryTamperedRowIsRefused(t *testing.T) {
	db, repo := newDeliveryEnv(t)
	ctx := context.Background()
	if err := repo.Save(ctx, newTestDelivery(t, "d1", "k1")); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE deliveries SET attachment_sha256 = '' WHERE id = 'd1'`)
	if _, err := repo.Get(ctx, "d1"); !errors.Is(err, delivery.ErrInvalidDelivery) {
		t.Fatalf("Get of a row without digest = %v, want ErrInvalidDelivery", err)
	}

	_, err := db.sql.ExecContext(ctx, `UPDATE deliveries SET attachment_sha256 = 'abc' WHERE id = 'd1'`)
	if !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("digest of wrong length = %v, want ErrConstraint", err)
	}
}
