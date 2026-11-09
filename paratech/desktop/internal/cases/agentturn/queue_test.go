package agentturn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

const replyDone = `{"text":"готово"}`

// executing leaves the current chat th1 with a request whose calls wait for the Driver (a tool run).
func executing(t *testing.T, e *env) Step {
	t.Helper()
	e.model.then(raw("c1", "list_subjects", `{}`))
	st := ask(t, e, "мои предметы")
	if st.Status != thread.StatusPlanning || len(st.Calls) != 1 { // EXECUTING only under an approved plan

		t.Fatalf("setup step = %+v", st)
	}
	return st
}

// queue asks text in the current chat and wants it queued.
func queue(t *testing.T, e *env, text string) Step {
	t.Helper()
	st, err := e.svc.Ask(t.Context(), AskInput{Text: text})
	if err != nil || !st.Queued || st.Ticket == "" || st.Turn != 0 || st.Status != "" {
		t.Fatalf("Ask %q = %+v, %v, want queued", text, st, err)
	}
	return st
}

// finish answers the calls of st and lets the planner reply: the request is DONE.
func finish(t *testing.T, e *env, st Step) {
	t.Helper()
	e.model.then(raw("r1", "reply", replyDone))
	done, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{"subjects":[]}`)}})
	if err != nil || done.Status != thread.StatusDone {
		t.Fatalf("finish = %+v, %v", done, err)
	}
}

// startNext starts the oldest waiting question of th1, which the planner answers at once.
func startNext(t *testing.T, e *env) Step {
	t.Helper()
	e.model.then(raw("r9", "reply", replyDone))
	st, err := e.svc.StartQueued(t.Context(), "th1")
	if err != nil {
		t.Fatalf("StartQueued: %v", err)
	}
	return st
}

func lastText(t *testing.T, e *env) string {
	t.Helper()
	h := e.model.last(t).History
	return h[len(h)-1].StudentText
}

// An Ask during a Hub step returns queued at once, while the step still runs (it does not wait for s.mu), and
// reaches the model only once StartQueued starts it.
func TestAskQueuesDuringAHubStep(t *testing.T) {
	e := newEnv(t)
	entered, release := make(chan struct{}), make(chan struct{})
	e.model.do(func(values.StepRequest) (values.StepReply, error) {
		close(entered)
		<-release
		return reply(raw("r1", "reply", replyDone)), nil
	})
	first := make(chan error, 1)
	go func() {
		_, err := e.svc.Ask(context.Background(), AskInput{Scope: subjectScope(t), Text: "первый"})
		first <- err
	}()
	<-entered

	got := make(chan Step, 1)
	go func() {
		st, err := e.svc.Ask(context.Background(), AskInput{Text: "второй"})
		if err != nil {
			t.Errorf("queued Ask: %v", err)
		}
		got <- st
	}()
	var q Step
	select {
	case q = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the Ask waited for the running step")
	}
	if !q.Queued || q.Thread != "th1" || q.Ticket == "" {
		t.Fatalf("queued step = %+v", q)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if n := len(e.model.requests()); n != 1 {
		t.Fatalf("%d requests reached the model before the start", n)
	}

	st := startNext(t, e)
	if st.Ticket != q.Ticket || st.Turn != 2 || st.Status != thread.StatusDone || lastText(t, e) != "второй" {
		t.Fatalf("started = %+v, text %q", st, lastText(t, e))
	}
}

// While tools run the question waits too; StartQueued refuses as busy until the request is final, then starts the
// three waiting questions in order.
func TestQueueKeepsOrderWhileToolsRun(t *testing.T) {
	e := newEnv(t)
	st := executing(t, e)
	var tickets []Ticket
	for _, text := range []string{"а", "б", "в"} {
		tickets = append(tickets, queue(t, e, text).Ticket)
	}
	if !e.svc.HasQueued("th1") || tickets[0] == tickets[1] || tickets[1] == tickets[2] {
		t.Fatalf("tickets %v, has %v", tickets, e.svc.HasQueued("th1"))
	}
	before := storedThread(t, e, "th1").Version()
	if _, err := e.svc.StartQueued(t.Context(), "th1"); !errors.Is(err, thread.ErrTurnInProgress) {
		t.Fatalf("StartQueued while tools run = %v", err)
	}
	if storedThread(t, e, "th1").Version() != before || !e.svc.HasQueued("th1") {
		t.Fatal("a refused StartQueued changed something")
	}

	finish(t, e, st)
	for i, want := range []string{"а", "б", "в"} {
		got := startNext(t, e)
		if got.Ticket != tickets[i] || got.Turn != i+2 || lastText(t, e) != want {
			t.Fatalf("start %d = %+v, text %q", i, got, lastText(t, e))
		}
	}
	if e.svc.HasQueued("th1") {
		t.Fatal("questions left")
	}
	if _, err := e.svc.StartQueued(t.Context(), "th1"); !errors.Is(err, ErrNothingQueued) {
		t.Fatalf("StartQueued of an empty queue = %v", err)
	}
}

// A request EXECUTING an approved plan (its action waits for the Driver) queues the question too.
func TestQueueWhileAPlanExecutes(t *testing.T) {
	e := newEnv(t)
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	e.model.then(raw("p1", "show_plan", plan)).then(raw("c1", "create_subject", `{"name":"Химия"}`))
	st := ask(t, e, "добавь химию")
	run, err := e.svc.ApprovePlan(t.Context(), ref(st))
	if err != nil || run.Status != thread.StatusExecuting || len(run.Calls) != 1 {
		t.Fatalf("approved = %+v, %v", run, err)
	}
	q := queue(t, e, "потом")
	if _, err := e.svc.Stop(t.Context(), ref(run)); err != nil {
		t.Fatal(err)
	}
	if got := startNext(t, e); got.Ticket != q.Ticket || lastText(t, e) != "потом" {
		t.Fatalf("started = %+v", got)
	}
}

// The oldest question starts after any final status of the request: done, stopped by a failure (offline or failed),
// stopped by the student, cancelled.
func TestQueueStartsAfterAnyFinalStatus(t *testing.T) {
	tests := []struct {
		name string
		end  func(t *testing.T, e *env, st Step)
		want thread.Status
	}{
		{"done", finish, thread.StatusDone},
		{"stopped offline", func(t *testing.T, e *env, st Step) {
			e.model.fail(errors.New("connection reset"))
			if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil {
				t.Fatal(err)
			}
		}, thread.StatusStopped},
		{"stopped failed", func(t *testing.T, e *env, st Step) {
			e.model.fail(fmt.Errorf("provider: %w", values.ErrHubFailure))
			if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil {
				t.Fatal(err)
			}
		}, thread.StatusStopped},
		{"stopped by the student", func(t *testing.T, e *env, st Step) {
			if _, err := e.svc.Stop(t.Context(), ref(st)); err != nil {
				t.Fatal(err)
			}
		}, thread.StatusStopped},
		{"cancelled", func(t *testing.T, e *env, st Step) {
			if _, err := e.svc.Cancel(t.Context(), ref(st)); err != nil {
				t.Fatal(err)
			}
		}, thread.StatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			st := executing(t, e)
			q := queue(t, e, "следующий")
			tt.end(t, e, st)
			if turn, _ := storedThread(t, e, "th1").Turn(); turn.Status() != tt.want {
				t.Fatalf("ended as %s, want %s", turn.Status(), tt.want)
			}
			got := startNext(t, e)
			if got.Ticket != q.Ticket || got.Turn != 2 || got.Status != thread.StatusDone || lastText(t, e) != "следующий" {
				t.Fatalf("started = %+v", got)
			}
		})
	}
}

// A request waiting for the student keeps today's behaviour: an Ask with nothing queued is not queued (it cancels the
// request); with questions waiting, a new one joins them, and none starts while the student is asked.
func TestQueueAndARequestWaitingForTheStudent(t *testing.T) {
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	q := `{"questions":[{"kind":"choice","question":"Какой вариант?","options":["А","Б"],"default":0}]}`
	for _, card := range []values.RawCall{raw("p1", "show_plan", plan), raw("q1", "ask_questions", q)} {
		t.Run(card.Tool, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(card)
			if st := ask(t, e, "добавь"); st.Status != thread.StatusAwaitingPlan && st.Status != thread.StatusAwaitingAnswers {
				t.Fatalf("setup = %+v", st)
			}
			if _, ok, err := e.svc.Enqueue(t.Context(), AskInput{Text: "другое"}); ok || err != nil {
				t.Fatalf("Enqueue while the student is asked = %v, %v", ok, err)
			}

			e2 := newEnv(t)
			st := executing(t, e2)
			queue(t, e2, "первый")
			e2.model.then(card)
			if next, err := e2.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil ||
				(next.Status != thread.StatusAwaitingPlan && next.Status != thread.StatusAwaitingAnswers) {
				t.Fatalf("submit = %+v, %v", next, err)
			}
			queue(t, e2, "второй") // joins the waiting one, FIFO
			if _, err := e2.svc.StartQueued(t.Context(), "th1"); !errors.Is(err, ErrNothingQueued) {
				t.Fatalf("StartQueued while the student is asked = %v", err)
			}
		})
	}
}

// QueueReady is true exactly when StartQueued would take a question: one waits and the chat's request is final.
func TestQueueReady(t *testing.T) {
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	e := newEnv(t)
	if e.svc.QueueReady(t.Context(), "th1") {
		t.Fatal("ready without a chat")
	}
	st := executing(t, e)
	if e.svc.QueueReady(t.Context(), "th1") {
		t.Fatal("ready with nothing queued")
	}
	queue(t, e, "первый")
	if e.svc.QueueReady(t.Context(), "th1") {
		t.Fatal("ready while tools run")
	}

	e.model.then(raw("p1", "show_plan", plan))
	if next, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); err != nil ||
		next.Status != thread.StatusAwaitingPlan {
		t.Fatalf("submit = %+v, %v", next, err)
	}
	if e.svc.QueueReady(t.Context(), "th1") {
		t.Fatal("ready while the student is asked")
	}

	cur, ok, err := e.svc.Current(t.Context(), "th1")
	if err != nil || !ok {
		t.Fatalf("current = %+v, %v, %v", cur, ok, err)
	}
	if _, err := e.svc.Cancel(t.Context(), ref(cur)); err != nil {
		t.Fatal(err)
	}
	if !e.svc.QueueReady(t.Context(), "th1") {
		t.Fatal("not ready after the request ended")
	}
	if got := startNext(t, e); lastText(t, e) != "первый" || e.svc.QueueReady(t.Context(), "th1") {
		t.Fatalf("started = %+v, still ready", got)
	}
}

// The started request uses the question's own scope, text and mentions; a mention deleted while it waited is dropped.
func TestQueuedQuestionKeepsItsOwnContext(t *testing.T) {
	e := newEnv(t)
	m1, m2 := mention(t, values.KindSubject, "s1", "Физика"), mention(t, values.KindSubject, "s2", "Химия")
	known := &fakeMentioned{exists: values.NewRefSet(m1.Ref(), m2.Ref())}
	withMentions(t, e, known)
	st := executing(t, e)
	scope, err := values.NewScope(values.Ref{}, values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.svc.Ask(t.Context(), AskInput{Scope: scope, Text: "про обе", Mentions: []values.Mention{m1, m2}})
	if err != nil || !q.Queued || q.Scope != scope {
		t.Fatalf("queued = %+v, %v", q, err)
	}
	known.mu.Lock()
	known.exists = values.NewRefSet(m1.Ref()) // s2 deleted meanwhile
	known.mu.Unlock()
	finish(t, e, st)

	got := startNext(t, e)
	req := e.model.last(t)
	if got.Ticket != q.Ticket || got.Scope != scope || req.Scope != scope || lastText(t, e) != "про обе" {
		t.Fatalf("started = %+v, request scope %+v", got, req.Scope)
	}
	if len(req.Mentions) != 1 || req.Mentions[0].Ref != m1.Ref() {
		t.Fatalf("mentions sent = %+v", req.Mentions)
	}
}

// Withdraw removes a waiting question (never sent), reports one that started, and knows nothing else; other chats
// have queues of their own.
func TestWithdraw(t *testing.T) {
	e := newEnv(t)
	st := executing(t, e)
	gone, kept := queue(t, e, "отзову"), queue(t, e, "оставлю")
	if r := e.svc.Withdraw("th1", gone.Ticket); r != WithdrawWithdrawn {
		t.Fatalf("Withdraw waiting = %s", r)
	}
	if r := e.svc.Withdraw("th1", gone.Ticket); r != WithdrawNotFound {
		t.Fatalf("Withdraw twice = %s", r)
	}
	if r := e.svc.Withdraw("th2", kept.Ticket); r != WithdrawNotFound {
		t.Fatalf("Withdraw in another chat = %s", r)
	}
	if r := e.svc.Withdraw("th1", "q999"); r != WithdrawNotFound {
		t.Fatalf("Withdraw unknown = %s", r)
	}

	finish(t, e, st)
	if got := startNext(t, e); got.Ticket != kept.Ticket || lastText(t, e) != "оставлю" {
		t.Fatalf("started = %+v", got)
	}
	if r := e.svc.Withdraw("th1", kept.Ticket); r != WithdrawStarted {
		t.Fatalf("Withdraw started = %s", r)
	}
	for _, req := range e.model.requests() {
		for _, h := range req.History {
			if h.StudentText == "отзову" {
				t.Fatal("a withdrawn question reached the model")
			}
		}
	}
	if _, err := e.svc.StartQueued(t.Context(), "th1"); !errors.Is(err, ErrNothingQueued) {
		t.Fatalf("StartQueued after the queue emptied = %v", err)
	}
}

// At most MaxQueued wait per chat; one more is the old busy refusal. Another chat is not affected.
func TestQueueLimitAndTwoChats(t *testing.T) {
	e := newEnv(t)
	executing(t, e)
	for i := range MaxQueued {
		queue(t, e, fmt.Sprintf("вопрос %d", i))
	}
	if _, err := e.svc.Ask(t.Context(), AskInput{Text: "шестой"}); !errors.Is(err, thread.ErrTurnInProgress) {
		t.Fatalf("sixth Ask = %v", err)
	}

	other, err := thread.New("th2", e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	e.model.then(raw("r1", "reply", replyDone))
	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: "th2", Text: "во втором чате"})
	if err != nil || st.Queued || st.Status != thread.StatusDone || st.Thread != "th2" {
		t.Fatalf("Ask in an idle chat = %+v, %v", st, err)
	}
	if e.svc.HasQueued("th2") || !e.svc.HasQueued("th1") {
		t.Fatal("the queues of the chats mixed")
	}
	if _, err := e.svc.StartQueued(t.Context(), "th2"); !errors.Is(err, ErrNothingQueued) {
		t.Fatalf("StartQueued of the other chat = %v", err)
	}
}

// Enqueue checks the input as Ask does, resolves the current chat, and queues nothing without a chat.
func TestEnqueue(t *testing.T) {
	e := newEnv(t)
	if _, ok, err := e.svc.Enqueue(t.Context(), AskInput{Text: "привет"}); ok || err != nil {
		t.Fatalf("Enqueue without a chat = %v, %v", ok, err)
	}
	executing(t, e)
	for _, in := range []AskInput{
		{Text: ""},
		{ThreadID: "bad id!", Text: "x"},
		{Text: "x", RestartFrom: 1},
		{Text: "x", Mentions: []values.Mention{{}}},
	} {
		if _, ok, err := e.svc.Enqueue(t.Context(), in); ok || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Enqueue %+v = %v, %v", in, ok, err)
		}
	}
	q, ok, err := e.svc.Enqueue(t.Context(), AskInput{Text: "в текущий"})
	if err != nil || !ok || q.Thread != "th1" || q.Ticket.Validate() != nil {
		t.Fatalf("Enqueue in the current chat = %+v, %v, %v", q, ok, err)
	}
	// A chat that does not exist is not queued: Ask reports it.
	if _, ok, err := e.svc.Enqueue(t.Context(), AskInput{ThreadID: "nope", Text: "x"}); ok || err != nil {
		t.Fatalf("Enqueue in a missing chat = %v, %v", ok, err)
	}
}

// An Ask the thread refuses as busy once s.mu is held (a request started after Enqueue read the chat) is queued, not
// refused.
func TestAskQueuesWhenTheRaceIsLost(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", replyDone))
	ask(t, e, "первый") // th1, DONE
	var once sync.Once
	e.threads.mu.Lock()
	e.threads.afterGet = func() {
		once.Do(func() {
			e.threads.mu.Lock()
			stored := e.threads.states["th1"]
			e.threads.mu.Unlock()
			th, err := thread.Restore(stored)
			if err == nil {
				err = th.Ask(thread.Request{Text: "чужой"}, e.clock.Now())
			}
			if err == nil {
				err = e.threads.Save(context.Background(), th)
			}
			if err != nil {
				t.Errorf("competing request: %v", err)
			}
		})
	}
	e.threads.mu.Unlock()

	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: "th1", Text: "второй"})
	if err != nil || !st.Queued || st.Thread != "th1" || st.Ticket == "" {
		t.Fatalf("Ask that lost the race = %+v, %v", st, err)
	}
	if !e.svc.HasQueued("th1") || len(e.model.requests()) != 1 {
		t.Fatal("the question is not waiting")
	}
}

// Every step of the first drive of a started question carries its ticket: the progress before the Hub step, and the
// step of a failed start too.
func TestStartedStepsCarryTheTicket(t *testing.T) {
	p := &recProgress{}
	e := newEnvWith(t, p)
	p.threads = e.threads
	st := executing(t, e)
	q := queue(t, e, "следующий")
	finish(t, e, st)
	got := startNext(t, e)
	steps, _ := p.got()
	last := steps[len(steps)-1]
	if got.Ticket != q.Ticket || last.Ticket != q.Ticket || last.Turn != 2 {
		t.Fatalf("step %+v, progress %+v", got, last)
	}

	// The chat is gone when the question starts: the step names the chat, the scope and the ticket with the error.
	e2 := newEnv(t)
	st2 := executing(t, e2)
	q2 := queue(t, e2, "потом")
	finish(t, e2, st2)
	e2.threads.mu.Lock()
	delete(e2.threads.states, "th1")
	e2.threads.mu.Unlock()
	failed, err := e2.svc.StartQueued(t.Context(), "th1")
	if !errors.Is(err, shared.ErrNotFound) || failed.Thread != "th1" || failed.Ticket != q2.Ticket {
		t.Fatalf("failed start = %+v, %v", failed, err)
	}
}

// The queue lives in memory: a new service (the app started again) has nothing waiting, so nothing is sent.
func TestQueueIsNotKept(t *testing.T) {
	e := newEnv(t)
	executing(t, e)
	queue(t, e, "подожду")
	withMentions(t, e, nil) // a new service on the same store
	if e.svc.HasQueued("th1") {
		t.Fatal("a waiting question survived the service")
	}
}

func TestTicketValidate(t *testing.T) {
	for _, tt := range []struct {
		t  Ticket
		ok bool
	}{
		{"q1", true}, {"Q_a-9", true}, {"", false}, {"q 1", false}, {"q1;", false}, {"щ1", false},
		{Ticket("q" + string(make([]byte, 31))), false}, {"abcdefghijabcdefghijabcdefghij12", true},
		{"abcdefghijabcdefghijabcdefghij123", false},
	} {
		if err := tt.t.Validate(); (err == nil) != tt.ok {
			t.Errorf("Validate(%q) = %v", tt.t, err)
		}
	}
}
