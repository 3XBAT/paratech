package senddelivery

// The delivery list and the subject of the homework screen (TASK-045).

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// history is the DeliveryHistory over the store: newest first, at most limit; limits records each call.
type history struct{ s *store }

func (h history) ListByAssignment(_ context.Context, a shared.AssignmentID, limit int) ([]*delivery.Delivery, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	var states []delivery.State
	for _, st := range h.s.deliveries {
		if st.AssignmentID == a {
			states = append(states, st)
		}
	}
	slices.SortFunc(states, func(x, y delivery.State) int {
		return cmp.Or(y.CreatedAt.Compare(x.CreatedAt), cmp.Compare(y.ID, x.ID))
	})
	out := []*delivery.Delivery{}
	for _, st := range states[:min(limit, len(states))] {
		d, err := delivery.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// failingHistory records the limit and fails.
type failingHistory struct{ limits *[]int }

func (h failingHistory) ListByAssignment(_ context.Context, _ shared.AssignmentID, limit int) ([]*delivery.Delivery, error) {
	*h.limits = append(*h.limits, limit)
	return nil, errDisk
}

var _ DeliveryHistory = history{}

func TestListForAssignment(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	first, snap := e.created(t)
	if err := e.svc.Handle(ctx, snap, noReport); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	e.writeHomework(t, "a1", "second version")
	if err := e.svc.SendHomework(ctx, command(t, "p2", "a1", "second version")); err != nil {
		t.Fatal(err)
	}
	e.writeHomework(t, "a2", homework)
	if err := e.svc.SendHomework(ctx, command(t, "p3", "a2", homework)); err != nil {
		t.Fatal(err)
	}

	got, err := e.svc.ListForAssignment(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ID != first || got[0].Status != delivery.StatusPending || got[1].Status != delivery.StatusSent {
		t.Fatalf("ListForAssignment(a1) = %+v, want the new PENDING one, then the SENT one", got)
	}
	if got[0].AssignmentID != "a1" || got[0].Recipient != "teacher@school.example" || got[0].Subject != DefaultSubject {
		t.Errorf("view = %+v", got[0])
	}
	if none, err := e.svc.ListForAssignment(ctx, "a9"); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("no deliveries = %v, %v, want an empty list", none, err)
	}
}

func TestListForAssignmentIsBoundedAndWrapsErrors(t *testing.T) {
	e := newEnv(t)
	var limits []int
	svc, err := New(Deps{
		Deliveries: e.st, History: failingHistory{&limits}, Artifacts: e.files, Email: e.email, Jobs: e.st,
		Credentials: e.creds, Clock: e.clock, IDs: e.st, Tx: e.st,
	}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListForAssignment(context.Background(), "a1"); !errors.Is(err, errDisk) {
		t.Fatalf("err = %v, want the cause wrapped", err)
	}
	if !slices.Equal(limits, []int{MaxListedDeliveries}) {
		t.Fatalf("limits = %v, want [%d]", limits, MaxListedDeliveries)
	}
}

// Only a delivery whose outcome is unknown offers "mark as sent" and "send again" (AC-15); the aggregate decides.
func TestViewOffersTheResolutionOfAnUnknownOutcome(t *testing.T) {
	tests := []struct {
		name   string
		result error // the outcome of the first attempt; nil: no attempt
		run    bool
		want   delivery.Status
	}{
		{name: "pending", want: delivery.StatusPending},
		{name: "sent", run: true, want: delivery.StatusSent},
		{name: "failed", run: true, result: delivery.ErrRejected, want: delivery.StatusFailed},
		{name: "unknown", run: true, result: delivery.ErrOutcomeUnknown, want: delivery.StatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			id, snap := e.created(t)
			if tt.run {
				e.email.result = func(context.Context, *delivery.Delivery) error { return tt.result }
				_ = e.svc.Handle(ctx, snap, noReport)
			}
			v, err := e.svc.Get(ctx, id)
			if err != nil || v.Status != tt.want {
				t.Fatalf("Get = %+v, %v, want %s", v, err, tt.want)
			}
			unknown := tt.want == delivery.StatusUnknown
			if v.OutcomeUnknown != unknown || v.CanMarkSent != unknown || v.CanSendAgain != unknown {
				t.Fatalf("flags = %v %v %v, want all %v", v.OutcomeUnknown, v.CanMarkSent, v.CanSendAgain, unknown)
			}
		})
	}
}

// HasUnresolved locks the export file of an assignment while one of its emails may still go out or may have gone out
// unseen (PENDING, SENDING, UNKNOWN); SENT and FAILED do not lock, and other assignments are not affected.
func TestHasUnresolved(t *testing.T) {
	tests := []struct {
		name   string
		result error // the outcome of the first attempt; nil: no attempt
		run    bool
		want   bool
	}{
		{name: "pending", want: true},
		{name: "sent", run: true},
		{name: "failed", run: true, result: delivery.ErrRejected},
		{name: "unknown", run: true, result: delivery.ErrOutcomeUnknown, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			_, snap := e.created(t)
			if tt.run {
				e.email.result = func(context.Context, *delivery.Delivery) error { return tt.result }
				_ = e.svc.Handle(ctx, snap, noReport)
			}
			if got, err := e.svc.HasUnresolved(ctx, "a1"); err != nil || got != tt.want {
				t.Fatalf("HasUnresolved(a1) = %v, %v; want %v", got, err, tt.want)
			}
			if got, err := e.svc.HasUnresolved(ctx, "a2"); err != nil || got {
				t.Fatalf("HasUnresolved(a2) = %v, %v; want false", got, err)
			}
		})
	}
}

func TestSubject(t *testing.T) {
	if got := newEnv(t).svc.Subject(); got != DefaultSubject {
		t.Errorf("default subject = %q", got)
	}
	if got := newEnvWith(t, Config{Subject: "Домашнее задание"}).svc.Subject(); got != "Домашнее задание" {
		t.Errorf("configured subject = %q", got)
	}
}
