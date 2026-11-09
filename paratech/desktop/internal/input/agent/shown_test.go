package agent

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

const th2 = shared.ThreadID("th2")

// stepOn is a step of request turn of thread th.
func stepOn(th shared.ThreadID, turn int, status thread.Status, calls []agentturn.Call, cards ...values.Card) agentturn.Step {
	return agentturn.Step{Thread: th, Turn: turn, Status: status, Calls: calls, Cards: cards}
}

// button is an offer_button call of call id that opens material m; its card is materialCard(m).
func button(t *testing.T, id, m string) agentturn.Call {
	t.Helper()
	arg, _, _ := values.OfferTarget(values.OfferOpenMaterial)
	args := fmt.Sprintf(`{"action":%q,%q:%q}`, values.OfferOpenMaterial, arg, m)
	call, err := values.ParseCallAt(id, string(values.ToolOfferButton), args, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	return agentturn.Call{Call: call, Class: actionpolicy.ClassUI, PlanStep: -1, Card: values.CardOfferButton}
}

func materialCard(t *testing.T, m string) values.Card {
	t.Helper()
	return values.OfferButtonCard{Action: values.OfferOpenMaterial, Target: mustRef(t, values.KindMaterial, m)}
}

func reply(text string) values.Card { return values.ReplyCard{Text: values.Text(text)} }

func question(t *testing.T) values.Card {
	t.Helper()
	q := values.QuestionsCard{Round: 1, Questions: []values.Question{{Kind: values.QuestionText, Text: "К какому сроку?"}}}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	return q
}

func assertCards(t *testing.T, what string, got []values.Card, want ...values.Card) {
	t.Helper()
	if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Fatalf("%s: cards = %+v, want %+v", what, got, want)
	}
}

func lastOf(t *testing.T, sink *chanSink) Update {
	t.Helper()
	all := sink.all()
	if len(all) == 0 {
		t.Fatal("no update")
	}
	return all[len(all)-1]
}

// The tool cards of a request stay on every later update of it, in order, before the step's own cards; the step
// cards (questions, reply) are not carried on (BUG-20261108-agent-cards).
func TestToolCardsStayOnLaterUpdates(t *testing.T) {
	tests := []struct {
		name  string
		steps []agentturn.Step
		want  func(t *testing.T) []values.Card
	}{
		{
			name: "tool then reply",
			steps: []agentturn.Step{
				step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}),
				step(thread.StatusDone, nil, reply("Готово")),
			},
			want: func(t *testing.T) []values.Card { return []values.Card{materialCard(t, "m1"), reply("Готово")} },
		},
		{
			name: "two tool steps then reply",
			steps: []agentturn.Step{
				step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}),
				step(thread.StatusExecuting, []agentturn.Call{button(t, "c2", "m2"), button(t, "c3", "m3")}),
				step(thread.StatusDone, nil, reply("Готово")),
			},
			want: func(t *testing.T) []values.Card {
				return []values.Card{materialCard(t, "m1"), materialCard(t, "m2"), materialCard(t, "m3"), reply("Готово")}
			},
		},
		{
			name: "a replayed tool card is shown once",
			steps: []agentturn.Step{
				step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}),
				step(thread.StatusExecuting, []agentturn.Call{button(t, "c2", "m1")}),
				step(thread.StatusDone, nil, reply("Готово")),
			},
			want: func(t *testing.T) []values.Card { return []values.Card{materialCard(t, "m1"), reply("Готово")} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns := &scriptedTurns{}
			for _, st := range tt.steps {
				turns.then(st, nil)
			}
			d, sink := newDriver(t, newReads(), turns)
			if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "покажи"}); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			assertCards(t, "last update", lastOf(t, sink).Cards, tt.want(t)...)
		})
	}
}

// A step card (questions) is shown by its own update only: after the answers the next update has the tool cards and
// its own, never the questions again.
func TestStepCardsAreNotCarried(t *testing.T) {
	turns := (&scriptedTurns{}).
		then(step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}), nil).
		then(step(thread.StatusAwaitingAnswers, nil, question(t)), nil).
		then(step(thread.StatusDone, nil, reply("Готово")), nil)
	d, sink := newDriver(t, newReads(), turns)
	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "покажи"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	assertCards(t, "questions", lastOf(t, sink).Cards, materialCard(t, "m1"), question(t))

	if err := d.AnswerQuestions(t.Context(), ref1, []values.Answer{{Question: 0, Text: "к пятнице"}}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	assertCards(t, "after the answers", lastOf(t, sink).Cards, materialCard(t, "m1"), reply("Готово"))
}

// A new request of the thread starts with none of the older one's tool cards; an update of the older request after it
// gets none and leaves the newer request's cards.
func TestToolCardsOfANewRequest(t *testing.T) {
	turns := (&scriptedTurns{}).
		then(step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}), nil).
		then(step(thread.StatusDone, nil, reply("Первый")), nil).
		then(stepOn(th1, 2, thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m2")}), nil).
		then(stepOn(th1, 2, thread.StatusDone, nil, reply("Второй")), nil)
	d, sink := newDriver(t, newReads(), turns)
	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "покажи"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if err := d.Rerun(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	u := lastOf(t, sink)
	if u.Turn != 2 {
		t.Fatalf("turn = %d", u.Turn)
	}
	assertCards(t, "new request", u.Cards, materialCard(t, "m2"), reply("Второй"))

	if err := d.Cancel(t.Context(), ref1); err != nil { // the fake cancels turn 1 as asked
		t.Fatal(err)
	}
	d.Wait()
	u = lastOf(t, sink)
	if u.Turn != 1 || u.Status != thread.StatusCancelled {
		t.Fatalf("older update = %+v", u)
	}
	assertCards(t, "older request", u.Cards)

	turns.current = stepOn(th1, 2, thread.StatusDone, nil, reply("Второй"))
	cur, ok, err := d.Current(t.Context(), th1)
	if err != nil || !ok {
		t.Fatalf("current: %v %v", ok, err)
	}
	assertCards(t, "current", cur.Cards, materialCard(t, "m2"), reply("Второй"))
}

// The request is stopped after a tool card (a read cut after it): the stopped update still shows it, before the
// stopped card. The loop closes the step through interrupt as on «Стоп»; the ctx ending makes the order of updates
// deterministic (with «Стоп» the Stop operation publishes from its own goroutine).
func TestStoppedUpdateKeepsToolCards(t *testing.T) {
	r := newReads()
	r.library.block, r.library.started = true, make(chan struct{})
	turns := &stopTurns{first: step(thread.StatusExecuting, []agentturn.Call{
		button(t, "c1", "m1"), readCall(t, "c2", values.ToolListSubjects, `{}`),
	})}
	sink := &chanSink{}
	d, err := New(r.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := d.Ask(ctx, agentturn.AskInput{Text: "покажи"}); err != nil {
		t.Fatal(err)
	}
	<-r.library.started
	cancel()
	d.Wait()
	if rep := turns.report(); len(rep.interrupted) != 1 || len(rep.submitted) != 0 {
		t.Fatalf("report %+v", rep)
	}
	u := lastOf(t, sink)
	if u.Status != thread.StatusStopped {
		t.Fatalf("last update = %+v", u)
	}
	assertCards(t, "stopped", u.Cards, materialCard(t, "m1"), values.StoppedCard{Reason: values.StopOffline})
}

// Updates that are not request states carry no kept cards: a refused button (no status: the panel keeps what it
// shows) and a failed Ask with no thread.
func TestProblemUpdatesGetNoToolCards(t *testing.T) {
	turns := (&scriptedTurns{}).
		then(step(thread.StatusExecuting, []agentturn.Call{button(t, "c1", "m1")}), nil).
		then(step(thread.StatusAwaitingPlan, nil), nil).
		then(agentturn.Step{}, thread.ErrTurnInProgress).
		then(agentturn.Step{}, thread.ErrBudgetExceeded)
	d, sink := newDriver(t, newReads(), turns)
	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "покажи"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	assertCards(t, "plan wait", lastOf(t, sink).Cards, materialCard(t, "m1"))

	if err := d.ApprovePlan(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	u := lastOf(t, sink)
	if u.Problem != ProblemBusy || u.Status != "" || u.Thread != th1 {
		t.Fatalf("refused button = %+v", u)
	}
	assertCards(t, "refused button", u.Cards)

	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "ещё"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	u = lastOf(t, sink)
	if u.Problem != ProblemBudget || u.Thread != "" {
		t.Fatalf("failed ask = %+v", u)
	}
	assertCards(t, "failed ask", u.Cards)
}

// The kept cards: a newer turn starts empty, an older one changes nothing, equal cards are kept once, the cap drops
// the oldest, and only updates with a thread and a status get them.
func TestRememberAndShownCards(t *testing.T) {
	d, _ := newDriver(t, newReads(), &scriptedTurns{})
	at := func(turn int) agentturn.Step { return stepOn(th1, turn, thread.StatusExecuting, nil) }

	d.remember(at(1), []values.Card{materialCard(t, "m1"), materialCard(t, "m2")})
	d.remember(at(1), []values.Card{materialCard(t, "m2"), materialCard(t, "m3")})
	assertCards(t, "dedupe", d.shownCards(at(1)), materialCard(t, "m1"), materialCard(t, "m2"), materialCard(t, "m3"))

	tests := []struct {
		name string
		st   agentturn.Step
	}{
		{"no thread", agentturn.Step{Turn: 1, Status: thread.StatusExecuting}},
		{"no status", agentturn.Step{Thread: th1, Turn: 1}},
		{"other thread", stepOn(th2, 1, thread.StatusExecuting, nil)},
		{"older turn", at(0)},
	}
	for _, tt := range tests {
		assertCards(t, tt.name, d.shownCards(tt.st))
	}
	assertCards(t, "after the misses", d.shownCards(at(1)), materialCard(t, "m1"), materialCard(t, "m2"), materialCard(t, "m3"))

	d.remember(at(2), []values.Card{materialCard(t, "m9")})
	d.remember(at(1), []values.Card{materialCard(t, "m4")}) // a late step of the older request
	assertCards(t, "newer turn", d.shownCards(at(2)), materialCard(t, "m9"))
	assertCards(t, "older turn", d.shownCards(at(1)))

	var many []values.Card
	for i := range maxShownCards + 6 {
		many = append(many, materialCard(t, fmt.Sprintf("m%d", 100+i)))
	}
	d.remember(at(3), many)
	assertCards(t, "cap", d.shownCards(at(3)), many[6:]...)

	if got := d.shownCards(at(4)); len(got) != 0 { // a newer update starts its request empty
		t.Fatalf("newer update = %+v", got)
	}
	assertCards(t, "dropped by the newer turn", d.shownCards(at(3)))
}

// routedTurns runs the script of each thread: requests of two threads in parallel.
type routedTurns struct {
	*scriptedTurns
	by map[shared.ThreadID]*scriptedTurns
}

func (r routedTurns) Ask(ctx context.Context, in agentturn.AskInput) (agentturn.Step, error) {
	return r.by[in.ThreadID].Ask(ctx, in)
}

func (r routedTurns) Submit(ctx context.Context, in agentturn.SubmitInput) (agentturn.Step, error) {
	return r.by[in.Turn.Thread].Submit(ctx, in)
}

// Two threads run their requests at once: each last update has its own tool cards only (go test -race).
func TestToolCardsOfParallelRequests(t *testing.T) {
	const rounds = 20
	script := func(th shared.ThreadID, m string) *scriptedTurns {
		s := &scriptedTurns{}
		for range rounds {
			s.then(stepOn(th, 1, thread.StatusExecuting, []agentturn.Call{button(t, "c1", m)}), nil)
		}
		return s.then(stepOn(th, 1, thread.StatusDone, nil, reply("Готово")), nil)
	}
	turns := routedTurns{scriptedTurns: &scriptedTurns{}, by: map[shared.ThreadID]*scriptedTurns{th1: script(th1, "m1"), th2: script(th2, "m2")}}
	d, sink := newDriver(t, newReads(), turns)

	var wg sync.WaitGroup
	for _, th := range []shared.ThreadID{th1, th2} {
		wg.Go(func() {
			if _, err := d.Ask(t.Context(), agentturn.AskInput{ThreadID: th, Text: "покажи"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	d.Wait()

	last := map[shared.ThreadID]Update{}
	for _, u := range sink.all() {
		last[u.Thread] = u
	}
	assertCards(t, "th1", last[th1].Cards, materialCard(t, "m1"), reply("Готово"))
	assertCards(t, "th2", last[th2].Cards, materialCard(t, "m2"), reply("Готово"))
}
