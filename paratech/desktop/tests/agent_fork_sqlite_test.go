package tests

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

type forkClock struct{ now time.Time }

func (c *forkClock) Now() time.Time {
	c.now = c.now.Add(time.Minute)
	return c.now
}

type forkIDs struct{}

func (forkIDs) NewThreadID() shared.ThreadID { return "th-fork" }

// «Форк» on the real store (TASK-271): the new chat shows the earlier requests before n with their texts and
// mentions, n's text comes back for the input without its upload line, a hidden request is not there, and the
// source chat still shows what it showed.
func TestAgentForkShowsTheEarlierRequests(t *testing.T) {
	ctx := context.Background()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(t.TempDir(), "fork.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clock := &forkClock{now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	repo, history := sqlite.NewAgentThreadRepository(db), sqlite.NewAgentHistory(db, clock)

	lesson, err := values.NewRef(values.KindLesson, "l1")
	if err != nil {
		t.Fatal(err)
	}
	m, err := values.NewMention(lesson, "Лекция 1")
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th1", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range []thread.Request{
		{Text: "первая", Mentions: []values.Mention{m}}, {Text: "скрытая"}, {Text: "третья"}, {Text: "четвёртая\n[upload:u1]"},
	} {
		turn := i + 1
		if turn == 3 {
			err = th.RestartFrom(2, r, clock.Now()) // «Скрыть» request 2
		} else {
			err = th.Ask(r, clock.Now())
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
		entries := []values.HistoryTurn{
			{StudentText: r.Text},
			{Calls: &values.SealedCalls{Seal: []byte{1}, Calls: []values.RawCall{{CallID: "r1", Tool: "reply", ArgumentsJSON: `{"text":"ответ"}`}}}},
			{Results: []values.ToolResult{values.ShownResult("r1")}},
		}
		var stored []values.HistoryEntry
		for _, e := range entries {
			if err := history.Append(ctx, "th1", turn, e); err != nil {
				t.Fatal(err)
			}
			stored = append(stored, values.HistoryEntry{Turn: turn, Entry: e})
		}
		if err := th.Complete(clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := th.FreezeReply(stored, clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
	}

	chats, err := agentchats.New(agentchats.Deps{Chats: repo, History: history, Clock: clock, IDs: forkIDs{}})
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := chats.Earlier(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := chats.Fork(ctx, "th1", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "четвёртая" || got.Chat.ID != "th-fork" || got.Chat.Title != "первая (fork)" {
		t.Fatalf("Fork = %+v", got)
	}
	if cur, err := repo.Current(ctx); err != nil || cur.ID() != "th-fork" {
		t.Fatalf("current chat = %v, %v", cur, err)
	}

	earlier, err := chats.Earlier(ctx, "th-fork")
	if err != nil {
		t.Fatal(err)
	}
	// The reply of a copy is its frozen line in the planner's form; on this branch Earlier still builds the reply
	// from the steps, which a fork does not copy, so it is empty (TASK-250 shows the frozen line).
	if len(earlier) != 2 || earlier[0].Turn != 1 || earlier[0].Text != "первая" || !reflect.DeepEqual(earlier[0].Mentions, []values.Mention{m}) ||
		earlier[1].Turn != 3 || earlier[1].Text != "третья" || !earlier[1].Current {
		t.Fatalf("Earlier of the fork = %+v", earlier)
	}
	entries, err := history.Since(ctx, "th-fork", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ReplyLine != "reply: ответ" || e.Entry.StudentText == "" {
			t.Fatalf("fork entry = %+v", e)
		}
	}
	sourceAfter, err := chats.Earlier(ctx, "th1")
	if err != nil || !reflect.DeepEqual(sourceAfter, sourceBefore) {
		t.Fatalf("source shows %+v, was %+v (%v)", sourceAfter, sourceBefore, err)
	}
}
