package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// replyEntries are the stored entries of a request that ended with a reply card: its text, the echo and the result.
func replyEntries(turn int, text, reply string) []values.HistoryEntry {
	return []values.HistoryEntry{
		{Turn: turn, Entry: values.HistoryTurn{StudentText: text}},
		{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{
			Calls: []values.RawCall{{CallID: "r1", Tool: "reply", ArgumentsJSON: `{"text":"` + reply + `"}`}}, Seal: []byte{1},
		}}},
		{Turn: turn, Entry: values.HistoryTurn{Results: []values.ToolResult{{CallID: "r1", ResultJSON: `{"shown":true}`}}}},
	}
}

// endedRequest stores request turn of th: asked, its entries appended, DONE with the reply line frozen.
func endedRequest(t *testing.T, repo *AgentThreadRepository, h *AgentHistory, th *thread.Thread, turn int, reply string) []values.HistoryEntry {
	t.Helper()
	ctx := context.Background()
	text := fmt.Sprintf("просьба %d", turn)
	if err := th.Ask(thread.Request{Text: text}, agentAt(10*turn)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	entries := replyEntries(turn, text, reply)
	for _, e := range entries {
		if err := h.Append(ctx, th.ID(), e.Turn, e.Entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := th.Complete(agentAt(10*turn + 1)); err != nil {
		t.Fatal(err)
	}
	if err := th.FreezeReply(entries, agentAt(10*turn+1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	return entries
}

func replyLineOf(t *testing.T, db *DB, id string, turn int) sql.NullString {
	t.Helper()
	var line sql.NullString
	err := db.sql.QueryRowContext(context.Background(),
		`SELECT reply_line FROM agent_turns WHERE thread_id = ? AND number = ?`, id, turn).Scan(&line)
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// Old requests (before 0047) have no reply line and old chats no summary: they read as before and the window builds
// their replies from the steps, exactly as without the migration. Down drops the columns and keeps the rows.
func TestMigrationAgentReplyKeepsOldRows(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db, steps := migratingAgentDB(t)
	if err := steps.UpTo(ctx, agentReplyVersion-1); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`INSERT INTO agent_threads (id, title, turns, history_from, seen, last_active_at, version, created_at, updated_at)
			VALUES ('th-old', 'Старый чат', 2, 0, '[]', 2000, 4, 1000, 2100)`,
		`INSERT INTO agent_turns (thread_id, number, status, scope, steps, started_at, updated_at) VALUES
			('th-old', 1, 'DONE', '{}', 1, 1000, 1100), ('th-old', 2, 'DONE', '{}', 1, 2000, 2100)`,
		`INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at) VALUES
			('th-old', 1, 1, 'student', '{"text":"просьба 1"}', 1000),
			('th-old', 2, 1, 'calls', '{"calls":[{"call_id":"r1","tool":"reply","arguments_json":"{\"text\":\"первый\"}"}],"seal":"AQ=="}', 1000),
			('th-old', 3, 1, 'results', '{"results":[{"call_id":"r1","result_json":"{\"shown\":true}"}]}', 1000),
			('th-old', 4, 2, 'student', '{"text":"просьба 2"}', 2000),
			('th-old', 5, 2, 'calls', '{"calls":[{"call_id":"r1","tool":"reply","arguments_json":"{\"text\":\"второй\"}"}],"seal":"AQ=="}', 2000),
			('th-old', 6, 2, 'results', '{"results":[{"call_id":"r1","result_json":"{\"shown\":true}"}]}', 2000)`,
	} {
		mustExec(t, db, s)
	}
	if err := steps.UpTo(ctx, agentReplyVersion); err != nil {
		t.Fatalf("up to %d: %v", agentReplyVersion, err)
	}
	for query, n := range map[string]int64{
		"SELECT count(*) FROM agent_turns WHERE reply_line IS NULL":                                                                                2,
		"SELECT count(*) FROM agent_threads WHERE summary_text IS NULL AND summary_through IS NULL AND summary_at IS NULL AND forked_from IS NULL": 1,
		"SELECT count(*) FROM pragma_foreign_key_check":                                                                                            0,
	} {
		if got := scanInt(t, db.sql, query); got != n {
			t.Errorf("%s = %d, want %d", query, got, n)
		}
	}

	th, err := NewAgentThreadRepository(db).Get(ctx, "th-old")
	if err != nil {
		t.Fatalf("Get of an old chat: %v", err)
	}
	if turn, _ := th.Turn(); !th.Summary().IsZero() || th.ForkedFrom() != "" || turn.ReplyLine() != "" || th.Version() != 4 {
		t.Fatalf("old chat = %+v", th.State())
	}
	got, err := NewAgentHistory(db, fixedNow{agentT0}).Since(ctx, "th-old", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := append(replyEntries(1, "просьба 1", "первый"), replyEntries(2, "просьба 2", "второй")...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Since = %+v\nwant %+v", got, want)
	}
	next := values.HistoryEntry{Turn: 3, Entry: values.HistoryTurn{StudentText: "просьба 3"}}
	window := thread.Window(append(got, next), 3, values.HistorySummary{})
	if !reflect.DeepEqual(window, thread.Window(append(want, next), 3, values.HistorySummary{})) ||
		len(window) != 5 || window[1].AgentReply != "reply: первый" || window[3].AgentReply != "reply: второй" {
		t.Fatalf("window = %+v", window)
	}

	// New data at 0047, then Down: the columns go, the rows stay; Up again reads them as old rows.
	if err := th.Ask(thread.Request{Text: "просьба 3"}, agentAt(100)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(agentAt(101)); err != nil {
		t.Fatal(err)
	}
	if err := th.FreezeReply(nil, agentAt(101)); err != nil {
		t.Fatal(err)
	}
	if err := th.Compact(summaryText(t, 2, "сводка"), agentAt(102)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentThreadRepository(db)
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if err := steps.DownTo(ctx, agentReplyVersion-1); err != nil {
		t.Fatalf("down to %d: %v", agentReplyVersion-1, err)
	}
	for query, n := range map[string]int64{
		"SELECT count(*) FROM agent_turns WHERE thread_id = 'th-old'": 3,
		"SELECT count(*) FROM agent_steps":                            6,
		"SELECT count(*) FROM pragma_table_info('agent_threads') WHERE name IN ('summary_text', 'summary_through', 'summary_at', 'forked_from')": 0,
		"SELECT count(*) FROM pragma_table_info('agent_turns') WHERE name = 'reply_line'":                                                        0,
		"SELECT count(*) FROM pragma_foreign_key_check":                                                                                          0,
	} {
		if got := scanInt(t, db.sql, query); got != n {
			t.Errorf("%s = %d, want %d", query, got, n)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	again, err := repo.Get(ctx, "th-old")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := again.Turn(); !again.Summary().IsZero() || turn.ReplyLine() != "" || turn.Number() != 3 {
		t.Fatalf("after down and up = %+v", again.State())
	}
}

func summaryText(t *testing.T, through int, text string) values.HistorySummary {
	t.Helper()
	s, err := values.ParseHistorySummary(through, text)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The summary, the fork source and the reply line go through Save and Get unchanged.
func TestAgentThreadSummaryForkAndReplyRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th-fork", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	endedRequest(t, repo, h, th, 1, "первый")
	endedRequest(t, repo, h, th, 2, "второй")
	if err := th.Compact(summaryText(t, 2, "Студент просил «две вещи».\nОбе сделаны."), agentAt(30)); err != nil {
		t.Fatal(err)
	}
	// A fork is created by TASK-271; here the stored form carries the source.
	st := th.State()
	st.ForkedFrom = "th-source"
	forked, err := thread.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, forked); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "th-fork")
	if err != nil {
		t.Fatal(err)
	}
	sameThread(t, got, forked)
	if got.ForkedFrom() != "th-source" || got.Summary().Through() != 2 || !got.SummaryAt().Equal(agentAt(30)) {
		t.Fatalf("restored = %+v", got.State())
	}
	if turn, _ := got.Turn(); turn.ReplyLine() != "reply: второй" {
		t.Fatalf("reply line = %q", turn.ReplyLine())
	}
	if line := replyLineOf(t, db, "th-fork", 1); line.String != "reply: первый" {
		t.Fatalf("line of the earlier request = %+v", line)
	}

	// A new request keeps the earlier lines and the summary; a chat without them stores NULL.
	if err := got.Ask(thread.Request{Text: "ещё"}, agentAt(40)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if line := replyLineOf(t, db, "th-fork", 3); line.Valid {
		t.Fatalf("line of a running request = %+v", line)
	}
	if line := replyLineOf(t, db, "th-fork", 2); line.String != "reply: второй" {
		t.Fatalf("line of request 2 after the next one = %+v", line)
	}
	plain := savedChat(t, repo, "plain", 1, 0)
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_threads WHERE id = 'plain' AND summary_text IS NULL AND summary_through IS NULL
		AND summary_at IS NULL AND forked_from IS NULL`); n != 1 {
		t.Fatalf("columns of a plain chat are not NULL")
	}
	if line := replyLineOf(t, db, string(plain.ID()), 1); line.Valid {
		t.Fatalf("a request ended without FreezeReply stored %+v", line)
	}
}

// A STOPPED request has its line; when it goes on, the next Save clears it, and its real end freezes a new one.
func TestAgentReplyLineFollowsAResumedRequest(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(thread.Request{Text: "сделай"}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopUser, agentAt(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.FreezeReply(replyEntries(1, "сделай", "начало"), agentAt(2)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if line := replyLineOf(t, db, "th1", 1); line.String != "reply: начало" {
		t.Fatalf("line of the stopped request = %+v", line)
	}

	if err := th.Continue(agentAt(3)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if line := replyLineOf(t, db, "th1", 1); line.Valid {
		t.Fatalf("line after «Продолжить» = %+v, want NULL", line)
	}
	if got, err := repo.Get(ctx, "th1"); err != nil {
		t.Fatal(err)
	} else if turn, _ := got.Turn(); turn.ReplyLine() != "" {
		t.Fatalf("restored line of a running request = %q", turn.ReplyLine())
	}

	if err := th.Complete(agentAt(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.FreezeReply(replyEntries(1, "сделай", "конец"), agentAt(4)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if line := replyLineOf(t, db, "th1", 1); line.String != "reply: конец" {
		t.Fatalf("line at the real end = %+v", line)
	}
}

// The CHECKs of the new columns: the summary is all or none, through >= 1, the sizes are bytes of UTF-8.
func TestAgentReplyAndSummaryChecks(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	savedChat(t, NewAgentThreadRepository(db), "c1", 1, 0)
	const summary = `UPDATE agent_threads SET summary_text = ?, summary_through = ?, summary_at = ? WHERE id = 'c1'`
	null := sql.NullInt64{}
	tests := []struct {
		name  string
		query string
		args  []any
		ok    bool
	}{
		{"all three", summary, []any{"сводка", 1, 5}, true},
		{"none", summary, []any{nil, nil, nil}, true},
		{"text only", summary, []any{"сводка", nil, nil}, false},
		{"no time", summary, []any{"сводка", 1, null}, false},
		{"time only", summary, []any{nil, nil, 5}, false},
		{"through 0", summary, []any{"сводка", 0, 5}, false},
		{"empty text", summary, []any{"", 1, 5}, false},
		{"8192 bytes", summary, []any{strings.Repeat("я", 4096), 1, 5}, true},
		{"8193 bytes", summary, []any{strings.Repeat("я", 4096) + "a", 1, 5}, false},
		{"fork source", `UPDATE agent_threads SET forked_from = ? WHERE id = 'c1'`, []any{"th-x"}, true},
		{"empty fork source", `UPDATE agent_threads SET forked_from = ? WHERE id = 'c1'`, []any{""}, false},
		{"long fork source", `UPDATE agent_threads SET forked_from = ? WHERE id = 'c1'`, []any{strings.Repeat("a", 129)}, false},
		{"reply line", `UPDATE agent_turns SET reply_line = ? WHERE thread_id = 'c1'`, []any{"reply: ок"}, true},
		{"2048 bytes", `UPDATE agent_turns SET reply_line = ? WHERE thread_id = 'c1'`, []any{strings.Repeat("я", 1024)}, true},
		{"2049 bytes", `UPDATE agent_turns SET reply_line = ? WHERE thread_id = 'c1'`, []any{strings.Repeat("я", 1024) + "a"}, false},
		{"empty reply line", `UPDATE agent_turns SET reply_line = ? WHERE thread_id = 'c1'`, []any{""}, false},
	}
	for _, tt := range tests {
		_, err := db.sql.ExecContext(ctx, tt.query, tt.args...)
		if tt.ok && err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
		if !tt.ok && !errors.Is(MapError(ctx, err), ErrConstraint) {
			t.Errorf("%s: %v, want ErrConstraint", tt.name, err)
		}
	}
}

// A stored summary, fork source or reply line that the domain refuses is an error on read, never skipped.
func TestAgentRestoreRefusesCorruptReplySummaryAndFork(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		query    string
		args     []any
		noChecks bool // written past the CHECKs, as a broken tool could
	}{
		{"control character", `UPDATE agent_threads SET summary_text = ?, summary_through = 1, summary_at = ? WHERE id = 'c1'`,
			[]any{"a\x01b", agentAt(5).UnixMilli()}, false},
		{"untrimmed text", `UPDATE agent_threads SET summary_text = ?, summary_through = 1, summary_at = ? WHERE id = 'c1'`,
			[]any{" сводка", agentAt(5).UnixMilli()}, false},
		{"covers a later request", `UPDATE agent_threads SET summary_text = 'сводка', summary_through = 2, summary_at = ? WHERE id = 'c1'`,
			[]any{agentAt(5).UnixMilli()}, false},
		{"time before created", `UPDATE agent_threads SET summary_text = 'сводка', summary_through = 1, summary_at = 0 WHERE id = 'c1'`, nil, false},
		{"through 0", `UPDATE agent_threads SET summary_text = 'сводка', summary_through = 0, summary_at = ? WHERE id = 'c1'`,
			[]any{agentAt(5).UnixMilli()}, true},
		{"text only", `UPDATE agent_threads SET summary_text = 'сводка' WHERE id = 'c1'`, nil, true},
		{"no time", `UPDATE agent_threads SET summary_text = 'сводка', summary_through = 1 WHERE id = 'c1'`, nil, true},
		{"fork of itself", `UPDATE agent_threads SET forked_from = 'c1' WHERE id = 'c1'`, nil, false},
		{"bad fork id", `UPDATE agent_threads SET forked_from = 'a/b' WHERE id = 'c1'`, nil, false},
		{"empty fork id", `UPDATE agent_threads SET forked_from = '' WHERE id = 'c1'`, nil, true},
		{"blank reply line", `UPDATE agent_turns SET reply_line = '   ' WHERE thread_id = 'c1'`, nil, false},
		{"NUL in reply line", `UPDATE agent_turns SET reply_line = ? WHERE thread_id = 'c1'`, []any{"a\x00b"}, false},
		{"invalid UTF-8", `UPDATE agent_turns SET reply_line = CAST(X'FF61' AS TEXT) WHERE thread_id = 'c1'`, nil, false},
		{"empty reply line", `UPDATE agent_turns SET reply_line = '' WHERE thread_id = 'c1'`, nil, true},
		{"line of a running request", `UPDATE agent_turns SET reply_line = 'reply: x', status = 'PLANNING' WHERE thread_id = 'c1'`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTest(t, nil)
			repo := NewAgentThreadRepository(db)
			savedChat(t, repo, "c1", 1, 0)
			conn, err := db.sql.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if tt.noChecks {
				if _, err := conn.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.ExecContext(ctx, tt.query, tt.args...); err != nil {
				t.Fatalf("corrupt the row: %v", err)
			}
			if _, err := conn.ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = repo.Get(ctx, "c1")
			if err == nil || !strings.Contains(err.Error(), "stored agent thread c1") {
				t.Fatalf("Get = %v, want a stored agent thread error", err)
			}
		})
	}
}

// Since gives every entry the frozen line of its request ("" without one) and still leaves out dropped requests; a
// stored line the value checks refuse is an error.
func TestAgentHistoryReplyLines(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	var want []values.HistoryEntry
	for turn := 1; turn <= 3; turn++ {
		entries := endedRequest(t, repo, h, th, turn, fmt.Sprintf("ответ %d", turn))
		if turn == 2 {
			continue
		}
		for _, e := range entries {
			e.ReplyLine = fmt.Sprintf("reply: ответ %d", turn)
			want = append(want, e)
		}
	}
	mustExec(t, db, `UPDATE agent_turns SET dropped_by = 3 WHERE thread_id = 'th1' AND number = 2`)
	mustExec(t, db, `UPDATE agent_turns SET reply_line = NULL WHERE thread_id = 'th1' AND number = 1`)
	for i := range 3 {
		want[i].ReplyLine = ""
	}
	got, err := h.Since(ctx, "th1", 0, 100)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Since = %+v, %v\nwant %+v", got, err, want)
	}

	mustExec(t, db, `UPDATE agent_turns SET reply_line = ' ' WHERE thread_id = 'th1' AND number = 1`)
	if _, err := h.Since(ctx, "th1", 0, 100); err == nil {
		t.Fatal("a blank stored reply line was read")
	}
	if _, err := h.Since(ctx, "th1", 3, 100); err != nil {
		t.Fatalf("Since of the later request only: %v", err)
	}
	if _, err := repo.Get(ctx, shared.ThreadID("th1")); err != nil {
		t.Fatalf("Get with a bad line of an earlier request: %v (only the current one is restored)", err)
	}
}
