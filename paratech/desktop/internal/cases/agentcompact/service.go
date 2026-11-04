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
// returns its fill without calling the Hub. A failure returns exactly one of the errors of this package, its cause
// wrapped (a missing chat is ErrStore and shared.ErrNotFound), and stores nothing.
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
		return Result{}, s.storeFail(ctx, id, err)
	}
	if _, ok := th.Turn(); !ok {
		return Result{Fill: Fill{Level: thread.FillNormal}}, nil
	}
	entries, err := s.history.Since(ctx, th.ID(), 1, thread.HistoryReadLimit)
	if err != nil {
		return Result{}, s.storeFail(ctx, th.ID(), err)
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

	if th, entries, err = s.store(ctx, th, entries, req, summary); err != nil {
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

// errRequestChanged is the cause of ErrChanged when the reloaded chat would ask for another summary than the one made.
var errRequestChanged = errors.New("the chat would ask for another summary now")

// store applies summary, made for req, and saves the chat; it returns the chat saved and the history entries it was
// checked against. On a version conflict the chat and its history are reloaded and the summary request rebuilt: the
// summary is applied again only when the rebuilt request is the same as req. Otherwise (a «Скрыть» or a restart forgot
// some of the requests summarized, another summary was stored, a request started) the summary no longer fits the
// chat: ErrChanged, nothing stored. The Hub is never asked again (its answer for the key would be the same).
func (s *Service) store(ctx context.Context, th *thread.Thread, entries []values.HistoryEntry, req values.SummaryRequest,
	summary values.HistorySummary,
) (*thread.Thread, []values.HistoryEntry, error) {
	for attempt := 1; ; attempt++ {
		if err := th.Compact(summary, s.clock.Now()); err != nil {
			code := ErrChanged
			if attempt == 1 && errors.Is(err, thread.ErrInvalidTransition) {
				code = ErrBusy
			}
			return nil, nil, s.fail(ctx, th.ID(), code, err)
		}
		err := s.chats.Save(ctx, th)
		if err == nil {
			return th, entries, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, nil, s.storeFail(ctx, th.ID(), err)
		}
		if attempt == maxSaves {
			return nil, nil, s.fail(ctx, th.ID(), ErrChanged, err)
		}
		id := th.ID()
		if th, err = s.chats.Get(ctx, id); err != nil {
			return nil, nil, s.storeFail(ctx, id, err)
		}
		if entries, err = s.history.Since(ctx, id, 1, thread.HistoryReadLimit); err != nil {
			return nil, nil, s.storeFail(ctx, id, err)
		}
		again, ok, err := th.CompactionRequest(entries)
		switch {
		case err != nil:
			return nil, nil, s.fail(ctx, id, ErrChanged, fmt.Errorf("%w: %w", errRequestChanged, err))
		case !ok || !again.Equal(req):
			return nil, nil, s.fail(ctx, id, ErrChanged, errRequestChanged)
		}
	}
}

// storeFail codes a failed read or save of the store: ErrUnavailable when the caller went away, else ErrStore.
func (s *Service) storeFail(ctx context.Context, id shared.ThreadID, err error) error {
	code := ErrStore
	if ctx.Err() != nil {
		code = ErrUnavailable
	}
	return s.fail(ctx, id, code, err)
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
