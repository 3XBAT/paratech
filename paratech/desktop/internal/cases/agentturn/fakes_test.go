package agentturn

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

var moscow = time.FixedZone("Europe/Moscow", 3*3600)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { c.now = c.now.Add(time.Second); return c.now }

type seqIDs struct{ n int }

func (g *seqIDs) NewThreadID() shared.ThreadID { g.n++; return shared.ThreadID(fmt.Sprintf("th%d", g.n)) }

// fakeModel answers steps from a script; each entry is a reply or an error. It records every request.
type fakeModel struct {
	mu      sync.Mutex
	script  []func(req values.StepRequest) (values.StepReply, error)
	reqs    []values.StepRequest
	replies map[string]values.StepReply // by key: a repeated key is replayed like the Hub does
}

func (m *fakeModel) Step(_ context.Context, req values.StepRequest) (values.StepReply, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	if r, ok := m.replies[req.Key]; ok {
		m.mu.Unlock()
		r.Replayed = true
		return r, nil
	}
	if len(m.script) == 0 {
		m.mu.Unlock()
		return values.StepReply{}, fmt.Errorf("fake model: no scripted step for %s", req.Key)
	}
	next := m.script[0]
	m.script = m.script[1:]
	m.mu.Unlock()

	r, err := next(req)
	if err == nil {
		m.mu.Lock()
		if m.replies == nil {
			m.replies = map[string]values.StepReply{}
		}
		m.replies[req.Key] = r
		m.mu.Unlock()
	}
	return r, err
}

func (m *fakeModel) then(calls ...values.RawCall) *fakeModel {
	m.script = append(m.script, func(values.StepRequest) (values.StepReply, error) { return reply(calls...), nil })
	return m
}

func (m *fakeModel) fail(err error) *fakeModel {
	m.script = append(m.script, func(values.StepRequest) (values.StepReply, error) { return values.StepReply{}, err })
	return m
}

func (m *fakeModel) do(f func(values.StepRequest) (values.StepReply, error)) *fakeModel {
	m.script = append(m.script, f)
	return m
}

func (m *fakeModel) requests() []values.StepRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.reqs)
}

func (m *fakeModel) last(t *testing.T) values.StepRequest {
	t.Helper()
	reqs := m.requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the model")
	}
	return reqs[len(reqs)-1]
}

func reply(calls ...values.RawCall) values.StepReply {
	return values.StepReply{Calls: values.SealedCalls{Calls: calls, Seal: []byte("seal")}, InputTokens: 100, OutputTokens: 20}
}

func raw(id, tool, args string) values.RawCall {
	return values.RawCall{CallID: id, Tool: tool, ArgumentsJSON: args}
}

// memThreads keeps stored states and restores them on every read, like the database does.
type memThreads struct {
	mu     sync.Mutex
	states map[shared.ThreadID]thread.State
}

func (r *memThreads) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[id]
	if !ok {
		return nil, fmt.Errorf("thread %s: %w", id, shared.ErrNotFound)
	}
	return thread.Restore(st)
}

func (r *memThreads) GetByKey(ctx context.Context, key thread.Key) (*thread.Thread, error) {
	r.mu.Lock()
	var id shared.ThreadID
	for _, st := range r.states {
		if st.Key == key {
			id = st.ID
		}
	}
	r.mu.Unlock()
	if id == "" {
		return nil, shared.ErrNotFound
	}
	return r.Get(ctx, id)
}

func (r *memThreads) Save(_ context.Context, th *thread.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states == nil {
		r.states = map[shared.ThreadID]thread.State{}
	}
	st := th.State()
	stored, ok := r.states[st.ID]
	switch {
	case st.Version == 0 && ok:
		return shared.ErrVersionConflict
	case st.Version != 0 && (!ok || stored.Version != st.Version):
		return shared.ErrVersionConflict
	}
	st.Version++
	r.states[st.ID] = st
	th.Persisted(st.Version)
	return nil
}

func (r *memThreads) ListRunning(ctx context.Context) ([]*thread.Thread, error) {
	r.mu.Lock()
	var ids []shared.ThreadID
	for id, st := range r.states {
		if st.Current != nil && (st.Current.Status == thread.StatusPlanning || st.Current.Status == thread.StatusExecuting) {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	var out []*thread.Thread
	for _, id := range ids {
		th, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, nil
}

type memHistory struct {
	mu      sync.Mutex
	entries map[shared.ThreadID][]values.HistoryEntry
}

func (h *memHistory) Append(_ context.Context, id shared.ThreadID, turn int, e values.HistoryTurn) error {
	if err := e.Validate(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = map[shared.ThreadID][]values.HistoryEntry{}
	}
	h.entries[id] = append(h.entries[id], values.HistoryEntry{Turn: turn, Entry: e})
	return nil
}

func (h *memHistory) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []values.HistoryEntry
	for _, e := range h.entries[id] {
		if e.Turn >= from {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (h *memHistory) all(id shared.ThreadID) []values.HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.entries[id])
}

type subjects []values.BaseSubject

func (s subjects) BaseSubjects(context.Context) ([]values.BaseSubject, error) { return s, nil }

// memTx rolls the history back when fn fails, like a database transaction.
type memTx struct{ h *memHistory }

func (tx memTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	tx.h.mu.Lock()
	saved := map[shared.ThreadID][]values.HistoryEntry{}
	for id, es := range tx.h.entries {
		saved[id] = slices.Clone(es)
	}
	tx.h.mu.Unlock()
	if err := fn(ctx); err != nil {
		tx.h.mu.Lock()
		tx.h.entries = saved
		tx.h.mu.Unlock()
		return err
	}
	return nil
}

type env struct {
	svc     *Service
	model   *fakeModel
	threads *memThreads
	history *memHistory
	clock   *fixedClock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		model: &fakeModel{}, threads: &memThreads{}, history: &memHistory{},
		clock: &fixedClock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, moscow)},
	}
	s1, _ := values.NewRef(values.KindSubject, "s1")
	svc, err := New(Deps{
		Model: e.model, Threads: e.threads, History: e.history,
		Subjects: subjects{{ID: s1, Name: "Физика"}}, Facts: UnknownFacts{}, Tx: memTx{e.history}, Clock: e.clock, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}
