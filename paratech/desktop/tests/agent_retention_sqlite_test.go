package tests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/cases/agentretention"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// historyKinds is a history as the planner gets it: the kind of each turn with its text.
func historyKinds(turns []values.HistoryTurn) []string {
	out := make([]string, len(turns))
	for i, h := range turns {
		switch {
		case h.Summary != "":
			out[i] = "summary: " + h.Summary
		case h.StudentText != "":
			out[i] = "student: " + h.StudentText
		case h.AgentReply != "":
			out[i] = "agent: " + h.AgentReply
		case h.Calls != nil:
			out[i] = fmt.Sprintf("calls: %d", len(h.Calls.Calls))
		default:
			out[i] = fmt.Sprintf("results: %d", len(h.Results))
		}
	}
	return out
}

func hubKinds(req *hubv1.AgentStepRequest) []string {
	out := make([]string, len(req.GetTurns()))
	for i, h := range req.GetTurns() {
		switch {
		case h.GetHistorySummary() != "":
			out[i] = "summary: " + h.GetHistorySummary()
		case h.GetStudentText() != "":
			out[i] = "student: " + h.GetStudentText()
		case h.GetAgentReply() != "":
			out[i] = "agent: " + h.GetAgentReply()
		case h.GetCalls() != nil:
			out[i] = fmt.Sprintf("calls: %d", len(h.GetCalls().GetCalls()))
		default:
			out[i] = fmt.Sprintf("results: %d", len(h.GetResults().GetResults()))
		}
	}
	return out
}

func turnsBefore(entries []values.HistoryEntry, n int) []values.HistoryEntry {
	return slices.DeleteFunc(slices.Clone(entries), func(e values.HistoryEntry) bool { return e.Turn >= n })
}

func turnsOf(entries []values.HistoryEntry, n int) []values.HistoryEntry {
	return slices.DeleteFunc(slices.Clone(entries), func(e values.HistoryEntry) bool { return e.Turn != n })
}

// TASK-250: a chat whose earlier requests ended more than 90 days ago loses their calls and results; what the agent
// gets, the context ring, the compaction and the earlier messages stay the same, and the current request still goes
// on («Продолжить», «Повторить» after a failure, «Повторить» of a finished one, «Скрыть»).
func TestAgentChatAfterTheDetailsPurge(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	repo, history := sqlite.NewAgentThreadRepository(e.db), sqlite.NewAgentHistory(e.db, e.clock)
	chats, err := agentchats.New(agentchats.Deps{Chats: repo, History: history, Clock: e.clock, IDs: e.ids})
	if err != nil {
		t.Fatal(err)
	}
	// «Повторить» of a finished request asks the journal for its effects, as the app wires it.
	if e.turns, err = agentturn.New(agentturn.Deps{
		Model: e.model, Threads: repo, History: history, Subjects: baseSubjects{}, Facts: zeroFacts{}, Tx: e.db,
		Clock: e.clock, IDs: e.ids, Effects: e.journal, Log: slog.New(slog.DiscardHandler),
	}); err != nil {
		t.Fatal(err)
	}

	// 21 earlier requests of different shapes, then a current one stopped offline after its first step.
	var id shared.ThreadID
	ask := func(text string) agentturn.Step {
		t.Helper()
		st, err := e.turns.Ask(ctx, agentturn.AskInput{ThreadID: id, Text: text})
		if err != nil {
			t.Fatalf("Ask %q: %v", text, err)
		}
		id = st.Thread
		return st
	}
	for n := 1; n <= 21; n++ {
		text := fmt.Sprintf("просьба %d", n)
		switch n % 4 {
		case 1: // a tool call, then a reply over two lines
			e.hub.Then(testenv.AgentCall{ID: "l1", Tool: "list_subjects", Args: `{}`}).
				Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: fmt.Sprintf(`{"text":"Строка %d\nвторая"}`, n)})
			st := ask(text)
			e.submit(t, st, agentturn.Outcome{Result: values.ToolResult{CallID: "l1", ResultJSON: `{"subjects":[]}`}})
		case 2:
			e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "cannot_do", Args: `{"your_part":["отправить письмо"],"our_part":["черновик"]}`})
			ask(text)
		case 3: // questions left open: the next request cancels it, with no reply line frozen
			e.hub.Then(testenv.AgentCall{ID: "q1", Tool: "ask_questions",
				Args: `{"questions":[{"kind":"choice","question":"Какая тема?","options":["А","Б"]}]}`})
			ask(text)
		default:
			e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: fmt.Sprintf(`{"text":"Ответ %d"}`, n)})
			ask(text)
		}
	}
	e.hub.Then(testenv.AgentCall{ID: "l2", Tool: "list_subjects", Args: `{}`})
	st := ask("текущая просьба")
	st = e.submit(t, st, agentturn.Outcome{Result: values.ToolResult{CallID: "l2", ResultJSON: `{"subjects":[]}`}})
	if st.Turn != 22 || st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("current request = %+v", st)
	}
	// The oldest requests ended before reply lines were frozen (TASK-266).
	if _, err := e.db.DBTX(ctx).ExecContext(ctx, `UPDATE agent_turns SET reply_line = NULL WHERE number <= 4`); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := e.db.DBTX(ctx).QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM agent_turns WHERE reply_line IS NULL AND number < 22`); n < 6 {
		t.Fatalf("%d earlier requests without a reply line, want the old and the cancelled ones", n)
	}

	snapshot := func() ([]values.HistoryEntry, agentchats.Fill, []agentchats.Request, values.SummaryRequest, bool) {
		t.Helper()
		th, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := history.Since(ctx, id, 1, thread.HistoryReadLimit)
		if err != nil {
			t.Fatal(err)
		}
		fill, err := chats.Fill(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		earlier, err := chats.Earlier(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		req, ok, err := th.CompactionRequest(entries)
		if err != nil {
			t.Fatal(err)
		}
		return entries, fill, earlier, req, ok
	}
	before, fillBefore, earlierBefore, compBefore, compOK := snapshot()
	texts := count(`SELECT count(*) FROM agent_steps WHERE entry = 'student'`)

	// 89 days later nothing goes; 91 days later the details of requests 1..21 go, those of the current one stay.
	purger, err := agentretention.New(agentretention.Deps{Requests: sqlite.NewAgentRetention(e.db), Tx: e.db, Clock: e.clock})
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(89 * 24 * time.Hour)
	if n, err := purger.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("Purge at 89 days = %d, %v", n, err)
	}
	e.clock.Advance(2 * 24 * time.Hour)
	if n, err := purger.Purge(ctx); err != nil || n != 21 {
		t.Fatalf("Purge at 91 days = %d, %v", n, err)
	}
	if n, err := purger.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("second Purge = %d, %v", n, err)
	}
	if n := count(`SELECT count(*) FROM agent_steps WHERE entry = 'student'`); n != texts {
		t.Fatalf("texts = %d, want all %d", n, texts)
	}
	if n := count(`SELECT count(*) FROM agent_steps WHERE entry <> 'student' AND turn <> 22`); n != 0 {
		t.Fatalf("%d details of earlier requests left", n)
	}
	if n := count(`SELECT count(*) FROM agent_steps WHERE entry <> 'student' AND turn = 22`); n != 2 {
		t.Fatalf("the current request has %d details, want its echo and results", n)
	}
	if n := count(`SELECT count(*) FROM agent_turns WHERE reply_line IS NULL AND number < 22`); n != 0 {
		t.Fatalf("%d earlier requests without a frozen reply line", n)
	}

	after, fillAfter, earlierAfter, compAfter, compOKAfter := snapshot()
	if len(after) >= len(before) {
		t.Fatal("the history did not shrink")
	}
	next := values.HistoryEntry{Turn: 23, Entry: values.HistoryTurn{StudentText: "следующая"}}
	for _, current := range []int{22, 23} {
		b, a := thread.Window(append(slices.Clone(before), next), current, values.HistorySummary{}),
			thread.Window(append(slices.Clone(after), next), current, values.HistorySummary{})
		if !slices.Equal(historyKinds(a), historyKinds(b)) {
			t.Fatalf("window of %d after the purge:\n%q\nbefore:\n%q", current, historyKinds(a), historyKinds(b))
		}
	}
	if fillAfter != fillBefore {
		t.Fatalf("fill = %+v, before %+v", fillAfter, fillBefore)
	}
	if !compOK || !compOKAfter || !compAfter.Equal(compBefore) {
		t.Fatalf("compaction after the purge = %+v (%v), before %+v (%v)", compAfter, compOKAfter, compBefore, compOK)
	}
	sameEarlier := func(got, want []agentchats.Request) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("earlier = %d requests, want %d", len(got), len(want))
		}
		for i := range want {
			// A purged reply keeps its card on one line.
			w := want[i]
			w.Reply = strings.ReplaceAll(w.Reply, "\nвторая", " вторая")
			if got[i].Turn != w.Turn || got[i].Text != w.Text || got[i].Reply != w.Reply {
				t.Errorf("earlier %d = %+v, want %+v", i, got[i], w)
			}
		}
	}
	sameEarlier(earlierAfter, earlierBefore)
	if earlierAfter[0].Reply != "Строка 1 вторая" || earlierAfter[1].Reply == "" || earlierAfter[2].Reply != "Были уточняющие вопросы." {
		t.Fatalf("earlier replies = %q, %q, %q", earlierAfter[0].Reply, earlierAfter[1].Reply, earlierAfter[2].Reply)
	}

	// «Повторить» of the current request (stopped offline: «Продолжить» is refused since ADR-0026 addendum 2026-11-08)
	// sends what it sent before the purge; a provider failure, then «Повторить» again.
	want22 := historyKinds(thread.Window(before, 22, values.HistorySummary{}))
	sent := len(e.hub.Requests())
	e.hub.ThenProviderFails()
	if _, err := e.turns.Continue(ctx, turnOf(st)); !errors.Is(err, thread.ErrRetryRequired) {
		t.Fatalf("Continue = %v, want retry required", err)
	}
	if st, err = e.turns.Retry(ctx, turnOf(st)); err != nil || st.StopReason != values.StopFailed {
		t.Fatalf("first Retry = %+v, %v", st, err)
	}
	e.hub.Then(testenv.AgentCall{ID: "r2", Tool: "reply", Args: `{"text":"Предметов нет."}`})
	if st, err = e.turns.Retry(ctx, turnOf(st)); err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Retry = %+v, %v", st, err)
	}
	reqs := e.hub.Requests()
	if len(reqs) != sent+2 {
		t.Fatalf("hub got %d requests, want 2", len(reqs)-sent)
	}
	for _, r := range reqs[sent:] {
		if got := hubKinds(r); !slices.Equal(got, want22) {
			t.Fatalf("resumed step sent\n%q\nwant\n%q", got, want22)
		}
	}

	// «Повторить» of the finished request: a new request with its text and the same earlier history.
	stored, err := history.Since(ctx, id, 22, thread.HistoryReadLimit)
	if err != nil {
		t.Fatal(err)
	}
	rerun := values.HistoryEntry{Turn: 23, Entry: values.HistoryTurn{StudentText: "текущая просьба"}}
	want23 := historyKinds(thread.Window(append(append(turnsBefore(before, 22), turnsOf(stored, 22)...), rerun), 23, values.HistorySummary{}))
	e.hub.Then(testenv.AgentCall{ID: "r3", Tool: "reply", Args: `{"text":"Снова нет."}`})
	if st, err = e.turns.Rerun(ctx, turnOf(st)); err != nil || st.Turn != 23 || st.Status != thread.StatusDone {
		t.Fatalf("Rerun = %+v, %v", st, err)
	}
	if got := hubKinds(e.hub.Requests()[len(e.hub.Requests())-1]); !slices.Equal(got, want23) {
		t.Fatalf("rerun sent\n%q\nwant\n%q", got, want23)
	}

	// «Скрыть» from request 20 (purged): requests 20..23 leave the view and the agent's history.
	restart := values.HistoryEntry{Turn: 24, Entry: values.HistoryTurn{StudentText: "заново"}}
	want24 := historyKinds(thread.Window(append(turnsBefore(before, 20), restart), 24, values.HistorySummary{}))
	e.hub.Then(testenv.AgentCall{ID: "r4", Tool: "reply", Args: `{"text":"Начнём."}`})
	if st, err = e.turns.Ask(ctx, agentturn.AskInput{ThreadID: id, Text: "заново", RestartFrom: 20}); err != nil || st.Turn != 24 {
		t.Fatalf("restart = %+v, %v", st, err)
	}
	if got := hubKinds(e.hub.Requests()[len(e.hub.Requests())-1]); !slices.Equal(got, want24) {
		t.Fatalf("restart sent\n%q\nwant\n%q", got, want24)
	}
	earlier, err := chats.Earlier(ctx, id)
	if err != nil || len(earlier) != 20 || earlier[19].Turn != 24 {
		t.Fatalf("earlier after «Скрыть» = %+v, %v", earlier, err)
	}
	sameEarlier(earlier[:19], earlierBefore[:19])
}
