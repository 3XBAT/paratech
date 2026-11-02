package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// droppedMarks returns dropped_by of every stored request of th1, by number.
func droppedMarks(t *testing.T, db *DB) map[int]int {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `SELECT number, dropped_by FROM agent_turns WHERE thread_id = 'th1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]int{}
	for rows.Next() {
		var n, by int
		if err := rows.Scan(&n, &by); err != nil {
			t.Fatal(err)
		}
		out[n] = by
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func turnsOfEntries(es []values.HistoryEntry) []int {
	var out []int
	for _, e := range es {
		if len(out) == 0 || out[len(out)-1] != e.Turn {
			out = append(out, e.Turn)
		}
	}
	return out
}

// «Скрыть» (thread.RestartFrom): the save of the new request marks the dropped ones in the same write, Get reads the
// marks back as DropsFrom, the history read leaves them out, and nothing is deleted. A second restart over requests
// already dropped marks them with its own number.
func TestAgentRestartFromMarksDroppedRequests(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 4; turn++ {
		endedRequest(t, repo, h, th, turn, fmt.Sprintf("ответ %d", turn))
	}
	steps := scanInt(t, db.sql, "SELECT count(*) FROM agent_steps WHERE thread_id = 'th1'")

	if err := th.RestartFrom(2, thread.Request{Text: "заново"}, agentAt(100)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(ctx, "th1", 5, values.HistoryTurn{StudentText: "заново"}); err != nil {
		t.Fatal(err)
	}
	if got := droppedMarks(t, db); !reflect.DeepEqual(got, map[int]int{1: 0, 2: 5, 3: 5, 4: 5, 5: 0}) {
		t.Fatalf("dropped_by = %v", got)
	}
	back, err := repo.Get(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	want := th.State()
	if got := back.State(); !reflect.DeepEqual(got, want) || got.Current.DropsFrom != 2 {
		t.Fatalf("restored %+v\nwant %+v", got.Current, want.Current)
	}
	entries, err := h.Since(ctx, "th1", 0, 100)
	if err != nil || fmt.Sprint(turnsOfEntries(entries)) != "[1 5]" {
		t.Fatalf("Since = %v, %v", turnsOfEntries(entries), err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM agent_steps WHERE thread_id = 'th1'"); n != steps+1 {
		t.Fatalf("stored entries = %d, want %d: nothing deleted", n, steps+1)
	}

	// Later saves of the same request keep the marks.
	if err := back.Complete(agentAt(101)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, back); err != nil {
		t.Fatal(err)
	}
	if got := droppedMarks(t, db); !reflect.DeepEqual(got, map[int]int{1: 0, 2: 5, 3: 5, 4: 5, 5: 0}) {
		t.Fatalf("dropped_by after a later save = %v", got)
	}

	if err := back.RestartFrom(1, thread.Request{Text: "с нуля"}, agentAt(102)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, back); err != nil {
		t.Fatal(err)
	}
	if got := droppedMarks(t, db); !reflect.DeepEqual(got, map[int]int{1: 6, 2: 6, 3: 6, 4: 6, 5: 6, 6: 0}) {
		t.Fatalf("dropped_by after the second restart = %v", got)
	}
	if again, err := repo.Get(ctx, "th1"); err != nil {
		t.Fatal(err)
	} else if turn, _ := again.Turn(); turn.Number() != 6 || turn.DropsFrom() != 1 {
		t.Fatalf("restored request %d drops from %d", turn.Number(), turn.DropsFrom())
	}

	// An ordinary request after it drops nothing more.
	if err := back.Complete(agentAt(103)); err != nil {
		t.Fatal(err)
	}
	if err := back.Ask(thread.Request{Text: "дальше"}, agentAt(104)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, back); err != nil {
		t.Fatal(err)
	}
	if got := droppedMarks(t, db); got[6] != 0 || got[7] != 0 || got[5] != 6 {
		t.Fatalf("dropped_by after an ordinary request = %v", got)
	}
}

// A restart before the history start of an expired-request Restart stores the start back at 0 in the save of the new
// request (TASK-249).
func TestAgentRestartFromStoresHistoryStart(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 4; turn++ {
		endedRequest(t, repo, h, th, turn, fmt.Sprintf("ответ %d", turn))
	}
	st := th.State()
	st.HistoryFrom = 4
	if th, err = thread.Restore(st); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT history_from FROM agent_threads WHERE id = 'th1'"); n != 4 {
		t.Fatalf("history_from before the restart = %d", n)
	}

	if err := th.RestartFrom(2, thread.Request{Text: "заново"}, agentAt(100)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT history_from FROM agent_threads WHERE id = 'th1'"); n != 0 {
		t.Fatalf("history_from after the restart = %d, want 0", n)
	}
	back, err := repo.Get(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := back.Turn(); back.HistoryFrom() != 0 || turn.DropsFrom() != 2 {
		t.Fatalf("restored history from %d, drops from %d", back.HistoryFrom(), turn.DropsFrom())
	}
	if err := h.Append(ctx, "th1", 5, values.HistoryTurn{StudentText: "заново"}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Since(ctx, "th1", back.HistoryFrom(), 100)
	if err != nil || fmt.Sprint(turnsOfEntries(entries)) != "[1 5]" {
		t.Fatalf("Since = %v, %v", turnsOfEntries(entries), err)
	}
}

// The marks belong to the transaction of the new request: when it rolls back, nothing is dropped.
func TestAgentRestartFromRollsBack(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 2; turn++ {
		endedRequest(t, repo, h, th, turn, "ок")
	}
	if err := th.RestartFrom(1, thread.Request{Text: "заново"}, agentAt(100)); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("later write failed")
	err = db.Do(ctx, func(ctx context.Context) error {
		if err := repo.Save(ctx, th); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do = %v", err)
	}
	if got := droppedMarks(t, db); !reflect.DeepEqual(got, map[int]int{1: 0, 2: 0}) {
		t.Fatalf("dropped_by after the rollback = %v", got)
	}
	if back, err := repo.Get(ctx, "th1"); err != nil {
		t.Fatal(err)
	} else if turn, _ := back.Turn(); turn.Number() != 2 || turn.DropsFrom() != 0 {
		t.Fatalf("after the rollback request %d drops from %d", turn.Number(), turn.DropsFrom())
	}
}
