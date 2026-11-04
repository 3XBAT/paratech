package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

// retentionDB stores two chats with requests around the cutoff (ms):
//
//	a: 1 DONE at cutoff-2d, 2 STOPPED at cutoff-1ms with a line, 3 FAILED at the cutoff, 4 CANCELLED after it,
//	   5 AWAITING_PLAN (not final) long before, 6 DONE long before but current (turns = 6);
//	b: 1 CANCELLED at cutoff-2d dropped by 3, 2 DONE at cutoff-2d with its text only, 3 PLANNING (current).
//
// Every request has a text, calls and results (b2 a text only); request a1 has a journal entry.
func retentionDB(t *testing.T) (*DB, time.Time) {
	t.Helper()
	db := openTest(t, nil)
	cutoff := agentT0.Add(1000 * time.Hour)
	ms := func(d time.Duration) int64 { return cutoff.Add(d).UnixMilli() }
	day := 24 * time.Hour
	mustExec(t, db, `INSERT INTO agent_threads (id, title, turns, history_from, seen, last_active_at, version, created_at, updated_at)
		VALUES ('a', 'Чат', 6, 0, '[]', 1, 1, 1, 1), ('b', 'Чат', 3, 0, '[]', 1, 1, 1, 1)`)
	turns := []struct {
		id      string
		n       int
		status  string
		at      int64
		line    any
		dropped int
	}{
		{"a", 1, "DONE", ms(-2 * day), nil, 0},
		{"a", 2, "STOPPED", ms(-time.Millisecond), "reply: как было", 0},
		{"a", 3, "FAILED", ms(0), nil, 0},
		{"a", 4, "CANCELLED", ms(day), nil, 0},
		{"a", 5, "AWAITING_PLAN", ms(-100 * day), nil, 0},
		{"a", 6, "DONE", ms(-100 * day), nil, 0},
		{"b", 1, "CANCELLED", ms(-2 * day), nil, 3},
		{"b", 2, "DONE", ms(-2 * day), nil, 0},
		{"b", 3, "PLANNING", ms(-100 * day), nil, 0},
	}
	seq := map[string]int{}
	step := func(id string, n int, entry, body string) {
		seq[id]++
		mustExec(t, db, `INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at) VALUES (?, ?, ?, ?, ?, 1)`,
			id, seq[id], n, entry, body)
	}
	for _, u := range turns {
		mustExec(t, db, `INSERT INTO agent_turns (thread_id, number, status, scope, started_at, updated_at, reply_line, dropped_by)
			VALUES (?, ?, ?, '{}', 1, ?, ?, ?)`, u.id, u.n, u.status, u.at, u.line, u.dropped)
		step(u.id, u.n, entryStudent, fmt.Sprintf(`{"text":"просьба %s%d"}`, u.id, u.n))
		if u.id == "b" && u.n == 2 {
			continue
		}
		step(u.id, u.n, entryCalls, fmt.Sprintf(`{"calls":[{"call_id":"r1","tool":"reply","arguments_json":"{\"text\":\"ответ %s%d\"}"}],"seal":"AQ=="}`, u.id, u.n))
		step(u.id, u.n, entryResults, `{"results":[{"call_id":"r1","result_json":"{\"shown\":true}"}]}`)
	}
	mustExec(t, db, `INSERT INTO agent_actions (id, thread_id, turn, call_id, tool, status, version, created_at, updated_at)
		VALUES ('act1', 'a', 1, 'w1', 'create_folder', 'DONE', 1, 1, 1)`)
	return db, cutoff
}

func stepCount(t *testing.T, db *DB, id string, n int, entry string) int64 {
	t.Helper()
	return scanInt(t, db.sql, `SELECT count(*) FROM agent_steps WHERE thread_id = ? AND turn = ? AND entry = ?`, id, n, entry)
}

func TestAgentRetentionEndedBefore(t *testing.T) {
	db, cutoff := retentionDB(t)
	r, ctx := NewAgentRetention(db), context.Background()
	at := func(d time.Duration) time.Time { return time.UnixMilli(cutoff.Add(d).UnixMilli()).UTC() }
	a1 := thread.EndedRequest{Thread: "a", Number: 1, EndedAt: at(-48 * time.Hour)}
	b1 := thread.EndedRequest{Thread: "b", Number: 1, EndedAt: at(-48 * time.Hour)}
	a2 := thread.EndedRequest{Thread: "a", Number: 2, EndedAt: at(-time.Millisecond), ReplyFrozen: true}

	tests := []struct {
		name  string
		after thread.EndedRequest
		limit int
		want  []thread.EndedRequest
	}{
		{"all due, by end time, chat and number", thread.EndedRequest{}, 10, []thread.EndedRequest{a1, b1, a2}},
		{"the limit", thread.EndedRequest{}, 2, []thread.EndedRequest{a1, b1}},
		{"after a key of the same instant", a1, 10, []thread.EndedRequest{b1, a2}},
		{"after the last", a2, 10, []thread.EndedRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.EndedBefore(ctx, cutoff, tt.after, tt.limit)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EndedBefore = %+v, %v\nwant %+v", got, err, tt.want)
			}
		})
	}
}

func TestAgentRetentionEntries(t *testing.T) {
	db, _ := retentionDB(t)
	r, ctx := NewAgentRetention(db), context.Background()
	got, err := r.Entries(ctx, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].StudentText != "просьба a1" || got[1].Calls == nil || len(got[2].Results) != 1 {
		t.Fatalf("Entries = %+v", got)
	}
	if line, err := thread.FrozenReply(got); err != nil || line != "reply: ответ a1" {
		t.Fatalf("line = %q, %v", line, err)
	}
	// A dropped request is read too.
	if got, err := r.Entries(ctx, "b", 1); err != nil || len(got) != 3 {
		t.Fatalf("Entries of a dropped request = %+v, %v", got, err)
	}
	mustExec(t, db, `UPDATE agent_steps SET body = '{"text":"x","extra":1}' WHERE thread_id = 'a' AND turn = 4 AND entry = 'student'`)
	if _, err := r.Entries(ctx, "a", 4); err == nil {
		t.Fatal("Entries read an unknown field")
	}
}

// The purge freezes a missing line and deletes calls and results only of due requests, re-checked in the statement;
// the texts, the other requests and the journal stay.
func TestAgentRetentionPurgeDetails(t *testing.T) {
	db, cutoff := retentionDB(t)
	r, ctx := NewAgentRetention(db), context.Background()
	studentRows := scanInt(t, db.sql, `SELECT count(*) FROM agent_steps WHERE entry = 'student'`)

	tests := []struct {
		name     string
		id       string
		n        int
		line     string
		done     bool
		wantLine sql.NullString
	}{
		{"not due: exactly at the cutoff", "a", 3, "reply: x", false, sql.NullString{}},
		{"not due: after the cutoff", "a", 4, "reply: x", false, sql.NullString{}},
		{"not due: not final", "a", 5, "reply: x", false, sql.NullString{}},
		{"not due: the current request", "a", 6, "reply: x", false, sql.NullString{}},
		{"no line, none given: nothing deleted", "b", 1, "", false, sql.NullString{}},
		{"due, line frozen", "a", 1, "reply: ответ a1", true, sql.NullString{String: "reply: ответ a1", Valid: true}},
		{"again: nothing left", "a", 1, "reply: другое", false, sql.NullString{String: "reply: ответ a1", Valid: true}},
		{"a frozen line is not overwritten", "a", 2, "reply: другое", true, sql.NullString{String: "reply: как было", Valid: true}},
		{"a dropped request", "b", 1, "reply: ответ b1", true, sql.NullString{String: "reply: ответ b1", Valid: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stepCount(t, db, tt.id, tt.n, entryCalls)
			done, err := r.PurgeDetails(ctx, shared.ThreadID(tt.id), tt.n, tt.line, cutoff)
			if err != nil || done != tt.done {
				t.Fatalf("PurgeDetails = %v, %v, want %v", done, err, tt.done)
			}
			if got := replyLineOf(t, db, tt.id, tt.n); got != tt.wantLine {
				t.Fatalf("reply line = %+v, want %+v", got, tt.wantLine)
			}
			left := stepCount(t, db, tt.id, tt.n, entryCalls) + stepCount(t, db, tt.id, tt.n, entryResults)
			if tt.done && left != 0 || !tt.done && stepCount(t, db, tt.id, tt.n, entryCalls) != calls {
				t.Fatalf("details left = %d (done %v)", left, tt.done)
			}
		})
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_steps WHERE entry = 'student'`); n != studentRows {
		t.Fatalf("texts = %d, want all %d kept", n, studentRows)
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_actions WHERE id = 'act1' AND status = 'DONE'`); n != 1 {
		t.Fatal("the journal changed")
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_turns`); n != 9 {
		t.Fatalf("requests = %d, want 9", n)
	}
	if got, err := r.EndedBefore(ctx, cutoff, thread.EndedRequest{}, 10); err != nil || len(got) != 0 {
		t.Fatalf("still due after the purge: %+v, %v", got, err)
	}

	// After the purge the history reads the text with the frozen line.
	got, err := NewAgentHistory(db, fixedNow{agentT0}).Since(ctx, "a", 1, 100)
	if err != nil || len(got) != 14 || got[0].Turn != 1 || got[0].ReplyLine != "reply: ответ a1" || got[1].Turn != 2 ||
		got[1].Entry.StudentText != "просьба a2" {
		t.Fatalf("Since = %+v, %v", got, err)
	}

	// An invalid line is refused before anything is written.
	if _, err := r.PurgeDetails(ctx, "a", 3, "a\x00b", cutoff.Add(time.Hour)); err == nil {
		t.Fatal("PurgeDetails froze a line with NUL")
	}
	if got := replyLineOf(t, db, "a", 3); got.Valid || stepCount(t, db, "a", 3, entryCalls) != 1 {
		t.Fatal("a refused line changed the request")
	}
	if done, err := r.PurgeDetails(ctx, "nope", 1, "reply: x", cutoff); err != nil || done {
		t.Fatalf("a missing request: %v, %v", done, err)
	}
}
