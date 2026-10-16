package sqlite

// The lists of the homework screen (TASK-045): the newest assignments and the deliveries of one assignment.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

func TestAssignmentsListRecent(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	// Three items of one extraction at asgT0, in answer order c, a, b, and two older hand-made rows.
	for i, id := range []string{"c", "a", "b"} {
		if err := e.assignments.Save(ctx, newExtracted(t, id, "t1", i, time.Time{})); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"old1", "old2"} {
		mustExec(t, e.db, `INSERT INTO assignments (id, title, version, created_at, updated_at) VALUES (?, 'hw', 1, ?, ?)`,
			id, asgT0.Add(-time.Duration(2-i)*time.Hour).UnixMilli(), asgT0.UnixMilli())
	}

	tests := []struct {
		limit int
		want  string
	}{
		{limit: 10, want: "c,a,b,old2,old1"},
		{limit: 4, want: "c,a,b,old2"},
		{limit: 1, want: "c"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.limit), func(t *testing.T) {
			got, err := e.assignments.ListRecent(ctx, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(got))
			for i, a := range got {
				ids[i] = string(a.ID())
			}
			if strings.Join(ids, ",") != tt.want {
				t.Fatalf("ListRecent(%d) = %v, want %s (newest first, answer order inside one extraction)", tt.limit, ids, tt.want)
			}
		})
	}

	if _, err := e.assignments.ListRecent(ctx, 0); err == nil {
		t.Error("limit 0 must be refused")
	}
}

func TestAssignmentsListRecentEmpty(t *testing.T) {
	e := newCourseworkEnv(t)
	got, err := e.assignments.ListRecent(context.Background(), 5)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListRecent on an empty table = %v, %v, want an empty list", got, err)
	}
}

func TestDeliveryListByAssignment(t *testing.T) {
	db, repo := newDeliveryEnv(t)
	ctx := context.Background()
	mustExec(t, db, `INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a2', 'hw', 0, 0)`)

	save := func(d *delivery.Delivery) {
		t.Helper()
		if err := repo.Save(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	save(newTestDelivery(t, "d1", "k1"))
	save(newTestDeliveryFor(t, "x1", "kx", "a2"))
	// A later delivery of a1: created after d1.
	later, err := delivery.New(delivery.Spec{
		ID: "d2", AssignmentID: "a1", Recipient: "teacher@example.org", Subject: "Homework",
		Attachment: newTestDelivery(t, "tmp", "tmp").Attachment(), MessageID: "<d2@paratech.local>", Key: "k2",
		ConfirmedAt: dlvT0,
	}, dlvT0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	save(later)

	tests := []struct {
		a     shared.AssignmentID
		limit int
		want  string
	}{
		{a: "a1", limit: 10, want: "d2,d1"},
		{a: "a1", limit: 1, want: "d2"},
		{a: "a2", limit: 10, want: "x1"},
		{a: "none", limit: 10, want: ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d", tt.a, tt.limit), func(t *testing.T) {
			got, err := repo.ListByAssignment(ctx, tt.a, tt.limit)
			if err != nil || got == nil {
				t.Fatalf("ListByAssignment = %v, %v", got, err)
			}
			ids := make([]shared.DeliveryID, len(got))
			for i, d := range got {
				ids[i] = d.ID()
			}
			if s := strings.Join(idStrings(ids), ","); s != tt.want {
				t.Fatalf("ListByAssignment(%s, %d) = %s, want %s", tt.a, tt.limit, s, tt.want)
			}
		})
	}

	if _, err := repo.ListByAssignment(ctx, "a1", 0); err == nil {
		t.Error("limit 0 must be refused")
	}
}
