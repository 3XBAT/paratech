package tests

import (
	"context"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/testenv"
)

// The reply line of a request is stored when it ends (TASK-266): after the app starts again on the same database the
// line is there, and the next request of the chat sends it as the agent reply of the earlier one.
func TestAgentReplyLineSurvivesARestart(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := openSQLite(ctx, t, sqlite.Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	e.db = db
	e.start(t)

	e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Привет!"}`}).
		Then(testenv.AgentCall{ID: "r2", Tool: "reply", Args: `{"text":"Ещё раз привет."}`})
	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "привет"})
	if err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Ask = %+v, %v", st, err)
	}

	// The app stops and starts again on the same file.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if e.db, err = openSQLite(ctx, t, sqlite.Options{Path: path, Migrate: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.db.Close() })
	e.start(t)

	th, err := sqlite.NewAgentThreadRepository(e.db).Get(ctx, st.Thread)
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if turn.ReplyLine() != "reply: Привет!" {
		t.Fatalf("stored reply line = %q", turn.ReplyLine())
	}
	if _, err := e.turns.Ask(ctx, agentturn.AskInput{ThreadID: st.Thread, Text: "ещё"}); err != nil {
		t.Fatal(err)
	}
	reqs := e.hub.Requests()
	turns := reqs[len(reqs)-1].GetTurns()
	if len(turns) != 3 || turns[0].GetStudentText() != "привет" || turns[1].GetAgentReply() != "reply: Привет!" ||
		turns[2].GetStudentText() != "ещё" {
		t.Fatalf("turns of the next request = %v", turns)
	}
}
