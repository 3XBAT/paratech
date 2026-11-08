package sqlite

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// eventT0 is the time of the first queued update in these tests.
var eventT0 = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

// TASK-276: an assignment has an event only with an executed create_deadline_event proposal and no waiting removal.
func TestHasDeadlineEvent(t *testing.T) {
	const cols = `INSERT INTO proposals (id, kind, status, assignment_id, due_at, reason, confirmed_at, executed_at,
		version, created_at, updated_at) VALUES `
	tests := []struct {
		name    string
		rows    []string
		removal bool
		want    bool
	}{
		{name: "no proposal"},
		{name: "executed", rows: []string{`('p1', 'create_deadline_event', 'EXECUTED', 'a1', 1, '', 1, 1, 1, 0, 0)`}, want: true},
		{name: "executed with a waiting removal", rows: []string{
			`('p1', 'create_deadline_event', 'EXECUTED', 'a1', 1, '', 1, 1, 1, 0, 0)`}, removal: true},
		{name: "waiting for «Да»", rows: []string{`('p1', 'create_deadline_event', 'VALIDATED', 'a1', 1, '', NULL, NULL, 1, 0, 0)`}},
		{name: "confirmed, not yet executed", rows: []string{
			`('p1', 'create_deadline_event', 'CONFIRMED', 'a1', 1, '', 1, NULL, 1, 0, 0)`}},
		{name: "declined", rows: []string{`('p1', 'create_deadline_event', 'DECLINED', 'a1', 1, '', NULL, NULL, 1, 0, 0)`}},
		{name: "stale", rows: []string{`('p1', 'create_deadline_event', 'STALE', 'a1', 1, 'assignment_changed', 1, NULL, 1, 0, 0)`}},
		{name: "executed for another assignment", rows: []string{
			`('p1', 'create_deadline_event', 'EXECUTED', 'a2', 1, '', 1, 1, 1, 0, 0)`}},
		{name: "an executed export is no event", rows: []string{
			`('p1', 'export_homework', 'EXECUTED', 'a1', NULL, '', 1, 1, 1, 0, 0)`}},
		{name: "a declined one and an executed one", rows: []string{
			`('p1', 'create_deadline_event', 'DECLINED', 'a1', 1, '', NULL, NULL, 1, 0, 0)`,
			`('p2', 'create_deadline_event', 'EXECUTED', 'a1', 1, '', 1, 1, 1, 0, 0)`}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, ctx := openTest(t, nil), context.Background()
			for _, row := range tt.rows {
				mustExec(t, db, cols+row)
			}
			if tt.removal {
				if err := NewDeadlineEventRemovals(db).QueueDeadlineRemoval(ctx, "a1", eventT0); err != nil {
					t.Fatal(err)
				}
			}
			got, err := NewDeadlineEventUpdates(db).HasDeadlineEvent(ctx, "a1")
			if err != nil || got != tt.want {
				t.Fatalf("HasDeadlineEvent = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// TASK-276: a repeated queue keeps one row with the later time; the list is oldest first and bounded; the row goes
// with a rolled-back transaction; a purge of a missing row is no error.
func TestQueueDeadlineUpdate(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	updates := NewDeadlineEventUpdates(db)

	for _, q := range []struct {
		id shared.AssignmentID
		at time.Time
	}{{"a1", eventT0}, {"a2", eventT0.Add(time.Minute)}, {"a1", eventT0.Add(time.Hour)}} {
		if err := updates.QueueDeadlineUpdate(ctx, q.id, q.at); err != nil {
			t.Fatal(err)
		}
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_updates WHERE assignment_id = 'a1'"); n != 1 {
		t.Fatalf("rows of a1 = %d, want 1", n)
	}
	if n := scanInt(t, db.sql, "SELECT created_at FROM deadline_event_updates WHERE assignment_id = 'a1'"); n != eventT0.Add(time.Hour).UnixMilli() {
		t.Fatalf("created_at of a1 = %d, want the later time", n)
	}

	for _, tc := range []struct {
		limit int
		want  []shared.AssignmentID
	}{
		{limit: 10, want: []shared.AssignmentID{"a2", "a1"}},
		{limit: 1, want: []shared.AssignmentID{"a2"}},
		{limit: 0, want: []shared.AssignmentID{}},
		{limit: -1, want: []shared.AssignmentID{}},
	} {
		ids, err := updates.ListDeadlineUpdates(ctx, tc.limit)
		if err != nil || !slices.Equal(ids, tc.want) {
			t.Errorf("ListDeadlineUpdates(%d) = %v, %v; want %v", tc.limit, ids, err, tc.want)
		}
	}

	rollback := errors.New("rolled back")
	err := db.Do(ctx, func(ctx context.Context) error {
		if err := updates.QueueDeadlineUpdate(ctx, "a3", eventT0); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Do = %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_updates WHERE assignment_id = 'a3'"); n != 0 {
		t.Fatalf("a rolled-back queue left %d rows", n)
	}

	for range 2 {
		if err := updates.PurgeDeadlineUpdate(ctx, "a1"); err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := updates.ListDeadlineUpdates(ctx, 10); err != nil || !slices.Equal(ids, []shared.AssignmentID{"a2"}) {
		t.Fatalf("updates after the purge = %v, %v; want [a2]", ids, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM deadline_event_removals"); n != 0 {
		t.Fatalf("an update queued %d removals", n)
	}
}
