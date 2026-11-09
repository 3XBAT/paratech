package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentcompact"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// stateStore keeps the threads of the real step use case as stored states (each read restores a copy, saves compare
// the version), so an Enqueue that reads a chat while its step runs shares nothing with the step.
type stateStore struct {
	mu      sync.Mutex
	txMu    sync.Mutex
	states  map[shared.ThreadID]thread.State
	history map[shared.ThreadID][]values.HistoryEntry
	next    int
}

func newStateStore() *stateStore {
	return &stateStore{states: map[shared.ThreadID]thread.State{}, history: map[shared.ThreadID][]values.HistoryEntry{}}
}

func (s *stateStore) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	s.mu.Lock()
	st, ok := s.states[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("thread %s: %w", id, shared.ErrNotFound)
	}
	return thread.Restore(st)
}

func (s *stateStore) Current(ctx context.Context) (*thread.Thread, error) {
	s.mu.Lock()
	var cur *thread.State
	for _, st := range s.states {
		if st.ArchivedAt.IsZero() && (cur == nil || st.LastActive.After(cur.LastActive) || (st.LastActive.Equal(cur.LastActive) && st.ID > cur.ID)) {
			cur = &st
		}
	}
	s.mu.Unlock()
	if cur == nil {
		return nil, fmt.Errorf("current chat: %w", shared.ErrNotFound)
	}
	return s.Get(ctx, cur.ID)
}

func (s *stateStore) Save(_ context.Context, th *thread.Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := th.State()
	if stored, ok := s.states[st.ID]; (st.Version == 0 && ok) || (st.Version != 0 && (!ok || stored.Version != st.Version)) {
		return shared.ErrVersionConflict
	}
	st.Version++
	s.states[st.ID] = st
	th.Persisted(st.Version)
	return nil
}

func (s *stateStore) ListRunning(context.Context) ([]*thread.Thread, error) { return nil, nil }

func (s *stateStore) Append(_ context.Context, id shared.ThreadID, turn int, e values.HistoryTurn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history[id] = append(s.history[id], values.HistoryEntry{Turn: turn, Entry: e})
	return nil
}

func (s *stateStore) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []values.HistoryEntry
	for _, e := range s.history[id] {
		if e.Turn >= from {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *stateStore) NewThreadID() shared.ThreadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return shared.ThreadID(fmt.Sprintf("th%d", s.next))
}

// Do runs one transaction at a time, like the single SQLite writer (no rollback: nothing here fails).
func (s *stateStore) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return fn(ctx)
}

// stepModel answers the planner steps in order with its script; each entry may block. It records the requests.
type stepModel struct {
	mu     sync.Mutex
	script []func(ctx context.Context) ([]values.RawCall, error)
	reqs   []values.StepRequest
}

func (m *stepModel) Step(ctx context.Context, req values.StepRequest, _ func(string)) (values.StepReply, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	if len(m.script) == 0 {
		m.mu.Unlock()
		return values.StepReply{}, fmt.Errorf("script: no step %d", len(m.reqs))
	}
	next := m.script[0]
	m.script = m.script[1:]
	m.mu.Unlock()
	calls, err := next(ctx)
	if err != nil {
		return values.StepReply{}, err
	}
	return values.StepReply{Calls: values.SealedCalls{Calls: calls, Seal: []byte("seal")}}, nil
}

func (m *stepModel) then(f func(ctx context.Context) ([]values.RawCall, error)) *stepModel {
	m.script = append(m.script, f)
	return m
}

func (m *stepModel) reply(text string) *stepModel {
	id := fmt.Sprintf("r%d", len(m.script)+1)
	return m.then(func(context.Context) ([]values.RawCall, error) {
		return []values.RawCall{{CallID: id, Tool: "reply", ArgumentsJSON: `{"text":"` + text + `"}`}}, nil
	})
}

// texts are the newest student text each step sent.
func (m *stepModel) texts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.reqs {
		text := ""
		for _, h := range r.History {
			if h.StudentText != "" {
				text = h.StudentText
			}
		}
		out = append(out, text)
	}
	return out
}

type queueEnv struct {
	drv   *Driver
	turns *agentturn.Service
	store *stateStore
	model *stepModel
	sink  *chanSink
	reads *reads
}

// newQueueEnv is the Driver on the real step use case, with its progress on the same sink.
func newQueueEnv(t *testing.T) *queueEnv {
	t.Helper()
	e := &queueEnv{store: newStateStore(), model: &stepModel{}, sink: &chanSink{}, reads: newReads()}
	facts, err := NewFacts(&memLessons{})
	if err != nil {
		t.Fatal(err)
	}
	progress, err := NewProgress(e.sink)
	if err != nil {
		t.Fatal(err)
	}
	e.turns, err = agentturn.New(agentturn.Deps{
		Model: e.model, Threads: e.store, History: e.store, Subjects: noSubjects{}, Facts: facts, Tx: e.store,
		Clock: fixedClock{}, IDs: e.store, Progress: progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.drv, err = New(e.reads.deps(e.turns, e.sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.drv.Close)
	return e
}

func listSubjects(context.Context) ([]values.RawCall, error) {
	return []values.RawCall{{CallID: "c1", Tool: "list_subjects", ArgumentsJSON: `{}`}}, nil
}

// askQueued asks text and wants it queued at once (the Driver must not wait for the running step).
func (e *queueEnv) askQueued(t *testing.T, in agentturn.AskInput) agentturn.Queued {
	t.Helper()
	type res struct {
		q   agentturn.Queued
		err error
	}
	got := make(chan res, 1)
	go func() {
		q, err := e.drv.Ask(context.Background(), in)
		got <- res{q, err}
	}()
	select {
	case r := <-got:
		if r.err != nil || r.q.Ticket == "" || r.q.Thread == "" {
			t.Fatalf("Ask %q = %+v, %v, want queued", in.Text, r.q, r.err)
		}
		return r.q
	case <-time.After(5 * time.Second):
		t.Fatal("Ask waited for the running request")
	}
	return agentturn.Queued{}
}

// turnUpdates are the updates of request turn of th1, in order.
func (e *queueEnv) turnUpdates(turn int) []Update {
	var out []Update
	for _, u := range e.sink.all() {
		if u.Thread == "th1" && u.Turn == turn && !u.Compaction {
			out = append(out, u)
		}
	}
	return out
}

// A question asked while a Hub step runs gets its ticket synchronously; after the loop of the current request ends it
// starts as the next request, and the first update of that request (its progress) carries the ticket.
func TestDriverStartsAQueuedQuestionAfterTheLoop(t *testing.T) {
	e := newQueueEnv(t)
	entered, release := make(chan struct{}), make(chan struct{})
	e.model.then(func(context.Context) ([]values.RawCall, error) {
		close(entered)
		<-release
		return listSubjects(context.Background())
	}).reply("один").reply("два")

	if q, err := e.drv.Ask(t.Context(), agentturn.AskInput{Text: "первый"}); err != nil || q.Ticket != "" {
		t.Fatalf("first Ask = %+v, %v", q, err)
	}
	<-entered
	q := e.askQueued(t, agentturn.AskInput{Text: "второй"})
	if q.Thread != "th1" {
		t.Fatalf("queued in %q", q.Thread)
	}
	close(release)
	e.drv.Wait()

	if got := e.model.texts(); !slices.Equal(got, []string{"первый", "первый", "второй"}) {
		t.Fatalf("texts sent = %q", got)
	}
	ups := e.turnUpdates(2)
	if len(ups) == 0 || ups[0].Ticket != string(q.Ticket) || !ups[0].Progress {
		t.Fatalf("updates of the started question = %+v", ups)
	}
	last := ups[len(ups)-1]
	if last.Status != thread.StatusDone || last.Ticket != string(q.Ticket) || last.Queued {
		t.Fatalf("last update = %+v", last)
	}
	for _, u := range e.turnUpdates(1) {
		if u.Ticket != "" || u.Queued {
			t.Fatalf("an update of the first request carries a ticket: %+v", u)
		}
	}
}

// «Стоп» of the current request does not cancel the waiting question: it starts once the last loop of the chat ended
// (the loop of the stopped request closes its step after the Stop loop).
func TestDriverStopKeepsTheQueue(t *testing.T) {
	e := newQueueEnv(t)
	e.reads.library.block, e.reads.library.started = true, make(chan struct{})
	e.model.then(listSubjects).reply("два")

	if _, err := e.drv.Ask(t.Context(), agentturn.AskInput{Text: "первый"}); err != nil {
		t.Fatal(err)
	}
	<-e.reads.library.started // the read of the first request runs
	q := e.askQueued(t, agentturn.AskInput{Text: "второй"})
	if err := e.drv.Stop(t.Context(), agentturn.TurnRef{Thread: "th1", Turn: 1}); err != nil {
		t.Fatal(err)
	}
	e.drv.Wait()

	if got := e.model.texts(); !slices.Equal(got, []string{"первый", "второй"}) {
		t.Fatalf("texts sent = %q", got)
	}
	if ups := e.turnUpdates(2); len(ups) == 0 || ups[len(ups)-1].Status != thread.StatusDone || ups[0].Ticket != string(q.Ticket) {
		t.Fatalf("updates of the started question = %+v", ups)
	}
	stopped, err := e.store.Get(t.Context(), "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := stopped.Turn(); turn.Number() != 2 {
		t.Fatalf("current request = %d", turn.Number())
	}
}

// The app closes while a question waits: the loop of the current request ends, nothing starts, the question is never
// sent; a later Ask is refused.
func TestDriverCloseDropsTheQueue(t *testing.T) {
	e := newQueueEnv(t)
	entered := make(chan struct{})
	e.model.then(func(ctx context.Context) ([]values.RawCall, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}).reply("никогда")

	if _, err := e.drv.Ask(t.Context(), agentturn.AskInput{Text: "первый"}); err != nil {
		t.Fatal(err)
	}
	<-entered
	e.askQueued(t, agentturn.AskInput{Text: "второй"})
	e.drv.Close()

	if got := e.model.texts(); !slices.Equal(got, []string{"первый"}) {
		t.Fatalf("texts sent = %q", got)
	}
	if !e.turns.HasQueued("th1") || len(e.turnUpdates(2)) != 0 {
		t.Fatal("the waiting question started after Close")
	}
	if _, err := e.drv.Ask(t.Context(), agentturn.AskInput{Text: "третий"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ask after Close = %v", err)
	}
}

// A request left running with no loop (TASK-171) does not hold the queue: the drain recovers it once and starts the
// waiting question.
func TestDriverDrainRecoversAStuckRequest(t *testing.T) {
	e := newQueueEnv(t)
	th, err := thread.New("th1", now)
	if err == nil {
		err = th.Ask(thread.Request{Text: "застрял"}, now)
	}
	if err == nil {
		err = e.store.Save(t.Context(), th)
	}
	if err != nil {
		t.Fatal(err)
	}
	e.model.reply("ответ")

	q := e.askQueued(t, agentturn.AskInput{ThreadID: "th1", Text: "второй"})
	e.drv.Wait()

	if got := e.model.texts(); !slices.Equal(got, []string{"второй"}) {
		t.Fatalf("texts sent = %q", got)
	}
	ups := e.turnUpdates(2)
	if len(ups) == 0 || ups[len(ups)-1].Status != thread.StatusDone || ups[len(ups)-1].Ticket != string(q.Ticket) {
		t.Fatalf("updates of the started question = %+v", ups)
	}
}

// An Ask that the step use case queued once it ran (the chat became busy after Enqueue) publishes the waiting
// question: no status, no cards; Withdraw goes straight to the use case.
func TestDriverPublishesAQuestionQueuedLate(t *testing.T) {
	turns := (&scriptedTurns{}).then(agentturn.Step{Thread: "th1", Queued: true, Ticket: "q3"}, nil)
	d, sink := newDriver(t, newReads(), turns)
	if q, err := d.Ask(t.Context(), agentturn.AskInput{Text: "второй"}); err != nil || q.Ticket != "" {
		t.Fatalf("Ask = %+v, %v", q, err)
	}
	d.Wait()
	ups := sink.all()
	if len(ups) != 1 || !ups[0].Queued || ups[0].Ticket != "q3" || ups[0].Thread != "th1" || ups[0].Status != "" || len(ups[0].Cards) != 0 {
		t.Fatalf("updates = %+v", ups)
	}
	if r := d.Withdraw("th1", "q3"); r != agentturn.WithdrawWithdrawn || !slices.Equal(turns.withdrawn, []agentturn.Ticket{"q3"}) {
		t.Fatalf("Withdraw = %s, %v", r, turns.withdrawn)
	}
}

// A queued Ask returns at once with no Hub call; a drain that finds nothing to start ends quietly and does not drain
// again by itself.
func TestDriverQueuedAskAndAnIdleDrain(t *testing.T) {
	turns := &scriptedTurns{
		enqueue: func(agentturn.AskInput) (agentturn.Queued, bool, error) {
			return agentturn.Queued{Thread: "th1", Ticket: "q1"}, true, nil
		},
		waiting: map[shared.ThreadID]bool{"th1": true},
	}
	d, sink := newDriver(t, newReads(), turns)
	q, err := d.Ask(t.Context(), agentturn.AskInput{Text: "второй"})
	if err != nil || q != (agentturn.Queued{Thread: "th1", Ticket: "q1"}) {
		t.Fatalf("Ask = %+v, %v", q, err)
	}
	d.Wait()
	turns.mu.Lock()
	defer turns.mu.Unlock()
	if turns.asks != 0 || !slices.Equal(turns.starts, []shared.ThreadID{"th1"}) || len(sink.all()) != 0 {
		t.Fatalf("asks %d, starts %v, updates %+v", turns.asks, turns.starts, sink.all())
	}
}

// awaitingTurns are scripted turns whose chats wait for the student: no waiting question is ready to start.
type awaitingTurns struct {
	*scriptedTurns
}

func (awaitingTurns) QueueReady(context.Context, shared.ThreadID) bool { return false }

// Two chats each hold a waiting question behind a request that waits for the student: their idle drains overlap, and
// the end of one does not wake the other, so each chat gets one StartQueued; nothing starts and nothing is compacted.
func TestDriverIdleDrainsOfTwoChatsDoNotWakeEachOther(t *testing.T) {
	const chatA, chatB = shared.ThreadID("tha"), shared.ThreadID("thb")
	both := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	first := map[shared.ThreadID]bool{}
	turns := &scriptedTurns{
		enqueue: func(in agentturn.AskInput) (agentturn.Queued, bool, error) {
			return agentturn.Queued{Thread: in.ThreadID, Ticket: agentturn.Ticket("q" + string(in.ThreadID))}, true, nil
		},
		waiting: map[shared.ThreadID]bool{chatA: true, chatB: true},
	}
	turns.startQueued = func(id shared.ThreadID) (agentturn.Step, error) {
		mu.Lock()
		first[id] = true
		if len(first) == 2 {
			once.Do(func() { close(both) })
		}
		mu.Unlock()
		select { // the first drains of both chats run at the same time
		case <-both:
		case <-time.After(5 * time.Second):
		}
		return agentturn.Step{}, agentturn.ErrNothingQueued
	}
	compactor := &fakeCompactor{run: compacted(agentcompact.Fill{Level: thread.FillNormal})}
	sink := &chanSink{}
	deps := newReads().deps(awaitingTurns{turns}, sink)
	deps.Compactor = compactor
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)

	for _, id := range []shared.ThreadID{chatA, chatB} {
		if q, err := d.Ask(t.Context(), agentturn.AskInput{ThreadID: id, Text: "потом"}); err != nil || q.Thread != id {
			t.Fatalf("Ask in %s = %+v, %v", id, q, err)
		}
	}
	d.Wait()

	turns.mu.Lock()
	starts := slices.Clone(turns.starts)
	turns.mu.Unlock()
	for _, id := range []shared.ThreadID{chatA, chatB} {
		if n := countOf(starts, id); n != 1 { // the end counter before the fix let one of them drain again
			t.Fatalf("StartQueued of %s ran %d times: %v", id, n, starts)
		}
	}
	if ups := sink.all(); len(ups) != 0 {
		t.Fatalf("updates = %+v", ups)
	}
	if got := compactor.calls(); len(got) != 0 {
		t.Fatalf("compacted %v while nothing could start", got)
	}
}

// A drain whose question can start compacts the chat first, as before an Ask.
func TestDriverReadyDrainCompactsFirst(t *testing.T) {
	turns := &scriptedTurns{
		enqueue: func(agentturn.AskInput) (agentturn.Queued, bool, error) {
			return agentturn.Queued{Thread: th1, Ticket: "q1"}, true, nil
		},
		waiting: map[shared.ThreadID]bool{th1: true},
	}
	turns.startQueued = func(id shared.ThreadID) (agentturn.Step, error) {
		turns.mu.Lock()
		turns.waiting[id] = false
		turns.mu.Unlock()
		return agentturn.Step{Thread: id, Turn: 2, Status: thread.StatusDone, Ticket: "q1"}, nil
	}
	compactor := &fakeCompactor{run: compacted(agentcompact.Fill{Level: thread.FillNormal})}
	deps := newReads().deps(turns, &chanSink{})
	deps.Compactor = compactor
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)

	if _, err := d.Ask(t.Context(), agentturn.AskInput{ThreadID: th1, Text: "потом"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if got := compactor.calls(); !slices.Equal(got, []shared.ThreadID{th1}) {
		t.Fatalf("compacted %v", got)
	}
}

func countOf(ids []shared.ThreadID, id shared.ThreadID) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// gatedAsk holds Ask until gate closes, outside the lock of the scripted turns.
type gatedAsk struct {
	*scriptedTurns
	gate chan struct{}
}

func (g gatedAsk) Ask(ctx context.Context, in agentturn.AskInput) (agentturn.Step, error) {
	<-g.gate
	return g.scriptedTurns.Ask(ctx, in)
}

// A loop that has not named its chat yet holds the drain back, as it may act on the same chat; when it ends (naming
// another chat) the held drain runs, so the waiting question is not left behind.
func TestDriverDrainHeldByAnUnnamedLoop(t *testing.T) {
	turns := (&scriptedTurns{
		enqueue: func(in agentturn.AskInput) (agentturn.Queued, bool, error) {
			return agentturn.Queued{Thread: "th1", Ticket: "q1"}, in.ThreadID == "th1", nil
		},
		waiting: map[shared.ThreadID]bool{"th1": true},
	}).then(agentturn.Step{Thread: "th2", Turn: 1, Status: thread.StatusDone}, nil)
	turns.startQueued = func(id shared.ThreadID) (agentturn.Step, error) {
		turns.mu.Lock()
		turns.waiting[id] = false
		turns.mu.Unlock()
		return agentturn.Step{Thread: id, Turn: 2, Status: thread.StatusDone, Ticket: "q1"}, nil
	}
	g := gatedAsk{scriptedTurns: turns, gate: make(chan struct{})}
	d, sink := newDriver(t, newReads(), g)

	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "в текущий чат"}); err != nil {
		t.Fatal(err)
	}
	if q, err := d.Ask(t.Context(), agentturn.AskInput{ThreadID: "th1", Text: "в первый"}); err != nil || q.Ticket != "q1" {
		t.Fatalf("queued Ask = %+v, %v", q, err)
	}
	turns.mu.Lock()
	early := len(turns.starts)
	turns.mu.Unlock()
	if early != 0 {
		t.Fatal("the drain ran beside a loop that may act on the same chat")
	}
	close(g.gate)
	d.Wait()

	turns.mu.Lock()
	starts := slices.Clone(turns.starts)
	turns.mu.Unlock()
	if !slices.Equal(starts, []shared.ThreadID{"th1"}) {
		t.Fatalf("starts = %v", starts)
	}
	ups := sink.all()
	if last := ups[len(ups)-1]; last.Thread != "th1" || last.Ticket != "q1" || last.Status != thread.StatusDone {
		t.Fatalf("updates = %+v", ups)
	}
}

// A full queue (or any refusal of Enqueue) goes the usual way, which reports busy.
func TestDriverFullQueueIsBusy(t *testing.T) {
	busy := fmt.Errorf("agentturn: enqueue: %w", thread.ErrTurnInProgress)
	turns := (&scriptedTurns{
		enqueue: func(agentturn.AskInput) (agentturn.Queued, bool, error) { return agentturn.Queued{}, false, busy },
	}).then(agentturn.Step{}, busy)
	d, sink := newDriver(t, newReads(), turns)
	if _, err := d.Ask(t.Context(), agentturn.AskInput{Text: "шестой"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if ups := sink.all(); len(ups) != 1 || ups[0].Problem != ProblemBusy || ups[0].Queued {
		t.Fatalf("updates = %+v", ups)
	}
}
