package agentchats

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

type seqIDs struct{ n int }

func (g *seqIDs) NewThreadID() shared.ThreadID {
	g.n++
	return shared.ThreadID(fmt.Sprintf("th%d", g.n))
}

type turnKey struct {
	id   shared.ThreadID
	turn int
}

// memChats keeps stored states and restores them on every read, like the database does.
type memChats struct {
	mu       sync.Mutex
	states   map[shared.ThreadID]thread.State
	mentions map[turnKey][]values.Mention
	saves    int
	// beforeSave, when set, runs before every Save (outside the lock), so a test can save a competing change.
	beforeSave func()
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

// sorted is the order of the sqlite adapter: unarchived first, newest activity first, ties to the greater id.
func (r *memChats) sorted() []thread.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []thread.State
	for _, st := range r.states {
		all = append(all, st)
	}
	slices.SortFunc(all, func(a, b thread.State) int {
		if a.ArchivedAt.IsZero() != b.ArchivedAt.IsZero() {
			if a.ArchivedAt.IsZero() {
				return -1
			}
			return 1
		}
		if c := b.LastActive.Compare(a.LastActive); c != 0 {
			return c
		}
		if a.ID > b.ID {
			return -1
		}
		return 1
	})
	return all
}

func (r *memChats) Current(ctx context.Context) (*thread.Thread, error) {
	all := r.sorted()
	if len(all) == 0 || !all[0].ArchivedAt.IsZero() {
		return nil, shared.ErrNotFound
	}
	return r.Get(ctx, all[0].ID)
}

func (r *memChats) List(ctx context.Context) ([]*thread.Thread, error) {
	var out []*thread.Thread
	for _, st := range r.sorted() {
		th, err := r.Get(ctx, st.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, nil
}

func (r *memChats) RequestMentions(_ context.Context, id shared.ThreadID, turn int) ([]values.Mention, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[id]
	if !ok || turn < 1 || turn > st.Turns {
		return nil, fmt.Errorf("request %s/%d: %w", id, turn, shared.ErrNotFound)
	}
	return slices.Clone(r.mentions[turnKey{id, turn}]), nil
}

func (r *memChats) Save(_ context.Context, th *thread.Thread) error {
	if r.beforeSave != nil {
		r.beforeSave()
	}
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
	r.saves++
	th.Persisted(st.Version)
	return nil
}

// put stores th as a chat and the mentions of its current request, as agentturn's saves do.
func (r *memChats) put(t *testing.T, th *thread.Thread) {
	t.Helper()
	if err := r.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}
	if turn, ok := th.Turn(); ok {
		r.mu.Lock()
		if r.mentions == nil {
			r.mentions = map[turnKey][]values.Mention{}
		}
		r.mentions[turnKey{th.ID(), turn.Number()}] = turn.Mentions()
		r.mu.Unlock()
	}
}

// bump saves the stored chat once more, as a step of its request would.
func (r *memChats) bump(id shared.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.states[id]
	st.Version++
	r.states[id] = st
}

type memHistory struct {
	entries map[shared.ThreadID][]values.HistoryEntry
	err     error
}

func (h *memHistory) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
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

type env struct {
	svc     *Service
	chats   *memChats
	history *memHistory
	clock   *fixedClock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		chats: &memChats{}, history: &memHistory{entries: map[shared.ThreadID][]values.HistoryEntry{}},
		clock: &fixedClock{now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)},
	}
	svc, err := New(Deps{Chats: e.chats, History: e.history, Clock: e.clock, IDs: &seqIDs{}})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// chat builds a chat with requests of the given texts; every request but the last is done, the last one is left
// PLANNING unless done is set.
func (e *env) chat(t *testing.T, id shared.ThreadID, done bool, texts ...string) *thread.Thread {
	t.Helper()
	th, err := thread.New(id, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		if err := th.Ask(thread.Request{Text: text}, e.clock.Now()); err != nil {
			t.Fatal(err)
		}
		if i < len(texts)-1 || done {
			if err := th.Complete(e.clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.chats.put(t, th)
	return th
}
