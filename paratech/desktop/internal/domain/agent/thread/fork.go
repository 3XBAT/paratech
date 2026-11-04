package thread

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// forkSuffix ends the title of a fork, in English (owner decision, ADR-0028 Open 9 → а).
const forkSuffix = " (fork)"

// ForkedFrom returns the chat this one was forked from (ADR-0028 «Restart from an earlier message»); empty when it is
// not a fork. Informational only: the source may be gone, nothing follows the link.
func (th *Thread) ForkedFrom() shared.ThreadID { return th.forkedFrom }

// checkFork is the Restore check of the fork source: a valid id that is not the chat's own.
func checkFork(s State) error {
	if s.ForkedFrom == "" {
		return nil
	}
	if err := s.ForkedFrom.Validate(); err != nil {
		return err
	}
	if s.ForkedFrom == s.ID {
		return errors.New("a chat forked from itself")
	}
	return nil
}

// PastRequest is a stored request of a chat as a fork reads it; the store never reads a hidden («Скрыть») one.
type PastRequest struct {
	Number    int
	Scope     values.Scope
	Mentions  []values.Mention
	Texts     []string // the student's texts in order, as stored; at least one
	ReplyLine string   // the frozen line; "" when the request has none
	// Entries are the stored entries of the request, read only when ReplyLine is "" (Reply builds the line from them).
	Entries   []values.HistoryTurn
	StartedAt time.Time
	EndedAt   time.Time // the request's own last change
}

// ForkedRequest is a copy of an earlier request as the fork stores it: texts, mentions, scope and reply line only.
type ForkedRequest struct {
	Number    int
	Scope     values.Scope
	Mentions  []values.Mention
	Texts     []string
	ReplyLine string
	StartedAt time.Time
	UpdatedAt time.Time
}

// State is the stored form of the copy: DONE with its reply line, no budget, steps, plan, calls or created ids.
func (f ForkedRequest) State() TurnState {
	return TurnState{
		Number: f.Number, Scope: f.Scope, Mentions: slices.Clone(f.Mentions), Status: StatusDone,
		ReplyLine: f.ReplyLine, StartedAt: f.StartedAt, UpdatedAt: f.UpdatedAt,
	}
}

// Fork is a new chat forked from another: Thread and the copies of the requests, oldest first. The last copy is the
// thread's current request (stored by the thread's own save); the store writes the others and every copy's texts.
type Fork struct {
	Thread   *Thread
	Requests []ForkedRequest
}

// ForkOf is «Начать заново отсюда» → «Форк» (ADR-0028): a new chat id with copies of the requests of src before n,
// numbers kept. past are the stored requests of src that are not hidden, up to n, oldest first, n the last: n itself
// is not copied (its text goes to the input of the new chat), and hidden requests are not copied at all, so the
// numbers may have gaps. A copy keeps its texts, mentions, scope and times; its reply line is the frozen one, or Reply
// of its entries as Window would send it. Nothing else is copied: no steps, seals, plan, budget, actions or seen ids.
// The summary is copied only when it covers requests before n that are all among the copies' range, the title is the
// source's cut so that it fits with « (fork)». A request n that is not in past, out of range or without a text is
// ErrInvalidFork. The source does not change.
func ForkOf(src *Thread, n int, past []PastRequest, id shared.ThreadID, now time.Time) (Fork, error) {
	bad := func(format string, a ...any) (Fork, error) {
		return Fork{}, fmt.Errorf("%w: %s", ErrInvalidFork, fmt.Sprintf(format, a...))
	}
	if src == nil {
		return bad("no source chat")
	}
	if n < 1 || n > src.turns {
		return bad("no request %d to fork from (last %d)", n, src.turns)
	}
	if len(past) == 0 || past[len(past)-1].Number != n {
		return bad("request %d is hidden or not stored", n)
	}
	for i, p := range past {
		if p.Number < 1 || (i > 0 && p.Number <= past[i-1].Number) {
			return bad("requests out of order")
		}
		if !hasText(p) {
			if i == len(past)-1 {
				return bad("request %d has no text", p.Number)
			}
			continue // an earlier request without a text is not copied
		}
		for _, text := range p.Texts {
			if text == "" || (values.HistoryTurn{StudentText: text}).Validate() != nil {
				return bad("request %d has a bad text", p.Number)
			}
		}
	}

	th, err := New(id, now)
	if err != nil {
		return Fork{}, err
	}
	copies := make([]ForkedRequest, 0, len(past)-1)
	for _, p := range past[:len(past)-1] {
		if !hasText(p) {
			continue
		}
		line := p.ReplyLine
		if line == "" {
			line = Reply(p.Entries)
		}
		c := ForkedRequest{
			Number: p.Number, Scope: p.Scope, Mentions: slices.Clone(p.Mentions), Texts: slices.Clone(p.Texts),
			ReplyLine: line, StartedAt: p.StartedAt, UpdatedAt: p.EndedAt,
		}
		if _, err := restoreTurn(c.State(), c.Number); err != nil {
			return Fork{}, fmt.Errorf("%w: %w", ErrInvalidFork, err)
		}
		copies = append(copies, c)
	}

	s := th.State()
	s.Version, s.ForkedFrom, s.Title = 1, src.id, forkTitle(src.title)
	if len(copies) > 0 {
		last := copies[len(copies)-1]
		cur := last.State()
		s.Turns, s.Current = last.Number, &cur
		if through := src.summary.Through(); !src.summary.IsZero() && through < n && through <= last.Number {
			s.Summary, s.SummaryAt = src.summary, now
		}
	}
	// The checks of a stored chat hold for the new one; it is not stored yet.
	fork, err := Restore(s)
	if err != nil {
		return Fork{}, fmt.Errorf("%w: %w", ErrInvalidFork, err)
	}
	fork.version = 0
	return Fork{Thread: fork, Requests: copies}, nil
}

// hasText reports whether the request has a non-empty student text.
func hasText(p PastRequest) bool {
	return slices.ContainsFunc(p.Texts, func(t string) bool { return t != "" })
}

// forkTitle is title with forkSuffix, the title cut on a rune boundary and trimmed so the whole fits MaxTitleRunes.
func forkTitle(title string) string {
	runes := []rune(title)
	if keep := MaxTitleRunes - len([]rune(forkSuffix)); len(runes) > keep {
		runes = runes[:keep]
	}
	base := strings.TrimSpace(string(runes))
	if base == "" {
		base = DefaultTitle
	}
	return base + forkSuffix
}
