package tests

// Scenario tests of the agent core (TASK-116, ADR-0021, ADR-0023) on the real SQLite, the real Hub client and the
// real use cases; only the Hub is a fake that speaks the Connect contract. The test plays the Driver: it runs the
// calls a step returns through the journal and reports the results.

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const agentTestKey = "la_test_0123456789abcdef"

type agentIDs struct{ threads, actions int }

func (g *agentIDs) NewThreadID() shared.ThreadID {
	g.threads++
	return shared.ThreadID(fmt.Sprintf("th%d", g.threads))
}

func (g *agentIDs) NewActionID() shared.ActionID {
	g.actions++
	return shared.ActionID(fmt.Sprintf("act%d", g.actions))
}

type baseSubjects []values.BaseSubject

func (b baseSubjects) BaseSubjects(context.Context) ([]values.BaseSubject, error) { return b, nil }

// zeroFacts knows nothing of a call: the cost is unknown (a paid write needs a plan), no ready result, no triage.
type zeroFacts struct{}

func (zeroFacts) Facts(context.Context, values.Call) (thread.Facts, error) {
	return thread.Facts{}, nil
}

type agentEnv struct {
	db      *sqlite.DB
	hub     *testenv.FakeAgentHub
	model   *hubclient.Client
	clock   *testenv.FakeClock
	ids     *agentIDs
	turns   *agentturn.Service
	journal *agentjournal.Service
}

func newAgentEnv(t *testing.T) *agentEnv {
	t.Helper()
	ctx := context.Background()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(t.TempDir(), "agent.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := &agentEnv{db: db, hub: testenv.NewFakeAgentHub(), clock: testenv.NewFakeClock(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)), ids: &agentIDs{}}
	url, client := testenv.StartFakeAgentHub(t, e.hub)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(ctx, hubclient.DefaultCredentialName, agentTestKey); err != nil {
		t.Fatal(err)
	}
	if e.model, err = hubclient.New(hubclient.Config{BaseURL: url, HTTPClient: client, RequestTimeout: 5 * time.Second}, creds); err != nil {
		t.Fatal(err)
	}
	e.start(t)
	return e
}

// start builds the use cases on the stored state, as the app does at every start.
func (e *agentEnv) start(t *testing.T) {
	t.Helper()
	s1, err := values.NewRef(values.KindSubject, "s1")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	if e.turns, err = agentturn.New(agentturn.Deps{
		Model: e.model, Threads: sqlite.NewAgentThreadRepository(e.db), History: sqlite.NewAgentHistory(e.db, e.clock),
		Subjects: baseSubjects{{ID: s1, Name: "Физика"}}, Facts: zeroFacts{}, Tx: e.db, Clock: e.clock,
		IDs: e.ids, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	if e.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: sqlite.NewAgentActionRepository(e.db), Clock: e.clock, IDs: e.ids, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
}

func agentRef(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func turnOf(st agentturn.Step) agentturn.TurnRef {
	return agentturn.TurnRef{Thread: st.Thread, Turn: st.Turn}
}

// runWrite plays the Driver for one U call: journal entry, the "use case" (here: the id it would create), outcome,
// and the result for the planner.
func (e *agentEnv) runWrite(t *testing.T, st agentturn.Step, c agentturn.Call, created values.Ref) agentturn.Outcome {
	t.Helper()
	ctx := context.Background()
	if c.Class != actionpolicy.ClassU {
		t.Fatalf("call %s is %s, want U", c.Call.Tool(), c.Class)
	}
	entry, err := e.journal.Begin(ctx, agentjournal.BeginInput{Thread: st.Thread, Turn: st.Turn, CallID: c.Call.ID(), Tool: c.Call.Tool()})
	if err != nil {
		t.Fatal(err)
	}
	var comp action.Compensation
	switch created.Kind() {
	case values.KindSubject:
		comp = action.RemoveSubject{Subject: created}
	case values.KindFolder:
		subject, _ := c.Call.Args().ID("subject")
		comp = action.RemoveFolder{Subject: subject, Folder: created}
	case values.KindMaterial:
		comp = action.ReturnToInbox{Material: created}
	}
	if _, err := e.journal.Finish(ctx, entry.ID, agentjournal.Outcome{Compensation: comp, TargetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	out := agentturn.Outcome{
		Result: values.ToolResult{CallID: c.Call.ID(), ResultJSON: `{"id":"` + created.ID() + `"}`}, Seen: []values.Ref{created},
	}
	if created.Kind() == values.KindSubject || created.Kind() == values.KindFolder {
		out.Created = created
	}
	return out
}

func (e *agentEnv) submit(t *testing.T, st agentturn.Step, outs ...agentturn.Outcome) agentturn.Step {
	t.Helper()
	next, err := e.turns.Submit(context.Background(), agentturn.SubmitInput{Turn: turnOf(st), Outcomes: outs})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return next
}

func lastTurn(t *testing.T, req *hubv1.AgentStepRequest) *hubv1.AgentTurn {
	t.Helper()
	turns := req.GetTurns()
	if len(turns) == 0 {
		t.Fatal("request without turns")
	}
	return turns[len(turns)-1]
}

// Ask → step → tool result → second step, with the action journaled and the history stored in SQLite.
func TestAgentAskResultAndSecondStep(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "create_subject", Args: `{"name":"Химия"}`}).
		Then(testenv.AgentCall{ID: "toolu_2", Tool: "reply", Args: `{"text":"Предмет создан"}`})

	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "добавь предмет химия"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if st.Status != thread.StatusExecuting || len(st.Calls) != 1 {
		t.Fatalf("first step = %+v", st)
	}
	st = e.submit(t, st, e.runWrite(t, st, st.Calls[0], agentRef(t, values.KindSubject, "s2")))
	if st.Status != thread.StatusDone || len(st.Cards) != 1 || st.Cards[0].Kind() != values.CardReply {
		t.Fatalf("second step = %+v", st)
	}

	reqs := e.hub.Requests()
	if len(reqs) != 2 || reqs[0].GetIdempotencyKey() != "ag:th1:1:1" || reqs[1].GetIdempotencyKey() != "ag:th1:1:2" {
		t.Fatalf("hub saw %d requests", len(reqs))
	}
	if reqs[1].GetBase().GetToday() != "2026-10-22" || reqs[1].GetBase().GetSubjects()[0].GetName() != "Физика" ||
		reqs[1].GetScope().GetSubjectId() != "" || len(reqs[1].GetTurns()) != 3 {
		t.Fatalf("second request = %v", reqs[1])
	}
	if res := lastTurn(t, reqs[1]).GetResults().GetResults(); len(res) != 1 || res[0].GetResultJson() != `{"id":"s2"}` {
		t.Fatalf("results sent = %v", res)
	}
	entries, err := e.journal.List(ctx, st.Thread, 10)
	if err != nil || len(entries) != 1 || entries[0].Status != action.StatusDone || !entries[0].CanUndo {
		t.Fatalf("journal = %+v, %v", entries, err)
	}
}

// Plan approval: «Да» lets the plan run; the deferred folder binds to the folder the plan's create step made.
func TestAgentPlanApproval(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	plan := `{"summary":"Разложить файл","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}},` +
		`{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1"}}]}`
	e.hub.Then(testenv.AgentCall{ID: "toolu_p", Tool: "show_plan", Args: plan}).
		Then(testenv.AgentCall{ID: "toolu_w1", Tool: "create_folder", Args: `{"subject":"s1","name":"Лабы"}`}).
		Then(testenv.AgentCall{ID: "toolu_w2", Tool: "place_upload", Args: `{"upload":"u1","subject":"s1","folder":"f1"}`}).
		Then(testenv.AgentCall{ID: "toolu_r", Tool: "reply", Args: `{"text":"Готово"}`})

	scope, err := values.NewScope(agentRef(t, values.KindSubject, "s1"), values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := e.turns.Ask(ctx, agentturn.AskInput{Scope: scope, Text: "положи в папку Лабы", Uploads: []values.Ref{agentRef(t, values.KindUpload, "u1")}})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != thread.StatusAwaitingPlan || st.Cards[0].Kind() != values.CardPlan {
		t.Fatalf("plan step = %+v", st)
	}
	if st, err = e.turns.ApprovePlan(ctx, turnOf(st)); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if len(st.Calls) != 1 || st.Calls[0].PlanStep != 0 {
		t.Fatalf("first approved step = %+v", st.Calls)
	}
	st = e.submit(t, st, e.runWrite(t, st, st.Calls[0], agentRef(t, values.KindFolder, "f1")))
	if len(st.Calls) != 1 || st.Calls[0].PlanStep != 1 || st.Calls[0].Call.Tool() != values.ToolPlaceUpload {
		t.Fatalf("second approved step = %+v", st.Calls)
	}
	st = e.submit(t, st, e.runWrite(t, st, st.Calls[0], agentRef(t, values.KindMaterial, "m1")))
	if st.Status != thread.StatusDone {
		t.Fatalf("after the plan = %+v", st)
	}
	reqs := e.hub.Requests()
	if got := reqs[1].GetTurns()[2].GetResults().GetResults()[0].GetResultJson(); got != `{"approved":true}` {
		t.Fatalf("plan result = %s", got)
	}
	if got := reqs[0].GetTurns()[0].GetStudentText(); !strings.Contains(got, "upload:u1") || reqs[0].GetScope().GetSubjectId() != "s1" {
		t.Fatalf("first request = %v", reqs[0])
	}
}

// A third question round is refused; the planner gets the note and goes on with stated assumptions.
func TestAgentThirdQuestionRound(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	q := `{"questions":[{"kind":"text","question":"Объём работы?"}]}`
	e.hub.Then(testenv.AgentCall{ID: "q1", Tool: "ask_questions", Args: q}).
		Then(testenv.AgentCall{ID: "q2", Tool: "ask_questions", Args: q}).
		Then(testenv.AgentCall{ID: "q3", Tool: "ask_questions", Args: q}).
		Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Принято: 10 страниц"}`})

	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "напиши доклад"})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round <= 2; round++ {
		if st.Status != thread.StatusAwaitingAnswers {
			t.Fatalf("round %d = %+v", round, st)
		}
		if st, err = e.turns.AnswerQuestions(ctx, turnOf(st), []values.Answer{{Question: 0, Text: "10 страниц"}}); err != nil {
			t.Fatal(err)
		}
	}
	if st.Status != thread.StatusDone {
		t.Fatalf("after the third round = %+v", st)
	}
	reqs := e.hub.Requests()
	res := lastTurn(t, reqs[len(reqs)-1]).GetResults().GetResults()
	if len(res) != 1 || !res[0].GetIsError() || !strings.Contains(res[0].GetResultJson(), "continue_with_stated_assumptions") {
		t.Fatalf("third round result = %v", res)
	}
}

// A restart with a call running: the request stops as «нет связи», the journal entry becomes UNKNOWN, and
// «Продолжить» tells the planner the outcome is unknown.
func TestAgentRecoverAfterRestart(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "create_subject", Args: `{"name":"Химия"}`}).
		Then(testenv.AgentCall{ID: "toolu_2", Tool: "reply", Args: `{"text":"Проверьте список предметов"}`})
	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "добавь химию"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := e.journal.Begin(ctx, agentjournal.BeginInput{Thread: st.Thread, Turn: st.Turn, CallID: st.Calls[0].Call.ID(), Tool: st.Calls[0].Call.Tool()})
	if err != nil {
		t.Fatal(err)
	}

	// The app stops here and starts again on the same database.
	e.start(t)
	if n, err := e.turns.Recover(ctx); err != nil || n != 1 {
		t.Fatalf("agent Recover = %d, %v", n, err)
	}
	if rep, err := e.journal.Recover(ctx); err != nil || rep.Unknown != 1 {
		t.Fatalf("journal Recover = %+v, %v", rep, err)
	}
	entries, _ := e.journal.List(ctx, st.Thread, 10)
	if len(entries) != 1 || entries[0].ID != entry.ID || entries[0].Status != action.StatusUnknown || entries[0].CanUndo {
		t.Fatalf("journal after restart = %+v", entries)
	}
	if _, err := e.turns.Submit(ctx, agentturn.SubmitInput{Turn: turnOf(st), Outcomes: []agentturn.Outcome{
		{Result: values.ToolResult{CallID: "toolu_1", ResultJSON: `{}`}},
	}}); err == nil {
		t.Fatal("a late result of the interrupted call was accepted")
	}
	st, err = e.turns.Continue(ctx, turnOf(st))
	if err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	res := lastTurn(t, e.hub.Requests()[1]).GetResults().GetResults()
	if res[0].GetResultJson() != `{"error":"outcome_unknown"}` {
		t.Fatalf("result after restart = %v", res)
	}
}

// «Продолжить» after a lost answer repeats the key and is replayed for free; «Повторить» after a refusal sends a new
// key; an expired model turn starts the request again from its text.
func TestAgentKeysOnContinueRetryAndRestart(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	e.hub.ThenLost(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"первый ответ"}`})

	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "привет"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("lost answer = %+v", st)
	}
	if st, err = e.turns.Continue(ctx, turnOf(st)); err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	reqs := e.hub.Requests()
	if len(reqs) != 2 || reqs[0].GetIdempotencyKey() != reqs[1].GetIdempotencyKey() || e.hub.Executed() != 1 {
		t.Fatalf("keys %v, executed %d", reqs, e.hub.Executed())
	}

	e.hub.ThenProviderFails().Then(testenv.AgentCall{ID: "r2", Tool: "reply", Args: `{"text":"второй ответ"}`})
	st, err = e.turns.Ask(ctx, agentturn.AskInput{Text: "ещё вопрос"})
	if err != nil || st.StopReason != values.StopFailed {
		t.Fatalf("provider failure = %+v, %v", st, err)
	}
	if _, err := e.turns.Continue(ctx, turnOf(st)); err == nil {
		t.Fatal("Continue after a failure repeated the failed key")
	}
	if st, err = e.turns.Retry(ctx, turnOf(st)); err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Retry = %+v, %v", st, err)
	}
	reqs = e.hub.Requests()
	if got := []string{reqs[2].GetIdempotencyKey(), reqs[3].GetIdempotencyKey()}; got[0] != "ag:th1:2:1" || got[1] != "ag:th1:2:1:r1" {
		t.Fatalf("keys = %v", got)
	}

	e.hub.ThenTurnGone().Then(testenv.AgentCall{ID: "r3", Tool: "reply", Args: `{"text":"третий ответ"}`})
	st, err = e.turns.Ask(ctx, agentturn.AskInput{Text: "третий вопрос"})
	if err != nil || st.Status != thread.StatusDone || st.Turn != 4 {
		t.Fatalf("restart after the turn expired = %+v, %v", st, err)
	}
	reqs = e.hub.Requests()
	// Earlier requests go as texts only, so the Hub never needs the model turn of an earlier echo.
	for _, turn := range reqs[len(reqs)-2].GetTurns() {
		if turn.GetStudentText() == "" {
			t.Fatalf("the expired attempt sent an earlier echo: %v", reqs[len(reqs)-2])
		}
	}
	last := reqs[len(reqs)-1]
	if len(last.GetTurns()) != 1 || last.GetTurns()[0].GetStudentText() != "третий вопрос" || last.GetIdempotencyKey() != "ag:th1:4:1" {
		t.Fatalf("restarted request = %v", last)
	}
}
