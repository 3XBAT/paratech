package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

const freeTopic = "Реформы Петра I, презентация"

func mustScope(t *testing.T, subject string) values.Scope {
	t.Helper()
	var sub values.Ref
	if subject != "" {
		sub = mustRef(t, values.KindSubject, subject)
	}
	s, err := values.NewScope(sub, "", values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type memThreads map[shared.ThreadID]*thread.Thread

func (m memThreads) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	th, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("thread %s: %w", id, shared.ErrNotFound)
	}
	return th, nil
}

type memHistory map[shared.ThreadID][]values.HistoryEntry

func (m memHistory) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	var out []values.HistoryEntry
	for _, e := range m[id] {
		if e.Turn >= from {
			out = append(out, e)
		}
	}
	return out[max(0, len(out)-limit):], nil
}

// memNotice is the settings use case: the card setting and its changes.
type memNotice struct {
	on   bool
	sets []bool
	err  error
}

func (n *memNotice) Get(context.Context) (preferences.Toggles, error) {
	return preferences.Toggles{FreeWorkNotice: n.on}, n.err
}

func (n *memNotice) SetFreeWorkNotice(_ context.Context, on bool) (preferences.Toggles, error) {
	if n.err != nil {
		return preferences.Toggles{}, n.err
	}
	n.sets = append(n.sets, on)
	n.on = on
	return preferences.Toggles{FreeWorkNotice: on}, nil
}

// waitingCall is the history of request turn in which call id of a topic waits on the free-work card.
func waitingCall(t *testing.T, turn int, id, tool, args string) []values.HistoryEntry {
	t.Helper()
	res, err := values.FreeWorkResult(id)
	if err != nil {
		t.Fatal(err)
	}
	raw := values.RawCall{CallID: id, Tool: tool, ArgumentsJSON: args}
	return []values.HistoryEntry{
		{Turn: turn, Entry: values.HistoryTurn{StudentText: "сделай презентацию про Петра"}},
		{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{Calls: []values.RawCall{raw}, Seal: []byte("seal")}}},
		{Turn: turn, Entry: values.HistoryTurn{Results: []values.ToolResult{res}}},
	}
}

type freeWorld struct {
	*world
	turnkey *fakeTurnkey
	notice  *memNotice
	free    *FreeWorks
}

// newFreeWorld has thread th1 of subject s1 whose request 2 has call c1 waiting on the card, and thread th2 «Без
// предмета» whose request 1 has call c7 waiting.
func newFreeWorld(t *testing.T) *freeWorld {
	t.Helper()
	w, f := withTurnkey(t, turnkey.Started{View: questionsView(2), Created: true}, nil)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	th1, err := thread.New("th1", thread.Key("s1"), now)
	if err != nil {
		t.Fatal(err)
	}
	th2, err := thread.New("th2", thread.NoSubject, now)
	if err != nil {
		t.Fatal(err)
	}
	args := `{"topic":"` + freeTopic + `"}`
	history := memHistory{
		"th1": waitingCall(t, 2, "c1", "triage_assignment", args),
		"th2": waitingCall(t, 1, "c7", "triage_assignment", args),
	}
	n := &memNotice{on: true}
	free, err := NewFreeWorks(FreeWorkDeps{
		Threads: memThreads{"th1": th1, "th2": th2}, History: history, Journal: w.journal, Actions: w.actions, Settings: n,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &freeWorld{world: w, turnkey: f, notice: n, free: free}
}

var card1 = values.FreeWorkCard{Thread: "th1", Turn: 2, CallID: "c1"}

// «Сделать» starts one work with the topic of the stored call and the thread's subject, journaled with Undo; a
// second «Сделать» returns the same work without another reader call or journal entry.
func TestFreeWorkAccept(t *testing.T) {
	fw := newFreeWorld(t)
	ctx := t.Context()
	if got, err := fw.free.FreeWorkCard(ctx, card1); err != nil || got.State != values.FreeWorkWaiting {
		t.Fatalf("card before = %+v, %v", got, err)
	}

	got, err := fw.free.FreeWork(ctx, card1, true, false)
	want := values.FreeWorkOutcome{State: values.FreeWorkDone, Work: "w1"}
	if err != nil || got != want {
		t.Fatalf("FreeWork = %+v, %v", got, err)
	}
	key := "ag:th1:2:c1"
	if len(fw.turnkey.topics) != 1 || fw.turnkey.topics[0] != (topicCall{request: key, text: freeTopic, subject: "s1"}) {
		t.Fatalf("topic calls = %+v", fw.turnkey.topics)
	}

	again, err := fw.free.FreeWork(ctx, card1, true, false)
	if err != nil || again != want || fw.turnkey.paidFor[key] != 1 || len(fw.store.order) != 1 {
		t.Fatalf("second «Сделать» = %+v, %v; reader calls %d, entries %d", again, err, fw.turnkey.paidFor[key], len(fw.store.order))
	}
	e, err := fw.journal.Find(ctx, "th1", 2, "c1")
	if err != nil || e.Status != action.StatusDone || e.Tool != values.ToolTriageAssignment {
		t.Fatalf("journal = %+v, %v", e, err)
	}
	if got, err := fw.free.FreeWorkCard(ctx, card1); err != nil || got != want {
		t.Fatalf("card after = %+v, %v", got, err)
	}
	if len(fw.notice.sets) != 0 {
		t.Fatalf("the setting changed: %v", fw.notice.sets)
	}

	if u, err := fw.journal.Undo(ctx, e.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	if got, err := fw.free.FreeWorkCard(ctx, card1); err != nil || got.State != values.FreeWorkUndone || got.Work != "w1" {
		t.Fatalf("card after Undo = %+v, %v", got, err)
	}
}

// A thread «Без предмета» gives a work without a subject.
func TestFreeWorkWithoutSubject(t *testing.T) {
	fw := newFreeWorld(t)
	if _, err := fw.free.FreeWork(t.Context(), values.FreeWorkCard{Thread: "th2", Turn: 1, CallID: "c7"}, true, false); err != nil {
		t.Fatal(err)
	}
	if len(fw.turnkey.topics) != 1 || fw.turnkey.topics[0].subject != "" || fw.turnkey.topics[0].request != "ag:th2:1:c7" {
		t.Fatalf("topic calls = %+v", fw.turnkey.topics)
	}
}

// «Не надо» stores nothing and asks nothing; «Больше не показывать» only changes the setting; a later «Сделать» on
// the same card still runs once.
func TestFreeWorkDecline(t *testing.T) {
	fw := newFreeWorld(t)
	ctx := t.Context()
	for _, dontShow := range []bool{false, true} {
		got, err := fw.free.FreeWork(ctx, card1, false, dontShow)
		if err != nil || got.State != values.FreeWorkWaiting {
			t.Fatalf("«Не надо» (dontShow %v) = %+v, %v", dontShow, got, err)
		}
	}
	if len(fw.turnkey.topics) != 0 || len(fw.store.order) != 0 {
		t.Fatalf("«Не надо» ran: %+v, %d entries", fw.turnkey.topics, len(fw.store.order))
	}
	if fw.notice.on || len(fw.notice.sets) != 1 || fw.notice.sets[0] {
		t.Fatalf("setting = %v, sets %v", fw.notice.on, fw.notice.sets)
	}
	if got, err := fw.free.FreeWork(ctx, card1, true, false); err != nil || got.Work != "w1" || len(fw.turnkey.topics) != 1 {
		t.Fatalf("«Сделать» after «Не надо» = %+v, %v", got, err)
	}
}

// A card that names no call waiting on it in its own thread is not found and runs nothing.
func TestFreeWorkRefusesOtherCards(t *testing.T) {
	tests := []struct {
		name string
		card values.FreeWorkCard
	}{
		{"a call of another thread", values.FreeWorkCard{Thread: "th1", Turn: 1, CallID: "c7"}},
		{"the right call under another thread", values.FreeWorkCard{Thread: "th2", Turn: 2, CallID: "c1"}},
		{"a made-up call", values.FreeWorkCard{Thread: "th1", Turn: 2, CallID: "c9"}},
		{"another request", values.FreeWorkCard{Thread: "th1", Turn: 1, CallID: "c1"}},
		{"a made-up thread", values.FreeWorkCard{Thread: "th9", Turn: 2, CallID: "c1"}},
		{"a malformed card", values.FreeWorkCard{Thread: "th1", Turn: 2, CallID: "c 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fw := newFreeWorld(t)
			if _, err := fw.free.FreeWork(t.Context(), tt.card, true, true); !errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("FreeWork = %v, want not found", err)
			}
			if _, err := fw.free.FreeWorkCard(t.Context(), tt.card); !errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("FreeWorkCard = %v, want not found", err)
			}
			if len(fw.turnkey.topics) != 0 || len(fw.store.order) != 0 || len(fw.notice.sets) != 0 {
				t.Fatalf("a refused card acted: %+v, %d entries, sets %v", fw.turnkey.topics, len(fw.store.order), fw.notice.sets)
			}
		})
	}
}

// A call that did not wait on the card (it ran, or was an assignment) is never run by «Сделать».
func TestFreeWorkOnlyForAWaitingTopic(t *testing.T) {
	fw := newFreeWorld(t)
	h := fw.free.history.(memHistory)
	h["th1"] = append(h["th1"], waitingCall(t, 3, "c2", "triage_assignment", `{"assignment":"a1"}`)...)
	ran := waitingCall(t, 4, "c3", "triage_assignment", `{"topic":"x"}`)
	ran[2].Entry.Results[0] = values.ToolResult{CallID: "c3", ResultJSON: `{"work":"w1"}`}
	h["th1"] = append(h["th1"], ran...)
	for _, c := range []values.FreeWorkCard{{Thread: "th1", Turn: 3, CallID: "c2"}, {Thread: "th1", Turn: 4, CallID: "c3"}} {
		if _, err := fw.free.FreeWork(t.Context(), c, true, false); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if len(fw.turnkey.topics)+len(fw.turnkey.calls) != 0 {
		t.Fatal("a triage ran")
	}
}

func TestFreeWorkFailures(t *testing.T) {
	t.Run("no turnkey", func(t *testing.T) {
		fw := newFreeWorld(t)
		fw.actions.turnkey = nil
		if _, err := fw.free.FreeWork(t.Context(), card1, true, false); !errors.Is(err, shared.ErrServiceNotConfigured) {
			t.Fatalf("FreeWork = %v", err)
		}
	})
	t.Run("the setting cannot be stored", func(t *testing.T) {
		fw := newFreeWorld(t)
		fw.notice.err = errors.New("disk")
		if _, err := fw.free.FreeWork(t.Context(), card1, true, true); err == nil || len(fw.turnkey.topics) != 0 {
			t.Fatalf("FreeWork = %v, topic calls %d", err, len(fw.turnkey.topics))
		}
	})
	t.Run("the reader refused the key", func(t *testing.T) {
		fw := newFreeWorld(t)
		fw.turnkey.started, fw.turnkey.err = turnkey.Started{}, turnkey.ErrInvalidRequest
		got, err := fw.free.FreeWork(t.Context(), card1, true, false)
		if err != nil || got != (values.FreeWorkOutcome{State: values.FreeWorkFailed}) {
			t.Fatalf("FreeWork = %+v, %v", got, err)
		}
		if card, err := fw.free.FreeWorkCard(t.Context(), card1); err != nil || card != got {
			t.Fatalf("card = %+v, %v; FreeWork said %+v", card, err, got)
		}
	})
	// The reader found a work of the key that this call did not create (journal FAILED): FreeWork says what the card
	// says, never «done» with a work Undo cannot remove.
	t.Run("an existing work", func(t *testing.T) {
		fw := newFreeWorld(t)
		fw.turnkey.started.Created = false
		got, err := fw.free.FreeWork(t.Context(), card1, true, false)
		if err != nil || got.State != values.FreeWorkFailed {
			t.Fatalf("FreeWork = %+v, %v", got, err)
		}
		if card, err := fw.free.FreeWorkCard(t.Context(), card1); err != nil || card != got {
			t.Fatalf("card = %+v, %v; FreeWork said %+v", card, err, got)
		}
	})
}

// The Driver answers a card admission with the free-work card and {"status":"awaiting_student"}: nothing runs or is
// journaled, the topic goes nowhere.
func TestFreeWorkCardOfTheDriver(t *testing.T) {
	w, f := withTurnkey(t, turnkey.Started{View: questionsView(2), Created: true}, nil)
	c := writeCall(t, "c1", values.ToolTriageAssignment, `{"topic":"`+freeTopic+`"}`)
	c.Class, c.Paid = actionpolicy.ClassUI, false
	r := w.tools(t).run(t.Context(), req, c)
	if r.out.Result.ResultJSON != `{"status":"awaiting_student"}` || r.out.Result.IsError || r.write {
		t.Fatalf("outcome = %+v", r)
	}
	if len(r.cards) != 1 || r.cards[0] != (values.FreeWorkCard{Thread: req.Thread, Turn: req.Turn, CallID: "c1"}) {
		t.Fatalf("cards = %+v", r.cards)
	}
	if len(f.topics) != 0 || len(w.store.order) != 0 {
		t.Fatalf("ran: %+v, %d entries", f.topics, len(w.store.order))
	}

	// Another tool admitted as a card is not run either.
	other := writeCall(t, "c2", values.ToolTriageAssignment, `{"assignment":"a1"}`)
	other.Class = actionpolicy.ClassUI
	if r := w.tools(t).run(t.Context(), req, other); r.out.Result.ResultJSON != `{"error":"unavailable"}` || len(r.cards) != 0 {
		t.Fatalf("other card admission = %+v", r)
	}
}

// Facts report the card setting for a topic only; without the setting wired the card is on.
func TestFactsOfAFreeTopic(t *testing.T) {
	topic := mustCall(t, "c1", values.ToolTriageAssignment, `{"topic":"`+freeTopic+`"}`)
	base, err := NewFacts(&memLessons{})
	if err != nil {
		t.Fatal(err)
	}
	if f, err := base.Facts(t.Context(), topic); err != nil || !f.FreeWorkNotice {
		t.Fatalf("unwired = %+v, %v", f, err)
	}
	n := &memNotice{}
	f, err := base.WithNotice(n).Facts(t.Context(), topic)
	if err != nil || f.FreeWorkNotice {
		t.Fatalf("off = %+v, %v", f, err)
	}
	n.on = true
	if f, err = base.WithNotice(n).Facts(t.Context(), topic); err != nil || !f.FreeWorkNotice {
		t.Fatalf("on = %+v, %v", f, err)
	}
	assignment := mustCall(t, "c2", values.ToolTriageAssignment, `{"assignment":"a1"}`)
	if f, err = base.WithNotice(n).Facts(t.Context(), assignment); err != nil || f.FreeWorkNotice {
		t.Fatalf("assignment = %+v, %v", f, err)
	}
	n.err = errors.New("disk")
	if _, err := base.WithNotice(n).Facts(t.Context(), topic); err == nil {
		t.Fatal("a failed read was not reported")
	}
}

// The deps are checked.
func TestNewFreeWorksNeedsDeps(t *testing.T) {
	w := newWorld(t)
	full := FreeWorkDeps{Threads: memThreads{}, History: memHistory{}, Journal: w.journal, Actions: w.actions, Settings: &memNotice{}}
	if _, err := NewFreeWorks(full); err != nil {
		t.Fatal(err)
	}
	for i, d := range []FreeWorkDeps{
		{History: full.History, Journal: full.Journal, Actions: full.Actions, Settings: full.Settings},
		{Threads: full.Threads, Journal: full.Journal, Actions: full.Actions, Settings: full.Settings},
		{Threads: full.Threads, History: full.History, Actions: full.Actions, Settings: full.Settings},
		{Threads: full.Threads, History: full.History, Journal: full.Journal, Settings: full.Settings},
		{Threads: full.Threads, History: full.History, Journal: full.Journal, Actions: full.Actions},
	} {
		if _, err := NewFreeWorks(d); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("deps %d: %v", i, err)
		}
	}
}
