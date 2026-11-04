package agentcompact

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts.
const (
	opNew     = "agentcompact: new"
	opCompact = "agentcompact: compact"
)

// maxSaves bounds the saves of one compaction when a request of the chat keeps saving in between.
const maxSaves = 3

// maxRefused bounds the refused keys remembered; the oldest is forgotten first.
const maxRefused = 256

// Deps are the ports of the service; all but Log are required.
type Deps struct {
	Chats      ChatRepository
	History    History
	Summarizer Summarizer
	Clock      Clock
	Log        *slog.Logger // nil: discard
}

// Fill is the context ring of a chat (ADR-0028 «The ring»), as agentchats shows it: how much of the window its next
// request would use, in whole percent (100 on an Overflow), its level, and whether an earlier request is left out.
type Fill struct {
	Percent  int
	Level    thread.FillLevel
	Overflow bool
}

// Result is the outcome of Compact: whether a new summary was stored, and the fill of the chat's next request after it.
type Result struct {
	Compacted bool
	Fill      Fill
}

// Service compacts agent chats.
type Service struct {
	chats      ChatRepository
	history    History
	summarizer Summarizer
	clock      Clock
	log        *slog.Logger

	// mu keeps one compaction, so one Hub call, at a time: a second Compact waits and sees the stored summary.
	mu sync.Mutex
	// refused are the keys whose summary the Hub refused for good, oldest first: the Hub replays the refusal.
	refused []string
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Chats == nil || d.History == nil || d.Summarizer == nil || d.Clock == nil {
		return nil, fmt.Errorf("%s: %w: every port except Log is required", opNew, ErrInvalidConfig)
	}
	s := &Service{chats: d.Chats, history: d.History, summarizer: d.Summarizer, clock: d.Clock, log: d.Log}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Compact makes and stores a new summary of chat id when its next request needs one (thread.CompactionRequest); an
// empty id is the current chat, and none is an empty Result. A chat with no request, or one that needs no compaction,
// returns its fill without calling the Hub. A failure returns one of the errors of this package and stores nothing; a
// missing chat or a failed read of the store passes through wrapped.
func (s *Service) Compact(ctx context.Context, id shared.ThreadID) (Result, error) {
	if id != "" {
		if err := id.Validate(); err != nil {
			return Result{}, fmt.Errorf("%s: %w: %w", opCompact, ErrInvalidInput, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	th, err := s.load(ctx, id)
	if errors.Is(err, shared.ErrNotFound) && id == "" {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", opCompact, err)
	}
	if _, ok := th.Turn(); !ok {
		return Result{Fill: Fill{Level: thread.FillNormal}}, nil
	}
	entries, err := s.history.Since(ctx, th.ID(), 1, thread.HistoryReadLimit)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", opCompact, err)
	}

	req, ok, err := th.CompactionRequest(entries)
	if err != nil {
		// A request the Hub limits cannot take is not sent at all.
		code := ErrRefused
		if errors.Is(err, thread.ErrTurnInProgress) || errors.Is(err, thread.ErrInvalidTransition) {
			code = ErrBusy
		}
		return Result{}, s.fail(ctx, th.ID(), code, err)
	}
	if !ok {
		return Result{Fill: fillOf(th, entries)}, nil
	}
	if s.wasRefused(req.Key()) {
		return Result{}, s.fail(ctx, th.ID(), ErrRefused, errors.New("refused before for the same key"))
	}

	text, err := s.summarizer.Summarize(ctx, req)
	if err != nil {
		code := codeOf(ctx, err)
		if errors.Is(err, values.ErrSummaryRefused) {
			s.remember(req.Key())
		}
		return Result{}, s.fail(ctx, th.ID(), code, err)
	}
	summary, err := values.ParseHistorySummary(req.Through(), text)
	if err != nil {
		// The Hub replays the same answer for the key: asking again gives the same text.
		s.remember(req.Key())
		return Result{}, s.fail(ctx, th.ID(), ErrRefused, err)
	}

	if th, err = s.store(ctx, th, summary); err != nil {
		return Result{}, err
	}
	return Result{Compacted: true, Fill: fillOf(th, entries)}, nil
}

// load reads chat id, or the current chat when id is empty.
func (s *Service) load(ctx context.Context, id shared.ThreadID) (*thread.Thread, error) {
	if id == "" {
		return s.chats.Current(ctx)
	}
	return s.chats.Get(ctx, id)
}

// store applies summary and saves the chat. On a version conflict the chat is reloaded and the same summary applied
// again: the thread checks once more that it covers more than the stored one and only requests that are over. The Hub
// is never asked again (its answer for the key would be the same).
func (s *Service) store(ctx context.Context, th *thread.Thread, summary values.HistorySummary) (*thread.Thread, error) {
	for attempt := 1; ; attempt++ {
		if err := th.Compact(summary, s.clock.Now()); err != nil {
			code := ErrChanged
			if attempt == 1 && errors.Is(err, thread.ErrInvalidTransition) {
				code = ErrBusy
			}
			return nil, s.fail(ctx, th.ID(), code, err)
		}
		err := s.chats.Save(ctx, th)
		if err == nil {
			return th, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, fmt.Errorf("%s: %w", opCompact, err)
		}
		if attempt == maxSaves {
			return nil, s.fail(ctx, th.ID(), ErrChanged, err)
		}
		if th, err = s.chats.Get(ctx, th.ID()); err != nil {
			return nil, fmt.Errorf("%s: %w", opCompact, err)
		}
	}
}

// fail logs a failed compaction by chat and code (never a text) and returns the code with its cause.
func (s *Service) fail(ctx context.Context, id shared.ThreadID, code, cause error) error {
	s.log.WarnContext(ctx, "agent compaction failed", "thread", id.String(), "code", code.Error(), "err", cause)
	return fmt.Errorf("%s: %w: %w", opCompact, code, cause)
}

// codeOf classifies a failed Summarize as agentturn classifies a failed step: the caller going away is unavailable
// whatever the error; a quota is ErrQuota; a refused answer or any failure a retry cannot fix is ErrRefused; the rest
// (network, Hub not set up, unavailable, busy, rate limited, timeout) is ErrUnavailable.
func codeOf(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return ErrUnavailable
	case errors.Is(err, shared.ErrQuotaExceeded):
		return ErrQuota
	case errors.Is(err, values.ErrSummaryRefused), errors.Is(err, backoff.ErrPermanent):
		return ErrRefused
	}
	return ErrUnavailable
}

func (s *Service) wasRefused(key string) bool { return slices.Contains(s.refused, key) }

// remember keeps a refused key, forgetting the oldest beyond maxRefused.
func (s *Service) remember(key string) {
	if s.wasRefused(key) {
		return
	}
	if len(s.refused) == maxRefused {
		s.refused = s.refused[1:]
	}
	s.refused = append(s.refused, key)
}

// fillOf is the fill of the chat's next request (thread.NextFill) on the entries read, with the chat's summary.
func fillOf(th *thread.Thread, entries []values.HistoryEntry) Fill {
	f := th.NextFill(entries)
	return Fill{Percent: f.Percent(), Level: f.Level(), Overflow: f.Overflow}
}
