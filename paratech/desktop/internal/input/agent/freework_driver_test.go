package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// turnStore keeps the threads and the history of the real step use case in memory.
type turnStore struct {
	mu      sync.Mutex
	threads map[shared.ThreadID]*thread.Thread
	history map[shared.ThreadID][]values.HistoryEntry
	next    int
}

func (s *turnStore) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if th, ok := s.threads[id]; ok {
		return th, nil
	}
	return nil, fmt.Errorf("thread %s: %w", id, shared.ErrNotFound)
}

// Current is the unarchived thread active last (ties to the greater id), as the sqlite adapter.
func (s *turnStore) Current(context.Context) (*thread.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur *thread.Thread
	for _, th := range s.threads {
		if th.Archived() {
			continue
		}
		if cur == nil || th.LastActive().After(cur.LastActive()) || (th.LastActive().Equal(cur.LastActive()) && th.ID() > cur.ID()) {
			cur = th
		}
	}
	if cur == nil {
		return nil, fmt.Errorf("current chat: %w", shared.ErrNotFound)
	}
	return cur, nil
}

func (s *turnStore) Save(_ context.Context, th *thread.Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[th.ID()] = th
	return nil
}

func (s *turnStore) ListRunning(context.Context) ([]*thread.Thread, error) { return nil, nil }

func (s *turnStore) Append(_ context.Context, id shared.ThreadID, turn int, entry values.HistoryTurn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history[id] = append(s.history[id], values.HistoryEntry{Turn: turn, Entry: entry})
	return nil
}

func (s *turnStore) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []values.HistoryEntry
	for _, e := range s.history[id] {
		if e.Turn >= from {
			out = append(out, e)
		}
	}
	return out[max(0, len(out)-limit):], nil
}

func (s *turnStore) NewThreadID() shared.ThreadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return shared.ThreadID(fmt.Sprintf("th%d", s.next))
}

// scriptModel answers each planner step with the next calls of its script and keeps the requests.
type scriptModel struct {
	mu    sync.Mutex
	steps [][]values.RawCall
	reqs  []values.StepRequest
}

func (m *scriptModel) Step(_ context.Context, req values.StepRequest) (values.StepReply, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	if len(m.steps) == 0 {
		return values.StepReply{}, fmt.Errorf("script: no step %d", len(m.reqs))
	}
	calls := m.steps[0]
	m.steps = m.steps[1:]
	return values.StepReply{Calls: values.SealedCalls{Calls: calls, Seal: []byte("seal")}}, nil
}

type noSubjects struct{}

func (noSubjects) BaseSubjects(context.Context) ([]values.BaseSubject, error) { return nil, nil }

// With the free-work card turned off («Больше не показывать»), the real step use case and the Driver run a topic call
// at once as the request's one paid write without a plan (journaled, a work started); a second paid write of the same
// request needs a plan and runs nothing.
func TestFreeTopicWithoutTheCardThroughTheDriver(t *testing.T) {
	w, f := withTurnkey(t, turnkey.Started{View: questionsView(2), Created: true}, nil)
	facts, err := NewFacts(&memLessons{})
	if err != nil {
		t.Fatal(err)
	}
	store := &turnStore{threads: map[shared.ThreadID]*thread.Thread{}, history: map[shared.ThreadID][]values.HistoryEntry{}}
	model := &scriptModel{steps: [][]values.RawCall{
		{{CallID: "c1", Tool: "triage_assignment", ArgumentsJSON: `{"topic":"` + freeTopic + `"}`}},
		{{CallID: "c2", Tool: "triage_assignment", ArgumentsJSON: `{"topic":"Реформы Екатерины II"}`}},
		{{CallID: "r1", Tool: "reply", ArgumentsJSON: `{"text":"Готово"}`}},
	}}
	turns, err := agentturn.New(agentturn.Deps{
		Model: model, Threads: store, History: store, Subjects: noSubjects{},
		Facts: facts.WithNotice(&memNotice{on: false}), Tx: &directTx{}, Clock: fixedClock{}, IDs: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &chanSink{}
	drv, err := New(w.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drv.Close)

	if err := drv.Ask(t.Context(), agentturn.AskInput{Text: "сделай презентацию про Петра"}); err != nil {
		t.Fatal(err)
	}
	drv.Wait()
	ups := sink.all()
	if len(ups) == 0 || ups[len(ups)-1].Status != thread.StatusDone {
		t.Fatalf("updates = %+v", ups)
	}
	last := ups[len(ups)-1]

	if len(f.topics) != 1 || f.topics[0].text != freeTopic {
		t.Fatalf("topic calls = %+v, want only the first", f.topics)
	}
	e, err := w.journal.Find(t.Context(), last.Thread, last.Turn, "c1")
	if err != nil || e.Status != action.StatusDone || e.Tool != values.ToolTriageAssignment {
		t.Fatalf("journal c1 = %+v, %v", e, err)
	}
	if _, err := w.journal.Find(t.Context(), last.Thread, last.Turn, "c2"); err == nil {
		t.Fatal("the second paid write was journaled")
	}

	got := map[string]values.ToolResult{}
	for _, h := range model.reqs[len(model.reqs)-1].History {
		for _, r := range h.Results {
			got[r.CallID] = r
		}
	}
	if why, refused := values.RefusalOf(got["c1"]); refused {
		t.Fatalf("c1 refused: %s", why)
	}
	if why, refused := values.RefusalOf(got["c2"]); !refused || why != values.RefusePlanRequired {
		t.Fatalf("c2 result = %+v, want plan_required", got["c2"])
	}
}
