package wails

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// summaryMarker is in the stored summary of the chat only: no binding may return it (ADR-0028 Open 8 → а).
const summaryMarker = "МАРКЕР-СВОДКИ-7f3a"

// memChatStore keeps stored chat states and restores them on every read, as the database does; it is both ports of
// agentchats.
type memChatStore struct {
	mu      sync.Mutex
	states  map[shared.ThreadID]thread.State
	history map[shared.ThreadID][]values.HistoryEntry
}

func (m *memChatStore) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	m.mu.Lock()
	st, ok := m.states[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("chat %s: %w", id, shared.ErrNotFound)
	}
	return thread.Restore(st)
}

func (m *memChatStore) sorted() []thread.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []thread.State
	for _, st := range m.states {
		all = append(all, st)
	}
	slices.SortFunc(all, func(a, b thread.State) int {
		if a.ArchivedAt.IsZero() != b.ArchivedAt.IsZero() {
			if a.ArchivedAt.IsZero() {
				return -1
			}
			return 1
		}
		return b.LastActive.Compare(a.LastActive)
	})
	return all
}

func (m *memChatStore) Current(ctx context.Context) (*thread.Thread, error) {
	for _, st := range m.sorted() {
		if st.ArchivedAt.IsZero() {
			return m.Get(ctx, st.ID)
		}
	}
	return nil, shared.ErrNotFound
}

func (m *memChatStore) List(ctx context.Context) ([]*thread.Thread, error) {
	var out []*thread.Thread
	for _, st := range m.sorted() {
		th, err := m.Get(ctx, st.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}
	return out, nil
}

func (m *memChatStore) RequestMentions(context.Context, shared.ThreadID, int) ([]values.Mention, error) {
	return nil, nil
}

func (m *memChatStore) Save(_ context.Context, th *thread.Thread) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := th.State()
	if stored, ok := m.states[st.ID]; ok != (st.Version != 0) || (ok && stored.Version != st.Version) {
		return shared.ErrVersionConflict
	}
	st.Version++
	m.states[st.ID] = st
	th.Persisted(st.Version)
	return nil
}

func (m *memChatStore) Since(_ context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []values.HistoryEntry
	for _, e := range m.history[id] {
		if e.Turn >= from && e.DroppedBy == 0 {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ForkSource groups the stored entries of the chat by request, as the sqlite adapter reads them.
func (m *memChatStore) ForkSource(_ context.Context, id shared.ThreadID, upTo int) ([]thread.PastRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at := m.states[id].CreatedAt
	var out []thread.PastRequest
	for _, e := range m.history[id] {
		if e.Turn > upTo || e.DroppedBy != 0 {
			continue
		}
		if n := len(out); n == 0 || out[n-1].Number != e.Turn {
			out = append(out, thread.PastRequest{Number: e.Turn, ReplyLine: e.ReplyLine, StartedAt: at, EndedAt: at})
		}
		p := &out[len(out)-1]
		if e.Entry.StudentText != "" {
			p.Texts = append(p.Texts, e.Entry.StudentText)
		}
		if p.ReplyLine == "" {
			p.Entries = append(p.Entries, e.Entry)
		}
	}
	return out, nil
}

// SaveFork stores the fork's thread and its copies' texts with their lines.
func (m *memChatStore) SaveFork(ctx context.Context, f thread.Fork) error {
	if err := m.Save(ctx, f.Thread); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range f.Requests {
		for _, text := range r.Texts {
			m.history[f.Thread.ID()] = append(m.history[f.Thread.ID()],
				values.HistoryEntry{Turn: r.Number, Entry: values.HistoryTurn{StudentText: text}, ReplyLine: r.ReplyLine})
		}
	}
	return nil
}

type summaryClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *summaryClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Second)
	return c.now
}

// nextIDs gives th2, th3, …: NewChat and ForkChat each make a chat.
type nextIDs struct {
	mu sync.Mutex
	n  int
}

func (g *nextIDs) NewThreadID() shared.ThreadID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return shared.ThreadID(fmt.Sprintf("th%d", g.n+1))
}

// compactedChat stores chat th1 with three done requests and a summary of the first holding summaryMarker.
func compactedChat(t *testing.T, store *memChatStore, clock *summaryClock) {
	t.Helper()
	th, err := thread.New("th1", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 3; turn++ {
		if err := th.Ask(thread.Request{Text: fmt.Sprintf("просьба %d", turn)}, clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := th.Complete(clock.Now()); err != nil {
			t.Fatal(err)
		}
		store.history["th1"] = append(store.history["th1"],
			values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{StudentText: fmt.Sprintf("просьба %d", turn)}})
	}
	summary, err := values.ParseHistorySummary(1, "Сводка: "+summaryMarker)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Compact(summary, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(t.Context(), "th1"); err != nil || !strings.Contains(got.Summary().Text(), summaryMarker) {
		t.Fatalf("the fixture holds no summary: %v", err)
	}
}

// No chat binding and no update returns the summary of a chat, on the real chat use case over a chat that has one.
func TestNoChatBindingReturnsTheSummary(t *testing.T) {
	store := &memChatStore{states: map[shared.ThreadID]thread.State{}, history: map[shared.ThreadID][]values.HistoryEntry{}}
	clock := &summaryClock{now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	compactedChat(t, store, clock)
	chats, err := agentchats.New(agentchats.Deps{Chats: store, History: store, Clock: clock, IDs: &nextIDs{}})
	if err != nil {
		t.Fatal(err)
	}

	s, _, _, logs := newAgent(t, readyLink())
	s.chats = chats
	s.panel.SetFills(chats)
	s.current = &fakeCurrent{ok: true, update: AgentUpdate{Thread: "th1", Turn: 3, Status: thread.StatusDone}}
	em := &fakeEmitter{}
	s.panel.Attach(em)

	ctx := context.Background()
	fill := &AgentFillView{Percent: 10, Level: "normal"}
	results := map[string]any{
		"Chats":        s.Chats(ctx),
		"ChatEarlier":  s.ChatEarlier(ctx, "th1"),
		"ChatCurrent":  s.ChatCurrent(ctx, "th1"),
		"ForkChat":     s.ForkChat(ctx, "th1", 3), // th2, with the summary of request 1 copied
		"Fork earlier": s.ChatEarlier(ctx, "th2"),
		"RenameChat":   s.RenameChat(ctx, "th1", "Физика"),
		"NewChat":      s.NewChat(ctx),
		"ArchiveChat":  s.ArchiveChat(ctx, "th1"),
		"RestoreChat":  s.RestoreChat(ctx, "th1"),
		"Chats after":  s.Chats(ctx),
		"Earlier more": s.ChatEarlier(ctx, "th1"),
	}
	if r := results["ForkChat"].(AgentForkResult); r.Problem != "" || r.Chat.ThreadID != "th2" || r.Text != "просьба 3" ||
		!strings.Contains(store.states["th2"].Summary.Text(), summaryMarker) {
		t.Fatalf("the fork does not hold the summary, so it proves nothing: %+v", r)
	}
	s.panel.Publish(AgentUpdate{Thread: "th1", Compaction: true, Compacting: true})
	s.panel.Publish(AgentUpdate{Thread: "th1", Compaction: true, Fill: fill})
	s.panel.Publish(AgentUpdate{Thread: "th1", Compaction: true})
	s.panel.Publish(AgentUpdate{Thread: "th1", Turn: 4, Status: thread.StatusDone})
	for i, e := range em.events {
		results[fmt.Sprintf("agent:update %d", i)] = e.data[0]
	}
	if len(em.events) != 4 {
		t.Fatalf("%d updates emitted", len(em.events))
	}

	for name, r := range results {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), summaryMarker) {
			t.Errorf("%s returns the summary: %s", name, b)
		}
		if strings.Contains(string(b), `"problem":"failed"`) || strings.Contains(string(b), `"problem":"not_found"`) {
			t.Errorf("%s failed, so it proves nothing: %s", name, b)
		}
	}
	if strings.Contains(logs.String(), summaryMarker) {
		t.Errorf("the log holds the summary: %s", logs.String())
	}
}

// No type the chat bindings or the update event return has a field or a type named Summary*, so a summary cannot be
// added to one by accident. The plan card's caption (AgentPlanView.Summary, «Понял так») is the one exception: it is
// the checked text of a card, not the chat summary.
func TestChatBindingTypesHaveNoSummary(t *testing.T) {
	allowed := map[string]bool{"AgentPlanView.Summary": true}
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(tp reflect.Type) {
		for tp.Kind() == reflect.Pointer || tp.Kind() == reflect.Slice || tp.Kind() == reflect.Array || tp.Kind() == reflect.Map {
			tp = tp.Elem()
		}
		if tp.Kind() != reflect.Struct || seen[tp] {
			return
		}
		seen[tp] = true
		if strings.HasPrefix(tp.Name(), "Summary") {
			t.Errorf("type %s", tp.Name())
		}
		for i := range tp.NumField() {
			f := tp.Field(i)
			if strings.HasPrefix(f.Name, "Summary") && !allowed[tp.Name()+"."+f.Name] {
				t.Errorf("field %s.%s", tp.Name(), f.Name)
			}
			walk(f.Type)
		}
	}
	for _, v := range []any{AgentChatsView{}, AgentChatResult{}, AgentEarlierView{}, AgentCurrentView{}, AgentUpdateView{}} {
		walk(reflect.TypeOf(v))
	}
	if !seen[reflect.TypeOf(AgentFillView{})] || !seen[reflect.TypeOf(AgentPlanView{})] {
		t.Fatal("the walk did not reach the nested views")
	}
}

// The JSON of the compaction updates: no request state, cards [], the new fill on the end of a compaction that stored
// a summary, null on its start and on a failure; the fill is never read for them.
func TestAgentCompactionUpdateContract(t *testing.T) {
	fills := &fakeFills{}
	panel := NewAgentPanel(discard())
	panel.SetFills(fills)
	em := &fakeEmitter{}
	panel.Attach(em)

	head := `{"threadId":"th1","turn":0,"subjectId":"","lessonId":"","assignmentId":"","screen":"",` +
		`"status":"","stopReason":"","problem":"","progress":false,"retry":0,"ticket":"","queued":false,"cards":[],`
	tests := []struct {
		name string
		u    AgentUpdate
		want string
	}{
		{"start", AgentUpdate{Thread: "th1", Compaction: true, Compacting: true},
			head + `"fill":null,"compaction":true,"compacting":true}`},
		{"end", AgentUpdate{Thread: "th1", Compaction: true, Fill: &AgentFillView{Percent: 45, Level: "normal"}},
			head + `"fill":{"percent":45,"level":"normal","overflow":false},"compaction":true,"compacting":false}`},
		{"failed", AgentUpdate{Thread: "th1", Compaction: true},
			head + `"fill":null,"compaction":true,"compacting":false}`},
	}
	for i, tt := range tests {
		panel.Publish(tt.u)
		got, err := json.Marshal(em.events[i].data[0])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tt.want {
			t.Errorf("%s JSON =\n%s\nwant\n%s", tt.name, got, tt.want)
		}
	}
	if len(fills.asked) != 0 {
		t.Fatalf("a compaction update read the fill: %v", fills.asked)
	}
}
