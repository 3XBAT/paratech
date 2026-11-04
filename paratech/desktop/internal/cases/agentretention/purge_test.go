package agentretention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/shared"
)

func newPurger(t *testing.T, s *store, d Deps) (*Purger, *tx, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	x := &tx{}
	d.Requests, d.Tx, d.Clock = s, x, fixedClock{now}
	d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	p, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return p, x, &logs
}

func TestNewNeedsPorts(t *testing.T) {
	for _, d := range []Deps{{}, {Requests: newStore(), Tx: &tx{}}, {Requests: newStore(), Clock: fixedClock{}}, {Tx: &tx{}, Clock: fixedClock{}}} {
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New(%+v) = %v", d, err)
		}
	}
}

// Which requests lose their calls and results: final, not current, ended more than 90 days ago (exactly 90 stays).
func TestPurgeSelectsRequests(t *testing.T) {
	tests := []struct {
		name  string
		req   *stored
		purge bool
	}{
		{"done 91 days ago", request(true, false, daysAgo(91), ""), true},
		{"done 89 days ago", request(true, false, daysAgo(89), ""), false},
		{"exactly 90 days ago", request(true, false, thread.DetailsCutoff(now), ""), false},
		{"a millisecond past 90 days", request(true, false, thread.DetailsCutoff(now).Add(-time.Millisecond), ""), true},
		{"the current request 200 days old", request(true, true, daysAgo(200), ""), false},
		{"not final", request(false, false, daysAgo(200), ""), false},
		{"frozen line", request(true, false, daysAgo(120), "reply: старая строка"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore()
			s.add("c1", 1, tt.req)
			p, _, _ := newPurger(t, s, Deps{})
			n, err := p.Purge(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			got := s.get("c1", 1)
			if purged := len(got.details) == 0; purged != tt.purge || n != map[bool]int{true: 1}[tt.purge] {
				t.Fatalf("purged = %v (removed %d), want %v", purged, n, tt.purge)
			}
			if len(got.texts) != 1 || got.texts[0].StudentText != "просьба" {
				t.Fatalf("the text is gone: %+v", got.texts)
			}
		})
	}
}

// A request without a reply line gets thread.Reply of its entries first; a frozen line is never overwritten.
func TestPurgeFreezesTheReplyLine(t *testing.T) {
	s := newStore()
	s.add("c1", 1, request(true, false, daysAgo(100), ""))
	s.add("c1", 2, request(true, false, daysAgo(100), "reply: как было"))
	texts, details := replied("вторая", "другой ответ")
	want := thread.Reply(append(texts, details...))
	s.add("c2", 1, &stored{final: true, endedAt: daysAgo(95), texts: texts, details: details})
	p, _, _ := newPurger(t, s, Deps{})
	if n, err := p.Purge(t.Context()); err != nil || n != 3 {
		t.Fatalf("Purge = %d, %v", n, err)
	}
	if got := s.get("c1", 1).line; got != "reply: ответ" {
		t.Fatalf("frozen line = %q", got)
	}
	if got := s.get("c1", 2).line; got != "reply: как было" {
		t.Fatalf("an existing line changed to %q", got)
	}
	if got := s.get("c2", 1).line; got != want {
		t.Fatalf("frozen line = %q, want %q", got, want)
	}
}

// An unreadable request keeps everything and is logged by chat, number and code only; the others go on.
func TestPurgeSkipsAnUnreadableRequest(t *testing.T) {
	s := newStore()
	broken := request(true, false, daysAgo(100), "")
	broken.unreadable = true
	s.add("c1", 1, broken)
	s.add("c1", 2, request(true, false, daysAgo(100), ""))
	frozen := request(true, false, daysAgo(100), "reply: есть")
	frozen.unreadable = true // its entries are not read: the line is frozen
	s.add("c1", 3, frozen)
	p, _, logs := newPurger(t, s, Deps{})
	n, err := p.Purge(t.Context())
	if err != nil || n != 2 {
		t.Fatalf("Purge = %d, %v", n, err)
	}
	if got := s.get("c1", 1); len(got.details) == 0 || got.line != "" {
		t.Fatalf("the unreadable request changed: %+v", got)
	}
	if len(s.get("c1", 2).details) != 0 || len(s.get("c1", 3).details) != 0 {
		t.Fatal("the other requests kept their details")
	}
	log := logs.String()
	if !strings.Contains(log, "thread=c1") || !strings.Contains(log, "turn=1") || !strings.Contains(log, ErrUnreadable.Error()) {
		t.Fatalf("log = %q", log)
	}
	if strings.Contains(log, "secret") || strings.Contains(log, "просьба") {
		t.Fatalf("the log holds a text: %q", log)
	}

	// The next run tries it again and still removes nothing else.
	if n, err := p.Purge(t.Context()); err != nil || n != 0 {
		t.Fatalf("second Purge = %d, %v", n, err)
	}
}

// A second run removes nothing; a run is bounded by its batches and the next one goes on.
func TestPurgeIsIdempotentAndBounded(t *testing.T) {
	s := newStore()
	for i := 1; i <= 7; i++ {
		s.add(shared.ThreadID(fmt.Sprintf("c%d", i%3)), i, request(true, false, daysAgo(100+float64(i)), ""))
	}
	p, x, _ := newPurger(t, s, Deps{BatchSize: 2, MaxBatches: 2})
	for _, want := range []int{4, 3, 0} {
		if n, err := p.Purge(t.Context()); err != nil || n != want {
			t.Fatalf("Purge = %d, %v, want %d", n, err, want)
		}
	}
	if x.n != 7 {
		t.Fatalf("transactions = %d, want one per request", x.n)
	}
}

// A request that stops qualifying between the list and the delete is not touched and not counted.
func TestPurgeRechecksInTheStore(t *testing.T) {
	s := newStore()
	r := request(true, false, daysAgo(100), "")
	s.add("c1", 1, r)
	s.onPurge = func() {
		s.mu.Lock()
		r.current = true
		s.mu.Unlock()
	}
	p, _, _ := newPurger(t, s, Deps{})
	if n, err := p.Purge(t.Context()); err != nil || n != 0 {
		t.Fatalf("Purge = %d, %v", n, err)
	}
	if got := s.get("c1", 1); len(got.details) == 0 || got.line != "" {
		t.Fatalf("a request that became current changed: %+v", got)
	}
}

func TestPurgeStopsOnCancelAndListFailure(t *testing.T) {
	s := newStore()
	for i := 1; i <= 3; i++ {
		s.add("c1", i, request(true, false, daysAgo(100), ""))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, _, _ := newPurger(t, s, Deps{})
	if n, err := p.Purge(ctx); !errors.Is(err, context.Canceled) || n != 0 || s.purges != 0 {
		t.Fatalf("Purge of a cancelled context = %d, %v (%d purges)", n, err, s.purges)
	}

	ctx, cancel = context.WithCancel(t.Context())
	s.onPurge = cancel // cancelled while the first request is purged
	if n, err := p.Purge(ctx); !errors.Is(err, context.Canceled) || n != 1 || s.purges != 1 {
		t.Fatalf("Purge cancelled midway = %d, %v (%d purges)", n, err, s.purges)
	}

	s.onPurge = nil
	s.listErr = errors.New("disk")
	if _, err := p.Purge(t.Context()); !errors.Is(err, ErrStore) || !errors.Is(err, s.listErr) {
		t.Fatalf("Purge with a failed list = %v", err)
	}
}
