package agentchats

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

func numbered(turn int) string { return fmt.Sprintf("просьба %d", turn) }

// historyOf is the stored history of n requests: a text, a shown reply card.
func historyOf(n int) []values.HistoryEntry {
	var out []values.HistoryEntry
	for turn := 1; turn <= n; turn++ {
		id := fmt.Sprintf("c%d", turn)
		out = append(out, student(turn, numbered(turn)),
			calls(turn, raw(id, "reply", `{"text":"ответ"}`)), results(turn, values.ShownResult(id)))
	}
	return out
}

// longChat is a chat of n requests with their history; the last one is done when done is set.
func (e *env) longChat(t *testing.T, id shared.ThreadID, n int, done bool) *thread.Thread {
	t.Helper()
	texts := make([]string, n)
	for i := range texts {
		texts[i] = numbered(i + 1)
	}
	th := e.chat(t, id, done, texts...)
	e.history.entries[id] = historyOf(n)
	return th
}

func fillOf(percent int, level thread.FillLevel, overflow bool) *Fill {
	return &Fill{Percent: percent, Level: level, Overflow: overflow}
}

func sameFill(a, b *Fill) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// The list carries the fill of every chat, the archived ones too; a chat whose history cannot be read has none and
// does not fail the list.
func TestListCarriesFill(t *testing.T) {
	e := newEnv(t)
	old := e.longChat(t, "old", 18, true)
	if err := old.Archive(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.chats.Save(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	e.longChat(t, "broken", 3, true)
	e.history.errs = map[shared.ThreadID]error{"broken": errors.New("disk")}
	e.longChat(t, "high", 14, true)
	e.longChat(t, "low", 13, false)
	e.longChat(t, "over", 21, true)
	e.chat(t, "empty", false)

	got, err := e.svc.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[shared.ThreadID]*Fill{
		"empty":  fillOf(0, thread.FillNormal, false),
		"over":   fillOf(100, thread.FillFull, true),
		"low":    fillOf(65, thread.FillNormal, false),
		"high":   fillOf(70, thread.FillHigh, false),
		"broken": nil,
		"old":    fillOf(90, thread.FillFull, false),
	}
	if len(got) != len(want) {
		t.Fatalf("listed %d chats: %+v", len(got), got)
	}
	for _, c := range got {
		if !sameFill(c.Fill, want[c.ID]) {
			t.Errorf("fill of %s = %+v, want %+v", c.ID, c.Fill, want[c.ID])
		}
	}
	for _, r := range e.history.reads {
		if r.id == "empty" {
			t.Errorf("an empty chat read its history")
		}
		if r.limit != thread.HistoryReadLimit {
			t.Errorf("read of %s with limit %d, want the step's %d", r.id, r.limit, thread.HistoryReadLimit)
		}
	}
}

func TestFill(t *testing.T) {
	e := newEnv(t)
	e.longChat(t, "c1", 17, true)
	e.longChat(t, "broken", 2, true)
	boom := errors.New("disk")
	e.history.errs = map[shared.ThreadID]error{"broken": boom}

	got, err := e.svc.Fill(t.Context(), "c1")
	if err != nil || got != (Fill{Percent: 85, Level: thread.FillHigh}) {
		t.Fatalf("Fill = %+v, %v", got, err)
	}
	if _, err := e.svc.Fill(t.Context(), "a/b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed id = %v", err)
	}
	if _, err := e.svc.Fill(t.Context(), "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a missing chat = %v", err)
	}
	if _, err := e.svc.Fill(t.Context(), "broken"); !errors.Is(err, boom) {
		t.Fatalf("a failed read = %v", err)
	}
}

// After a Restart only the restarted request's own history is sent (HistoryFrom): the older requests do not count
// and are no overflow (TASK-203 follow-up).
func TestFillFromHistoryStart(t *testing.T) {
	e := newEnv(t)
	e.longChat(t, "c1", 22, true)
	if f, _ := e.svc.Fill(t.Context(), "c1"); !f.Overflow {
		t.Fatalf("before the restart %+v, want an overflow", f)
	}
	e.chats.edit("c1", func(st *thread.State) { st.HistoryFrom = 22 })

	got, err := e.svc.Fill(t.Context(), "c1")
	if err != nil || got != (Fill{Percent: 5, Level: thread.FillNormal}) {
		t.Fatalf("Fill after the restart = %+v, %v", got, err)
	}
	if r := e.history.reads[len(e.history.reads)-1]; r.from != 22 {
		t.Fatalf("read from request %d, want 22", r.from)
	}
}

// A summary lowers the fill: it counts as one turn and its bytes, the requests it covers are neither pairs nor an
// overflow.
func TestFillWithSummary(t *testing.T) {
	e := newEnv(t)
	th := e.longChat(t, "c1", 21, true)
	if f, _ := e.svc.Fill(t.Context(), "c1"); f != (Fill{Percent: 100, Level: thread.FillFull, Overflow: true}) {
		t.Fatalf("before the compaction %+v", f)
	}
	summary, err := values.ParseHistorySummary(10, strings.Repeat("сводка ", 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Compact(summary, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.chats.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}

	got, err := e.svc.Fill(t.Context(), "c1")
	if err != nil || got != (Fill{Percent: 55, Level: thread.FillNormal}) {
		t.Fatalf("Fill after the compaction = %+v, %v", got, err)
	}

	// A summary long enough to carry the bytes share is counted too.
	long, err := values.ParseHistorySummary(20, strings.Repeat("я", values.MaxSummaryRunes))
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Compact(long, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.chats.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}
	got, err = e.svc.Fill(t.Context(), "c1")
	if err != nil || got != (Fill{Percent: 16, Level: thread.FillNormal}) {
		t.Fatalf("Fill with a long summary = %+v, %v", got, err)
	}
}

// The results of the chat moves carry the fill as well.
func TestMovesCarryFill(t *testing.T) {
	e := newEnv(t)
	e.longChat(t, "c1", 14, true)
	want := fillOf(70, thread.FillHigh, false)

	moves := map[string]func() (Chat, error){
		"rename":  func() (Chat, error) { return e.svc.Rename(t.Context(), "c1", "Матан") },
		"archive": func() (Chat, error) { return e.svc.Archive(t.Context(), "c1") },
		"restore": func() (Chat, error) { return e.svc.Restore(t.Context(), "c1") },
	}
	for _, name := range []string{"rename", "archive", "restore"} {
		c, err := moves[name]()
		if err != nil || !sameFill(c.Fill, want) {
			t.Fatalf("%s = %+v (fill %+v), %v", name, c, c.Fill, err)
		}
	}
	c, err := e.svc.New(t.Context())
	if err != nil || !sameFill(c.Fill, fillOf(0, thread.FillNormal, false)) {
		t.Fatalf("New = %+v (fill %+v), %v", c, c.Fill, err)
	}
}
