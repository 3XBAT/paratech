package agentretention

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

var now = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) time.Time { return now.Add(-time.Duration(d * 24 * float64(time.Hour))) }

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type key struct {
	thread shared.ThreadID
	number int
}

// stored is a request as the store keeps it.
type stored struct {
	final, current bool
	endedAt        time.Time
	line           string // "" = NULL
	texts          []values.HistoryTurn
	details        []values.HistoryTurn // calls and results, in order after the texts
	unreadable     bool
}

// store keeps the guards of the sqlite adapter: a request is due while final, not current and ended before the
// cutoff; the delete needs a reply line; the freeze writes only a NULL line.
type store struct {
	mu   sync.Mutex
	reqs map[key]*stored

	listErr error
	// onPurge runs before each PurgeDetails (a test cancels its context there).
	onPurge func()
	purges  int
}

func newStore() *store { return &store{reqs: map[key]*stored{}} }

func (s *store) add(id shared.ThreadID, n int, r *stored) { s.reqs[key{id, n}] = r }

func due(r *stored, cutoff time.Time) bool {
	return r.final && !r.current && r.endedAt.Before(cutoff)
}

func (s *store) EndedBefore(_ context.Context, cutoff time.Time, after thread.EndedRequest, limit int) ([]thread.EndedRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []thread.EndedRequest
	for k, r := range s.reqs {
		if due(r, cutoff) && len(r.details) > 0 {
			out = append(out, thread.EndedRequest{Thread: k.thread, Number: k.number, EndedAt: r.endedAt, ReplyFrozen: r.line != ""})
		}
	}
	order := func(a, b thread.EndedRequest) int {
		return cmp.Or(a.EndedAt.Compare(b.EndedAt), cmp.Compare(a.Thread, b.Thread), cmp.Compare(a.Number, b.Number))
	}
	slices.SortFunc(out, order)
	if !after.EndedAt.IsZero() {
		out = slices.DeleteFunc(out, func(e thread.EndedRequest) bool { return order(e, after) <= 0 })
	}
	return out[:min(limit, len(out))], nil
}

func (s *store) Entries(_ context.Context, id shared.ThreadID, n int) ([]values.HistoryTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reqs[key{id, n}]
	if !ok || r.unreadable {
		return nil, errors.New("stored agent request: json: secret text") // the cause may hold a text
	}
	return append(slices.Clone(r.texts), r.details...), nil
}

func (s *store) PurgeDetails(_ context.Context, id shared.ThreadID, n int, line string, cutoff time.Time) (bool, error) {
	if s.onPurge != nil {
		s.onPurge()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purges++
	r, ok := s.reqs[key{id, n}]
	if !ok || !due(r, cutoff) {
		return false, nil
	}
	if line != "" && r.line == "" {
		r.line = line
	}
	if r.line == "" || len(r.details) == 0 {
		return false, nil
	}
	r.details = nil
	return true, nil
}

func (s *store) get(id shared.ThreadID, n int) stored {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.reqs[key{id, n}]
}

// tx runs fn; it counts the transactions.
type tx struct{ n int }

func (t *tx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	t.n++
	return fn(ctx)
}

// replied is a request with a text and a shown reply card.
func replied(text, reply string) ([]values.HistoryTurn, []values.HistoryTurn) {
	return []values.HistoryTurn{{StudentText: text}}, []values.HistoryTurn{
		{Calls: &values.SealedCalls{Calls: []values.RawCall{{CallID: "r1", Tool: "reply", ArgumentsJSON: fmt.Sprintf(`{"text":%q}`, reply)}}, Seal: []byte{1}}},
		{Results: []values.ToolResult{values.ShownResult("r1")}},
	}
}

func request(final, current bool, ended time.Time, line string) *stored {
	texts, details := replied("просьба", "ответ")
	return &stored{final: final, current: current, endedAt: ended, line: line, texts: texts, details: details}
}
