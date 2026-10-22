package agentturn

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

func mustRef(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func subjectScope(t *testing.T) values.Scope {
	t.Helper()
	sc, err := values.NewScope(mustRef(t, values.KindSubject, "s1"), values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func ask(t *testing.T, e *env, text string) Step {
	t.Helper()
	st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: text})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	return st
}

func ref(st Step) TurnRef { return TurnRef{Thread: st.Thread, Turn: st.Turn} }

func okResult(id, body string) Outcome {
	return Outcome{Result: values.ToolResult{CallID: id, ResultJSON: body}}
}

// lastResults returns the results the planner got in its last request.
func lastResults(t *testing.T, e *env) []values.ToolResult {
	t.Helper()
	h := e.model.last(t).History
	if len(h) == 0 || len(h[len(h)-1].Results) == 0 {
		t.Fatalf("last history entry is not results: %+v", h)
	}
	return h[len(h)-1].Results
}

func TestNewNeedsPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without ports = %v", err)
	}
}

// Ask → step → tool result → second step: the request, the key and the history the planner sees.
func TestAskSubmitAndSecondStep(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "find_lessons", `{"subject":"s1"}`)).then(raw("c2", "reply", `{"text":"Найдено 2 пары"}`))

	st := ask(t, e, "какие пары по физике")
	if st.Status != thread.StatusPlanning || len(st.Calls) != 1 || st.Calls[0].Class != actionpolicy.ClassR {
		t.Fatalf("first step = %+v", st)
	}
	req := e.model.last(t)
	if req.Key != "ag:th1:1:1" || req.Thread != "th1" || req.MaxOutputTokens != MaxOutputTokens {
		t.Fatalf("request = %q %q %d", req.Key, req.Thread, req.MaxOutputTokens)
	}
	if req.Base.Today().String() != "2026-10-22" || req.Base.TimeZone() != "Europe/Moscow" || len(req.Base.Subjects()) != 1 {
		t.Fatalf("base = %+v", req.Base)
	}
	if req.Scope.Subject().ID() != "s1" || len(req.History) != 1 || req.History[0].StudentText != "какие пары по физике" {
		t.Fatalf("scope or history = %+v %+v", req.Scope, req.History)
	}

	st, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{{
		Result: values.ToolResult{CallID: "c1", ResultJSON: `{"lessons":["l7"]}`},
		Seen:   []values.Ref{mustRef(t, values.KindLesson, "l7")},
	}}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if st.Status != thread.StatusDone || len(st.Cards) != 1 || st.Cards[0].Kind() != values.CardReply {
		t.Fatalf("second step = %+v", st)
	}
	req = e.model.last(t)
	if req.Key != "ag:th1:1:2" || len(req.History) != 3 || req.History[1].Calls == nil {
		t.Fatalf("second request = %q, %d entries", req.Key, len(req.History))
	}
	if got := lastResults(t, e); got[0].ResultJSON != `{"lessons":["l7"]}` {
		t.Fatalf("results sent = %+v", got)
	}
	// The reply is answered too, so the history stays echo → results for the next request.
	if all := e.history.all("th1"); len(all) != 5 || len(all[4].Entry.Results) != 1 {
		t.Fatalf("stored history = %d entries", len(all))
	}
	th, _ := e.threads.Get(t.Context(), "th1")
	if !th.Seen(mustRef(t, values.KindLesson, "l7")) {
		t.Fatal("ids of the result are not known to provenance")
	}
}

func TestSubmitChecks(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_subjects", `{}`), raw("c2", "zzz", `{}`))
	st := ask(t, e, "что есть")
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: TurnRef{Thread: st.Thread, Turn: 9}}); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("submit to an old request = %v", err)
	}
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c2", `{}`)}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("submit for a refused call = %v", err)
	}
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{`)}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("submit of broken JSON = %v", err)
	}
	if _, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "ещё"}); !errors.Is(err, thread.ErrTurnInProgress) {
		t.Fatalf("Ask while calls run = %v", err)
	}
}

// Refused calls are answered by the case and the planner is asked again at once.
func TestRefusalsGoBackToThePlanner(t *testing.T) {
	e := newEnv(t)
	e.model.then(
		raw("c1", "send_email", `{}`),
		raw("c2", "find_lessons", `{"subject":"s999"}`),
		raw("c3", "find_lessons", `{"kind":"party"}`),
	).then(raw("c4", "reply", `{"text":"Готово"}`))

	st := ask(t, e, "сделай")
	if st.Status != thread.StatusDone || len(st.Calls) != 0 {
		t.Fatalf("step = %+v", st)
	}
	got := lastResults(t, e)
	want := []string{`{"error":"unknown_tool"}`, `{"error":"unknown_id"}`, `{"error":"invalid_arguments"}`}
	for i, w := range want {
		if !got[i].IsError || got[i].ResultJSON != w {
			t.Errorf("result %d = %+v, want %s", i, got[i], w)
		}
	}
}

// Plan approval with deferred IDs: the create step's ID is bound, another known ID is refused (decision a).
func TestPlanApprovalAndCreatedIDs(t *testing.T) {
	e := newEnv(t)
	plan := `{"summary":"Создать папку","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}},` +
		`{"tool":"create_folder","arguments":{"subject":"s1","name":"Лекции"}}]}`
	e.model.then(raw("p1", "show_plan", plan)).
		then(raw("w1", "create_folder", `{"subject":"s1","name":"Лабы"}`)).
		then(raw("w2", "create_folder", `{"subject":"s1","name":"Чужая"}`)).
		then(raw("r1", "reply", `{"text":"Папка создана"}`))

	st := ask(t, e, "создай папки")
	if st.Status != thread.StatusAwaitingPlan || len(st.Cards) != 1 || st.Cards[0].Kind() != values.CardPlan {
		t.Fatalf("plan step = %+v", st)
	}
	if _, err := e.svc.AnswerQuestions(t.Context(), ref(st), []values.Answer{{Question: 0}}); !errors.Is(err, thread.ErrInvalidTransition) {
		t.Fatalf("answers without questions = %v", err)
	}

	st, err := e.svc.ApprovePlan(t.Context(), ref(st))
	if err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if got := lastResults(t, e); got[0].ResultJSON != `{"approved":true}` {
		t.Fatalf("plan result = %+v", got)
	}
	if len(st.Calls) != 1 || st.Calls[0].PlanStep != 0 || st.Calls[0].Class != actionpolicy.ClassU {
		t.Fatalf("approved write = %+v", st.Calls)
	}

	st, err = e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{{
		Result: values.ToolResult{CallID: "w1", ResultJSON: `{"folder":"f1"}`}, Created: mustRef(t, values.KindFolder, "f1"),
	}}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// "Чужая" is not a step of the plan: refused, and the planner replies.
	if got := lastResults(t, e); got[0].ResultJSON != `{"error":"not_in_plan"}` {
		t.Fatalf("write outside the plan = %+v", got)
	}
	if st.Status != thread.StatusDone {
		t.Fatalf("status = %s", st.Status)
	}
}

func TestRevisePlan(t *testing.T) {
	e := newEnv(t)
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	e.model.then(raw("p1", "show_plan", plan)).then(raw("r1", "reply", `{"text":"Хорошо"}`))
	st := ask(t, e, "добавь предмет")
	if _, err := e.svc.RevisePlan(t.Context(), ref(st), "<b>"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("revise with HTML = %v", err)
	}
	if _, err := e.svc.RevisePlan(t.Context(), ref(st), "назови Органика"); err != nil {
		t.Fatal(err)
	}
	if got := lastResults(t, e); got[0].ResultJSON != `{"approved":false,"change":"назови Органика"}` {
		t.Fatalf("revise result = %+v", got)
	}
}

// Decision (c): a third question round is refused and the planner continues with stated assumptions.
func TestThirdQuestionRound(t *testing.T) {
	e := newEnv(t)
	q := `{"questions":[{"kind":"choice","question":"Какой вариант?","options":["А","Б"],"default":0}]}`
	e.model.then(raw("q1", "ask_questions", q)).then(raw("q2", "ask_questions", q)).
		then(raw("q3", "ask_questions", q)).then(raw("r1", "reply", `{"text":"Сделано с допущениями"}`))

	st := ask(t, e, "напиши реферат")
	for round := 1; round <= 2; round++ {
		card, ok := st.Cards[0].(values.QuestionsCard)
		if st.Status != thread.StatusAwaitingAnswers || !ok || card.Round != round {
			t.Fatalf("round %d = %+v", round, st)
		}
		var err error
		st, err = e.svc.AnswerQuestions(t.Context(), ref(st), []values.Answer{{Question: 0, Option: 1}})
		if err != nil {
			t.Fatalf("answer round %d: %v", round, err)
		}
	}
	if st.Status != thread.StatusDone || st.Cards[0].Kind() != values.CardReply {
		t.Fatalf("after the third round = %+v", st)
	}
	got := lastResults(t, e)
	if !got[0].IsError || !strings.Contains(got[0].ResultJSON, "continue_with_stated_assumptions") {
		t.Fatalf("third round result = %+v", got)
	}
	reqs := e.model.requests()
	if a := reqs[1].History[len(reqs[1].History)-1].Results[0].ResultJSON; a != `{"answers":[{"question":0,"option":1}]}` {
		t.Fatalf("answers sent = %s", a)
	}
}

// Decision (d): «Продолжить» after a lost answer repeats the key; «Повторить» after a refusal uses a new one.
func TestContinueAndRetryKeys(t *testing.T) {
	e := newEnv(t)
	e.model.fail(backoff.Transient(shared.ErrNetworkUnavailable)).
		then(raw("c1", "list_subjects", `{}`))

	st := ask(t, e, "мои предметы")
	if st.Status != thread.StatusStopped || st.StopReason != values.StopOffline || st.Cards[0].Kind() != values.CardStopped {
		t.Fatalf("offline step = %+v", st)
	}
	st, err := e.svc.Continue(t.Context(), ref(st))
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	reqs := e.model.requests()
	if reqs[0].Key != reqs[1].Key || len(st.Calls) != 1 {
		t.Fatalf("keys %q %q, step %+v", reqs[0].Key, reqs[1].Key, st)
	}

	e2 := newEnv(t)
	e2.model.fail(backoff.Permanent(errors.New("provider refused"))).then(raw("c1", "list_subjects", `{}`))
	st = ask(t, e2, "мои предметы")
	if st.StopReason != values.StopFailed {
		t.Fatalf("permanent failure = %+v", st)
	}
	if _, err := e2.svc.Continue(t.Context(), ref(st)); !errors.Is(err, thread.ErrRetryRequired) {
		t.Fatalf("Continue after a permanent failure = %v", err)
	}
	if _, err := e2.svc.Retry(t.Context(), ref(st)); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	reqs = e2.model.requests()
	if reqs[0].Key != "ag:th1:1:1" || reqs[1].Key != "ag:th1:1:1:r1" {
		t.Fatalf("keys = %q, %q", reqs[0].Key, reqs[1].Key)
	}
}

func TestQuotaStopContinues(t *testing.T) {
	e := newEnv(t)
	e.model.fail(shared.ErrQuotaExceeded).then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "привет")
	if st.StopReason != values.StopQuota {
		t.Fatalf("quota stop = %+v", st)
	}
	if st, err := e.svc.Continue(t.Context(), ref(st)); err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Continue after quota = %+v, %v", st, err)
	}
}

// A malformed reply is a failed step: retry with a new key.
func TestMalformedReplyFails(t *testing.T) {
	e := newEnv(t)
	e.model.do(func(values.StepRequest) (values.StepReply, error) { return values.StepReply{}, nil })
	st := ask(t, e, "привет")
	if st.StopReason != values.StopFailed {
		t.Fatalf("empty reply = %+v", st)
	}
}

// A request a day after the previous one keeps the thread: the earlier request goes as its text (its echo, whose model
// turn the Hub has purged, is not sent), and the echo of every step is one of the current request.
func TestEarlierRequestSentAsText(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "find_lessons", `{"subject":"s1"}`)).then(raw("r1", "reply", `{"text":"две пары"}`)).
		then(raw("c2", "list_subjects", `{}`)).then(raw("r2", "reply", `{"text":"один предмет"}`))
	st := ask(t, e, "какие пары")
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{"lessons":[]}`)}}); err != nil {
		t.Fatal(err)
	}

	st = ask(t, e, "а предметы")
	if st.Turn != 2 {
		t.Fatalf("second request = %+v", st)
	}
	if h := e.model.last(t).History; len(h) != 2 || h[0].StudentText != "какие пары" || h[1].StudentText != "а предметы" {
		t.Fatalf("first step of the next request = %+v", h)
	}
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c2", `{"subjects":[]}`)}}); err != nil {
		t.Fatal(err)
	}
	h := e.model.last(t).History
	if len(h) != 4 || h[0].StudentText != "какие пары" || h[2].Calls == nil || h[2].Calls.Calls[0].CallID != "c2" {
		t.Fatalf("second step of the next request = %+v", h)
	}
}

// The model turn expired on the Hub: a fresh request restarts by itself, one with done work asks the student.
func TestTurnGone(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"первый"}`)).
		fail(values.ErrTurnGone).
		then(raw("r2", "reply", `{"text":"второй"}`))
	ask(t, e, "первый вопрос")
	st := ask(t, e, "второй вопрос")
	if st.Status != thread.StatusDone || st.Turn != 3 {
		t.Fatalf("silent restart = %+v", st)
	}
	// The expired attempt carried no echo of the first request: earlier requests are sent as texts only.
	if h := e.model.requests()[1].History; len(h) != 2 || h[0].StudentText != "первый вопрос" || h[1].StudentText != "второй вопрос" {
		t.Fatalf("expired attempt history = %+v", h)
	}
	req := e.model.last(t)
	if len(req.History) != 1 || req.History[0].StudentText != "второй вопрос" || req.Key != "ag:th1:3:1" {
		t.Fatalf("restarted request = %q %+v", req.Key, req.History)
	}

	e2 := newEnv(t)
	e2.model.then(raw("w1", "create_subject", `{"name":"Химия"}`)).
		fail(values.ErrTurnGone).
		then(raw("r1", "reply", `{"text":"заново"}`))
	st = ask(t, e2, "создай предмет")
	st, err := e2.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{{
		Result: values.ToolResult{CallID: "w1", ResultJSON: `{"subject":"s2"}`}, Created: mustRef(t, values.KindSubject, "s2"),
	}}})
	if err != nil || st.StopReason != values.StopExpired {
		t.Fatalf("expired after a write = %+v, %v", st, err)
	}
	if _, err := e2.svc.Continue(t.Context(), ref(st)); !errors.Is(err, thread.ErrRestartRequired) {
		t.Fatalf("Continue after expiry = %v", err)
	}
	st, err = e2.svc.Restart(t.Context(), ref(st))
	if err != nil || st.Turn != 2 || st.Status != thread.StatusDone {
		t.Fatalf("Restart = %+v, %v", st, err)
	}
	if req := e2.model.last(t); len(req.History) != 1 || req.History[0].StudentText != "создай предмет" {
		t.Fatalf("restart history = %+v", req.History)
	}
}

// «Стоп» during a step wins: the reply is dropped and «Продолжить» replays the same key.
func TestStopDuringStep(t *testing.T) {
	e := newEnv(t)
	var stopped Step
	e.model.do(func(req values.StepRequest) (values.StepReply, error) {
		var err error
		stopped, err = e.svc.Stop(context.Background(), TurnRef{Thread: req.Thread, Turn: 1})
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
		return reply(raw("c1", "list_subjects", `{}`)), nil
	})
	st := ask(t, e, "мои предметы")
	if st.Status != thread.StatusStopped || st.StopReason != values.StopUser || stopped.Status != thread.StatusStopped {
		t.Fatalf("step after Stop = %+v", st)
	}
	st, err := e.svc.Continue(t.Context(), ref(st))
	if err != nil || len(st.Calls) != 1 {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	reqs := e.model.requests()
	if reqs[0].Key != reqs[1].Key {
		t.Fatalf("keys %q %q", reqs[0].Key, reqs[1].Key)
	}
	if all := e.history.all("th1"); len(all) != 2 {
		t.Fatalf("history after the dropped reply = %d entries, want text and one echo", len(all))
	}
}

// «Стоп» that loses its save to other saves twice still stops the request (bounded retry, TASK-116 follow-up); the
// step carries the scope of the request for the Driver.
func TestStopRetriesVersionConflicts(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_subjects", `{}`))
	st := ask(t, e, "мои предметы")
	if st.Scope != subjectScope(t) {
		t.Fatalf("step scope = %+v", st.Scope)
	}
	conflicts := 0
	e.threads.afterGet = func() {
		if conflicts == maxStopSaves-1 {
			return
		}
		conflicts++
		// Another save lands between Stop's load and its save.
		e.threads.mu.Lock()
		s := e.threads.states[st.Thread]
		s.Version++
		e.threads.states[st.Thread] = s
		e.threads.mu.Unlock()
	}
	got, err := e.svc.Stop(t.Context(), ref(st))
	if err != nil || got.Status != thread.StatusStopped || got.StopReason != values.StopUser {
		t.Fatalf("Stop after %d conflicts = %+v, %v", conflicts, got, err)
	}
}

// «Стоп» saved between Submit's load and save: the Driver's results are applied again to the stopped request, not
// dropped, and «Продолжить» goes on with them.
func TestSubmitRacingStop(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_subjects", `{}`)).then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "мои предметы")
	fired := false
	e.threads.afterGet = func() {
		if fired {
			return
		}
		fired = true
		if _, err := e.svc.Stop(context.Background(), ref(st)); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}

	got, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{"subjects":[]}`)}})
	if err != nil || got.Status != thread.StatusStopped || got.StopReason != values.StopUser {
		t.Fatalf("Submit after a racing Stop = %+v, %v", got, err)
	}
	stored := e.history.all(st.Thread)
	if last := stored[len(stored)-1].Entry; len(last.Results) != 1 || last.Results[0].CallID != "c1" {
		t.Fatalf("results dropped: last stored entry = %+v", last)
	}
	got, err = e.svc.Continue(t.Context(), ref(got))
	if err != nil || got.Status != thread.StatusDone {
		t.Fatalf("Continue = %+v, %v", got, err)
	}
	if res := lastResults(t, e); res[0].CallID != "c1" {
		t.Fatalf("results sent = %+v", res)
	}
}

// Submit and «Стоп» at the same time, in any order: the results are stored and no call stays open.
func TestSubmitConcurrentStop(t *testing.T) {
	for range 20 {
		e := newEnv(t)
		e.model.then(raw("c1", "list_subjects", `{}`)).then(raw("r1", "reply", `{"text":"ок"}`))
		st := ask(t, e, "мои предметы")
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = e.svc.Stop(context.Background(), ref(st)) }) // may find the request done or moved
		_, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}})
		wg.Wait()
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		th, _ := e.threads.Get(t.Context(), st.Thread)
		if turn, _ := th.Turn(); len(turn.OpenCalls()) != 0 {
			t.Fatalf("calls left open: %+v", turn.OpenCalls())
		}
		if !slices.ContainsFunc(e.history.all(st.Thread), func(h values.HistoryEntry) bool {
			return len(h.Entry.Results) == 1 && h.Entry.Results[0].CallID == "c1"
		}) {
			t.Fatal("the results of c1 are not stored")
		}
	}
}

// A save whose transaction rolls back (the history append failed after the thread was written) keeps the stored
// version in memory, so the next save of the same thread is not a conflict.
func TestRolledBackSaveKeepsVersion(t *testing.T) {
	e := newEnv(t)
	key, err := thread.KeyOf(subjectScope(t))
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th1", key, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := &change{th: th}
	if err := e.svc.save(t.Context(), c); err != nil || th.Version() != 1 {
		t.Fatalf("first save = %v, version %d", err, th.Version())
	}

	e.history.failing = errors.New("disk full")
	c.add(values.HistoryTurn{StudentText: "текст"})
	if err := e.svc.save(t.Context(), c); err == nil || th.Version() != 1 {
		t.Fatalf("failed save = %v, version %d, want 1", err, th.Version())
	}
	e.history.failing = nil
	if err := e.svc.save(t.Context(), c); err != nil {
		t.Fatalf("save after the rollback = %v", err)
	}
}

// At start a request left running stops as offline; calls handed to the Driver get outcome_unknown.
func TestRecover(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("w1", "create_subject", `{"name":"Химия"}`)).then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "создай предмет")

	n, err := e.svc.Recover(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v", n, err)
	}
	th, _ := e.threads.Get(t.Context(), st.Thread)
	turn, _ := th.Turn()
	if turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline {
		t.Fatalf("recovered = %s %s", turn.Status(), turn.StopReason())
	}
	if _, err := e.svc.Continue(t.Context(), ref(st)); err != nil {
		t.Fatal(err)
	}
	if got := lastResults(t, e); got[0].ResultJSON != `{"error":"outcome_unknown"}` {
		t.Fatalf("result after recovery = %+v", got)
	}
	if n, err := e.svc.Recover(t.Context()); err != nil || n != 0 {
		t.Fatalf("second Recover = %d, %v", n, err)
	}
}

// The Driver's context ended while its calls waited: they are answered not_done and the request stops as offline,
// so «Продолжить» goes on; a request the student stopped keeps its reason; a «Стоп» saved in between is retried.
func TestInterrupt(t *testing.T) {
	tests := []struct {
		name   string
		before func(t *testing.T, e *env, st Step)
		want   values.StopReason
	}{
		{"running", func(*testing.T, *env, Step) {}, values.StopOffline},
		{"stopped by the student", func(t *testing.T, e *env, st Step) {
			if _, err := e.svc.Stop(t.Context(), ref(st)); err != nil {
				t.Fatal(err)
			}
		}, values.StopUser},
		{"stop saved in between", func(t *testing.T, e *env, st Step) {
			fired := false
			e.threads.afterGet = func() {
				if fired {
					return
				}
				fired = true
				if _, err := e.svc.Stop(context.Background(), ref(st)); err != nil {
					t.Errorf("Stop: %v", err)
				}
			}
		}, values.StopUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("c1", "list_subjects", `{}`)).then(raw("r1", "reply", `{"text":"ок"}`))
			st := ask(t, e, "мои предметы")
			tt.before(t, e, st)

			got, err := e.svc.Interrupt(t.Context(), ref(st))
			if err != nil || got.Status != thread.StatusStopped || got.StopReason != tt.want || len(got.Calls) != 0 ||
				len(got.Cards) != 1 || got.Cards[0] != (values.StoppedCard{Reason: tt.want}) {
				t.Fatalf("Interrupt = %+v, %v", got, err)
			}
			th, _ := e.threads.Get(t.Context(), st.Thread)
			if turn, _ := th.Turn(); len(turn.OpenCalls()) != 0 {
				t.Fatalf("calls left open: %+v", turn.OpenCalls())
			}
			got, err = e.svc.Continue(t.Context(), ref(st))
			if err != nil || got.Status != thread.StatusDone {
				t.Fatalf("Continue = %+v, %v", got, err)
			}
			if res := lastResults(t, e); len(res) != 1 || res[0].ResultJSON != `{"error":"not_done"}` {
				t.Fatalf("results sent = %+v", res)
			}
		})
	}
}

func TestInterruptEndedRequest(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "привет")
	if _, err := e.svc.Interrupt(t.Context(), ref(st)); !errors.Is(err, thread.ErrInvalidTransition) {
		t.Fatalf("Interrupt of a done request = %v", err)
	}
	if _, err := e.svc.Interrupt(t.Context(), TurnRef{Thread: st.Thread, Turn: 9}); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("Interrupt of an old request = %v", err)
	}
}

// A new request closes the calls a waiting request left open, so the history keeps its shape.
func TestAskClosesWaitingRequest(t *testing.T) {
	e := newEnv(t)
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	e.model.then(raw("p1", "show_plan", plan)).then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "добавь")
	st2 := ask(t, e, "нет, другое")
	if st2.Turn != 2 || st2.Status != thread.StatusDone {
		t.Fatalf("second request = %+v", st2)
	}
	stored := e.history.all(st.Thread)
	if len(stored) != 6 || stored[2].Entry.Results[0].ResultJSON != `{"error":"not_done"}` || stored[3].Entry.StudentText != "нет, другое" {
		t.Fatalf("stored history = %+v", stored)
	}
	// The planner gets the earlier request as its text only.
	if h := e.model.last(t).History; len(h) != 2 || h[1].StudentText != "нет, другое" {
		t.Fatalf("history = %+v", h)
	}
	if _, err := e.svc.ApprovePlan(t.Context(), ref(st)); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("approve an old plan = %v", err)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	e.model.then(raw("p1", "show_plan", plan))
	st := ask(t, e, "добавь")
	st, err := e.svc.Cancel(t.Context(), ref(st))
	if err != nil || st.Status != thread.StatusCancelled {
		t.Fatalf("Cancel = %+v, %v", st, err)
	}
	if all := e.history.all(st.Thread); len(all) != 3 || all[2].Entry.Results[0].ResultJSON != `{"error":"not_done"}` {
		t.Fatalf("history = %+v", all)
	}
}

func TestAskInput(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty text = %v", err)
	}
	if _, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "x",
		Uploads: []values.Ref{mustRef(t, values.KindJob, "j")}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("upload of a wrong kind = %v", err)
	}
	// Uploads become known ids and reach the planner in the student's turn.
	e.model.then(raw("c1", "inspect_upload", `{"upload":"u1"}`))
	st, err := e.svc.Ask(t.Context(), AskInput{Scope: values.Scope{}, Text: "что это", Uploads: []values.Ref{mustRef(t, values.KindUpload, "u1")}})
	if err != nil || len(st.Calls) != 1 || !st.Calls[0].Paid {
		t.Fatalf("ask with an upload = %+v, %v", st, err)
	}
	if h := e.model.last(t).History; h[0].StudentText != "что это\n[upload:u1]" {
		t.Fatalf("student text = %q", h[0].StudentText)
	}
}
