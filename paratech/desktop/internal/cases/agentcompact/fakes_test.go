package agentcompact

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Second)
	return c.now
}

// memChats keeps stored states and restores them on every read, like the database does.
type memChats struct {
	mu     sync.Mutex
	states map[shared.ThreadID]thread.State
	saves  int
	// beforeSave, when set, runs before every Save (outside the lock), so a test can save a competing change.
	beforeSave func()
	// saveErr, when set, fails every Save after beforeSave.
	saveErr error
}

func (r *memChats) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	r.mu.Lock()
	st, ok := r.states[id]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("chat %s: %w", id, shared.ErrNotFound)
	}
	return thread.Restore(st)
}

// Current is the unarchived chat with the newest last activity.
func (r *memChats) Current(ctx context.Context) (*thread.Thread, error) {
	r.mu.Lock()
	var all []thread.State
	for _, st := range r.states {
		if st.ArchivedAt.IsZero() {
			all = append(all, st)
		}
	}
	r.mu.Unlock()
	if len(all) == 0 {
		return nil, shared.ErrNotFound
	}
	newest := slices.MaxFunc(all, func(a, b thread.State) int { return a.LastActive.Compare(b.LastActive) })
	return r.Get(ctx, newest.ID)
}

func (r *memChats) Save(ctx context.Context, th *thread.Thread) error {
	if r.beforeSave != nil {
		r.beforeSave()
	}
	if err := ctx.Err(); err != nil {
		return err // as the database does
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
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
	r.saves++
	th.Persisted(st.Version)
	return nil
}

func (r *memChats) put(t *testing.T, th *thread.Thread) {
	t.Helper()
	if err := r.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}
}

func (r *memChats) saved() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves
}

func (r *memChats) stored(t *testing.T, id shared.ThreadID) *thread.Thread {
	t.Helper()
	th, err := r.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// bump saves the stored chat once more, as a step of its request would.
func (r *memChats) bump(id shared.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.states[id]
	st.Version++
	r.states[id] = st
}

// remove forgets the stored chat.
func (r *memChats) remove(id shared.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, id)
}

type memHistory struct {
	mu      sync.Mutex
	entries map[shared.ThreadID][]values.HistoryEntry
	err     error
	reads   int
}

func (h *memHistory) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reads++
	if h.err != nil {
		return nil, h.err
	}
	var out []values.HistoryEntry
	for _, e := range h.entries[id] {
		if e.Turn >= from && e.DroppedBy == 0 {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (h *memHistory) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.err = err
}

// drop marks the entries of requests from on as dropped by request by, as a restart does.
func (h *memHistory) drop(id shared.ThreadID, from, by int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.entries[id] {
		if e.Turn >= from {
			h.entries[id][i].DroppedBy = by
		}
	}
}

// fakeHub is the Summarizer: it records the requests and answers with answer(n), n counting from 1. When hold is
// set, each call first signals arrived and waits for hold to close.
type fakeHub struct {
	mu      sync.Mutex
	reqs    []values.SummaryRequest
	answer  func(n int) (string, error)
	arrived chan struct{}
	hold    chan struct{}
}

func (f *fakeHub) Summarize(ctx context.Context, req values.SummaryRequest) (string, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	n := len(f.reqs)
	f.mu.Unlock()
	if f.hold != nil {
		f.arrived <- struct{}{}
		select {
		case <-f.hold:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.answer(n)
}

func (f *fakeHub) calls() []values.SummaryRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func answering(text string) func(int) (string, error) {
	return func(int) (string, error) { return text, nil }
}

// syncBuffer is a log sink safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type env struct {
	svc     *Service
	chats   *memChats
	history *memHistory
	hub     *fakeHub
	clock   *fixedClock
	log     *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		chats: &memChats{}, history: &memHistory{entries: map[shared.ThreadID][]values.HistoryEntry{}},
		hub:   &fakeHub{answer: answering("Сводка: обсуждали физику.")},
		clock: &fixedClock{now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}, log: &syncBuffer{},
	}
	svc, err := New(Deps{
		Chats: e.chats, History: e.history, Summarizer: e.hub, Clock: e.clock,
		Log: slog.New(slog.NewTextHandler(e.log, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

func numbered(turn int) string { return fmt.Sprintf("просьба %d", turn) }

// historyOf is the stored history of requests 1…n: a text, a shown reply card and its result.
func historyOf(n int, text func(int) string) []values.HistoryEntry {
	var out []values.HistoryEntry
	for turn := 1; turn <= n; turn++ {
		out = append(out,
			values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{StudentText: text(turn)}},
			values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{
				Calls: []values.RawCall{{CallID: "c1", Tool: "reply", ArgumentsJSON: fmt.Sprintf(`{"text":"ответ %d"}`, turn)}},
				Seal:  []byte{1},
			}}},
			values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Results: []values.ToolResult{values.ShownResult("c1")}}})
	}
	return out
}

// chat stores a chat of n requests with their history; every request is done, the last one unless open is set
// (then it is PLANNING and has no history yet).
func (e *env) chat(t *testing.T, id shared.ThreadID, n int, open bool) {
	t.Helper()
	th, err := thread.New(id, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= n; turn++ {
		if err := th.Ask(thread.Request{Text: numbered(turn)}, e.clock.Now()); err != nil {
			t.Fatal(err)
		}
		if turn < n || !open {
			if err := th.Complete(e.clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.chats.put(t, th)
	done := n
	if open {
		done--
	}
	e.history.entries[id] = historyOf(done, numbered)
}
