package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// dumpRows renders every row of a query as text, so a test can compare rows byte for byte.
func dumpRows(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if s, ok := v.([]byte); ok {
				v = string(s)
			}
			fmt.Fprintf(&b, "%s=%v|", cols[i], v)
		}
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// sourceRows are every stored row of chat id.
func sourceRows(t *testing.T, db *DB, id string) string {
	t.Helper()
	return dumpRows(t, db, `SELECT * FROM agent_threads WHERE id = ?`, id) +
		dumpRows(t, db, `SELECT * FROM agent_turns WHERE thread_id = ? ORDER BY number`, id) +
		dumpRows(t, db, `SELECT * FROM agent_steps WHERE thread_id = ? ORDER BY seq`, id)
}

// forkSource is a chat th1 of six requests: 1 with a scope and mentions, 2 without a frozen line (an old request),
// 3 and 4 hidden by request 5 («Скрыть»), 6 the one to fork from; the summary covers 1…2.
func forkSource(t *testing.T, db *DB) (*AgentThreadRepository, *AgentHistory, *thread.Thread) {
	t.Helper()
	ctx := context.Background()
	repo, h := NewAgentThreadRepository(db), NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewScope(agentRef(t, values.KindSubject, "s1"), values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mentions := []values.Mention{agentMention(t, values.KindLesson, "l1", "Лекция 1")}
	request := func(turn int, r thread.Request, restartFrom int) {
		t.Helper()
		var err error
		if restartFrom > 0 {
			err = th.RestartFrom(restartFrom, r, agentAt(10*turn))
		} else {
			err = th.Ask(r, agentAt(10*turn))
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
		entries := replyEntries(turn, r.Text, fmt.Sprintf("ответ %d", turn))
		for _, e := range entries {
			if err := h.Append(ctx, "th1", turn, e.Entry); err != nil {
				t.Fatal(err)
			}
		}
		if err := th.Complete(agentAt(10*turn + 1)); err != nil {
			t.Fatal(err)
		}
		if err := th.FreezeReply(entries, agentAt(10*turn+1)); err != nil {
			t.Fatal(err)
		}
		if turn == 2 {
			if err := th.Compact(summaryText(t, 2, "сводка 1–2"), agentAt(10*turn+2)); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
	}
	request(1, thread.Request{Scope: scope, Mentions: mentions, Text: "просьба 1"}, 0)
	request(2, thread.Request{Text: "просьба 2"}, 0)
	request(3, thread.Request{Text: "скрытая 3"}, 0)
	request(4, thread.Request{Text: "скрытая 4"}, 0)
	request(5, thread.Request{Text: "просьба 5"}, 3)
	request(6, thread.Request{Text: "просьба 6\n[upload:u1]"}, 0)
	mustExec(t, db, `UPDATE agent_turns SET reply_line = NULL WHERE thread_id = 'th1' AND number = 2`)
	return repo, h, th
}

func forkTo(t *testing.T, repo *AgentThreadRepository, src *thread.Thread, n int, id shared.ThreadID) thread.Fork {
	t.Helper()
	past, err := repo.ForkSource(context.Background(), src.ID(), n)
	if err != nil {
		t.Fatal(err)
	}
	f, err := thread.ForkOf(src, n, past, id, agentAt(500))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFork(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	return f
}

// ForkSource reads the visible requests with their texts, a request without a frozen line with its entries.
func TestAgentForkSource(t *testing.T) {
	db := openTest(t, nil)
	repo, _, src := forkSource(t, db)
	past, err := repo.ForkSource(context.Background(), src.ID(), 6)
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, p := range past {
		nums = append(nums, p.Number)
	}
	if fmt.Sprint(nums) != "[1 2 5 6]" {
		t.Fatalf("requests = %v", nums)
	}
	first := past[0]
	if first.Scope.Subject().ID() != "s1" || len(first.Mentions) != 1 || first.Mentions[0].Label() != "Лекция 1" ||
		!reflect.DeepEqual(first.Texts, []string{"просьба 1"}) || first.ReplyLine != "reply: ответ 1" || first.Entries != nil ||
		!first.StartedAt.Equal(agentAt(10)) || !first.EndedAt.Equal(agentAt(11)) {
		t.Fatalf("request 1 = %+v", first)
	}
	if old := past[1]; old.ReplyLine != "" || len(old.Entries) != 3 || thread.Reply(old.Entries) != "reply: ответ 2" {
		t.Fatalf("request 2 = %+v", old)
	}
	if got := past[3].Texts; !reflect.DeepEqual(got, []string{"просьба 6\n[upload:u1]"}) {
		t.Fatalf("texts of 6 = %q", got)
	}
	if past, err := repo.ForkSource(context.Background(), src.ID(), 4); err != nil || len(past) != 2 {
		t.Fatalf("up to a hidden request = %+v, %v", past, err)
	}

	// A stored row that does not decode fails the read, as Get and Since fail.
	mustExec(t, db, `UPDATE agent_steps SET body = '{"text":"x","extra":1}' WHERE thread_id = 'th1' AND seq = 1`)
	if _, err := repo.ForkSource(context.Background(), src.ID(), 6); err == nil {
		t.Fatal("a bad stored entry was read")
	}
}

// A fork stores the visible requests before n as DONE copies with their texts only; hidden requests, calls, results,
// seals and seen ids stay behind, and the source does not change by a byte. The fork reads back as it was built, its
// window carries no echo, and its next step has a key of its own.
func TestAgentForkStoresTheCopies(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo, h, src := forkSource(t, db)
	before := sourceRows(t, db, "th1")

	f := forkTo(t, repo, src, 6, "th-fork")
	if got := sourceRows(t, db, "th1"); got != before {
		t.Fatalf("source rows changed:\n%s\nwas\n%s", got, before)
	}
	if f.Thread.Version() != 1 {
		t.Fatalf("version after SaveFork = %d", f.Thread.Version())
	}
	rows := dumpRows(t, db, `SELECT number, status, dropped_by, steps, reply_line FROM agent_turns WHERE thread_id = 'th-fork' ORDER BY number`)
	want := "number=1|status=DONE|dropped_by=0|steps=0|reply_line=reply: ответ 1|\n" +
		"number=2|status=DONE|dropped_by=0|steps=0|reply_line=reply: ответ 2|\n" +
		"number=5|status=DONE|dropped_by=0|steps=0|reply_line=reply: ответ 5|\n"
	if rows != want {
		t.Fatalf("fork requests:\n%s\nwant\n%s", rows, want)
	}
	steps := dumpRows(t, db, `SELECT seq, turn, entry, body, created_at FROM agent_steps WHERE thread_id = 'th-fork' ORDER BY seq`)
	at := agentAt(500).UnixMilli()
	want = fmt.Sprintf("seq=1|turn=1|entry=student|body={\"text\":\"просьба 1\"}|created_at=%d|\n"+
		"seq=2|turn=2|entry=student|body={\"text\":\"просьба 2\"}|created_at=%d|\n"+
		"seq=3|turn=5|entry=student|body={\"text\":\"просьба 5\"}|created_at=%d|\n", at, at, at)
	if steps != want {
		t.Fatalf("fork steps:\n%s\nwant\n%s", steps, want)
	}
	all := sourceRows(t, db, "th-fork")
	for _, hidden := range []string{"скрытая", "ответ 3", "ответ 4", "просьба 6", "seal", "upload"} {
		if strings.Contains(all, hidden) {
			t.Errorf("the fork holds %q:\n%s", hidden, all)
		}
	}
	thread1 := dumpRows(t, db, `SELECT forked_from, seen, history_from, turns, summary_text, summary_through, summary_at FROM agent_threads WHERE id = 'th-fork'`)
	if want := fmt.Sprintf("forked_from=th1|seen=[]|history_from=0|turns=5|summary_text=сводка 1–2|summary_through=2|summary_at=%d|\n", at); thread1 != want {
		t.Fatalf("fork thread = %s, want %s", thread1, want)
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_actions WHERE thread_id = 'th-fork'`); n != 0 {
		t.Fatalf("%d journal rows", n)
	}

	back, err := repo.Get(ctx, "th-fork")
	if err != nil || !reflect.DeepEqual(back.State(), f.Thread.State()) || back.Title() != "просьба 1 (fork)" {
		t.Fatalf("Get = %+v, %v\nwant %+v", back.State(), err, f.Thread.State())
	}
	if m, err := repo.RequestMentions(ctx, "th-fork", 1); err != nil || len(m) != 1 || m[0].Label() != "Лекция 1" {
		t.Fatalf("mentions of 1 = %v, %v", m, err)
	}

	// The next request of the fork: its window has the summary, then the pair of 5, then its text; no echo.
	if err := back.Ask(thread.Request{Text: "просьба 6 заново"}, agentAt(501)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, back); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(ctx, "th-fork", 6, values.HistoryTurn{StudentText: "просьба 6 заново"}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Since(ctx, "th-fork", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	window := thread.Window(entries, 6, back.Summary())
	if len(window) != 4 || window[0].Summary == "" || window[1].StudentText != "просьба 5" ||
		window[2].AgentReply != "reply: ответ 5" || window[3].StudentText != "просьба 6 заново" {
		t.Fatalf("window = %+v", window)
	}
	for _, w := range window {
		if w.Calls != nil || len(w.Results) > 0 {
			t.Fatalf("the window sends an echo: %+v", w)
		}
	}
	key, err := back.BeginStep(agentAt(502))
	if err != nil || key != "ag:th-fork:6:1" {
		t.Fatalf("step key = %q, %v", key, err)
	}
}

// Without the summary (it covers request n) the fork's window starts with the first copy's text.
func TestAgentForkWithoutSummary(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo, h, src := forkSource(t, db)
	f := forkTo(t, repo, src, 2, "th-fork")
	if !f.Thread.Summary().IsZero() || scanInt(t, db.sql, `SELECT count(*) FROM agent_threads WHERE id = 'th-fork' AND summary_text IS NULL AND summary_at IS NULL`) != 1 {
		t.Fatalf("summary copied: %+v", f.Thread.Summary())
	}
	if err := f.Thread.Ask(thread.Request{Text: "снова 2"}, agentAt(501)); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Since(ctx, "th-fork", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, values.HistoryEntry{Turn: 2, Entry: values.HistoryTurn{StudentText: "снова 2"}})
	window := thread.Window(entries, 2, f.Thread.Summary())
	if len(window) != 3 || window[0].StudentText != "просьба 1" || window[1].AgentReply != "reply: ответ 1" {
		t.Fatalf("window = %+v", window)
	}
}

// A chat with more step rows than one history read holds still gives a fork every early request.
func TestAgentForkOfALongChat(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo, h := NewAgentThreadRepository(db), NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	endedRequest(t, repo, h, th, 1, "первый")
	if err := th.Ask(thread.Request{Text: "просьба 2"}, agentAt(20)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(ctx, "th1", 2, values.HistoryTurn{StudentText: "просьба 2"}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1100)
		INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at)
		SELECT 'th1', 4 + i, 2, 'results', '{"results":[{"call_id":"c1","result_json":"{\"ok\":true}"}]}', 0 FROM n`)
	if err := th.Complete(agentAt(21)); err != nil {
		t.Fatal(err)
	}
	if err := th.FreezeReply(nil, agentAt(21)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	endedRequest(t, repo, h, th, 3, "третий")

	if entries, err := h.Since(ctx, "th1", 1, maxAgentHistoryRows); err != nil || entries[0].Turn == 1 {
		t.Fatalf("the history read reaches request 1, so this proves nothing: %v", err)
	}
	f := forkTo(t, repo, th, 3, "th-fork")
	if len(f.Requests) != 2 || f.Requests[0].Number != 1 || !reflect.DeepEqual(f.Requests[0].Texts, []string{"просьба 1"}) ||
		f.Requests[0].ReplyLine != "reply: первый" || f.Requests[1].ReplyLine != thread.NoReply {
		t.Fatalf("copies = %+v", f.Requests)
	}
	if n := scanInt(t, db.sql, `SELECT count(*) FROM agent_steps WHERE thread_id = 'th-fork'`); n != 2 {
		t.Fatalf("fork steps = %d", n)
	}
}

// A fork onto an id stored first is a version conflict, and nothing of it is written.
func TestAgentSaveForkConflict(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo, _, src := forkSource(t, db)
	taken, err := thread.New("th-fork", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, taken); err != nil {
		t.Fatal(err)
	}
	before := sourceRows(t, db, "th-fork")
	past, err := repo.ForkSource(ctx, "th1", 6)
	if err != nil {
		t.Fatal(err)
	}
	f, err := thread.ForkOf(src, 6, past, "th-fork", agentAt(500))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFork(ctx, f); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("SaveFork = %v, want ErrVersionConflict", err)
	}
	if got := sourceRows(t, db, "th-fork"); got != before || f.Thread.Version() != 0 {
		t.Fatalf("rows after the conflict:\n%s\nwas\n%s; version %d", got, before, f.Thread.Version())
	}

	// A fork whose copies do not match its thread is refused before anything is written.
	f.Requests = f.Requests[:1]
	if err := repo.SaveFork(ctx, f); err == nil || errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("SaveFork of mismatched copies = %v", err)
	}
}
