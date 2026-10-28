package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The list filter of «Открытые / Выполненные / Все» (ADR-0033 §9) runs in SQL before the limit: done rows newer than
// every open one never crowd them out, open rows come first in «Все», deleted rows are never listed.
func TestAssignmentsListRecentByState(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	at := func(h int) int64 { return asgT0.Add(time.Duration(h) * time.Hour).UnixMilli() }
	rows := []struct {
		id            string
		created       int
		done, deleted bool
	}{
		{id: "o1", created: 1}, {id: "o2", created: 2}, {id: "o3", created: 3},
		{id: "d1", created: 4, done: true}, {id: "d2", created: 5, done: true},
		{id: "x1", created: 6, deleted: true}, {id: "x2", created: 7, done: true, deleted: true},
	}
	for _, r := range rows {
		var done, deleted any
		if r.done {
			done = at(10)
		}
		if r.deleted {
			deleted = at(11)
		}
		mustExec(t, e.db, `INSERT INTO assignments (id, title, version, created_at, updated_at, done_at, deleted_at)
			VALUES (?, 'hw', 1, ?, ?, ?, ?)`, r.id, at(r.created), at(r.created), done, deleted)
	}

	tests := []struct {
		open, done bool
		limit      int
		want       string
	}{
		{open: true, limit: 10, want: "o3,o2,o1"},
		{open: true, limit: 2, want: "o3,o2"},
		{done: true, limit: 10, want: "d2,d1"},
		{done: true, limit: 1, want: "d2"},
		{open: true, done: true, limit: 10, want: "o3,o2,o1,d2,d1"},
		{open: true, done: true, limit: 4, want: "o3,o2,o1,d2"},
		{limit: 10, want: ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("open=%v done=%v limit=%d", tt.open, tt.done, tt.limit), func(t *testing.T) {
			got, err := e.assignments.ListRecentByState(ctx, tt.open, tt.done, tt.limit)
			if err != nil || got == nil {
				t.Fatalf("ListRecentByState = %v, %v", got, err)
			}
			ids := make([]string, len(got))
			for i, a := range got {
				ids[i] = string(a.ID())
				if a.Done() != strings.HasPrefix(ids[i], "d") || a.Deleted() {
					t.Errorf("%s: done %v, deleted %v", ids[i], a.Done(), a.Deleted())
				}
			}
			if strings.Join(ids, ",") != tt.want {
				t.Fatalf("ListRecentByState = %v, want %s", ids, tt.want)
			}
		})
	}

	if _, err := e.assignments.ListRecentByState(ctx, true, true, 0); err == nil {
		t.Error("limit 0 must be refused")
	}
}
