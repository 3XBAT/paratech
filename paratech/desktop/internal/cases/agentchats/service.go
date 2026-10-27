package agentchats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts.
const (
	opNew     = "agentchats: new"
	opList    = "agentchats: list"
	opNewChat = "agentchats: new chat"
	opRename  = "agentchats: rename"
	opArchive = "agentchats: archive"
	opRestore = "agentchats: restore"
	opEarlier = "agentchats: earlier requests"
)

// maxSaves bounds the saves of one change when a request of the chat keeps saving in between.
const maxSaves = 3

// earlierLimit is the most history entries Earlier reads: the newest requests of a long chat; the oldest one read may
// lose its text and is left out.
const earlierLimit = 1024

// Deps are the ports of the service; all are required.
type Deps struct {
	Chats   ChatRepository
	History History
	Clock   Clock
	IDs     IDGenerator
}

// Chat is a line of the chat list.
type Chat struct {
	ID         shared.ThreadID
	Title      string
	Archived   bool
	LastActive time.Time
	Running    bool // the current request is PLANNING or EXECUTING
}

// Request is an earlier request of a chat, read-only: the student's text without the upload IDs, the mentions with
// the names shown at mention time, and the reply line (see replyLine). Current marks the chat's current request.
type Request struct {
	Turn     int
	Text     string
	Mentions []values.Mention
	Reply    string
	Current  bool
}

// Service runs the chat list operations.
type Service struct {
	chats   ChatRepository
	history History
	clock   Clock
	ids     IDGenerator

	mu sync.Mutex // New: two clicks give one empty chat
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Chats == nil || d.History == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%s: %w: every port is required", opNew, ErrInvalidConfig)
	}
	return &Service{chats: d.Chats, history: d.History, clock: d.Clock, ids: d.IDs}, nil
}

// List returns every chat: the unarchived ones by last activity, newest first, then the archive.
func (s *Service) List(ctx context.Context) ([]Chat, error) {
	list, err := s.chats.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	out := make([]Chat, 0, len(list))
	for _, th := range list {
		out = append(out, chatOf(th))
	}
	return out, nil
}

// New is «Новый чат»: the current chat when it has no request yet, else a new empty chat, which becomes current.
func (s *Service) New(ctx context.Context) (Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.chats.Current(ctx)
	switch {
	case err == nil && cur.IsEmpty():
		return chatOf(cur), nil
	case err != nil && !errors.Is(err, shared.ErrNotFound):
		return Chat{}, fmt.Errorf("%s: %w", opNewChat, err)
	}
	th, err := thread.New(s.ids.NewThreadID(), s.clock.Now())
	if err != nil {
		return Chat{}, fmt.Errorf("%s: %w", opNewChat, err)
	}
	if err := s.chats.Save(ctx, th); err != nil {
		return Chat{}, fmt.Errorf("%s: %w", opNewChat, err)
	}
	return chatOf(th), nil
}

// Rename sets the title the student typed (thread.Rename checks it). While the chat's request runs it is ErrBusy:
// the step loop holds the chat between its saves, and a save in between would make it drop the step's reply.
func (s *Service) Rename(ctx context.Context, id shared.ThreadID, title string) (Chat, error) {
	return s.update(ctx, id, opRename, func(th *thread.Thread, now time.Time) error {
		if running(th) {
			return ErrBusy
		}
		return th.Rename(title, now)
	})
}

// Archive is «В архив»; a chat whose request is not final is refused by the thread (thread.ErrInvalidTransition).
func (s *Service) Archive(ctx context.Context, id shared.ThreadID) (Chat, error) {
	return s.update(ctx, id, opArchive, (*thread.Thread).Archive)
}

// Restore is «Вернуть»: the chat leaves the archive.
func (s *Service) Restore(ctx context.Context, id shared.ThreadID) (Chat, error) {
	return s.update(ctx, id, opRestore, (*thread.Thread).Restore)
}

// update loads the chat, applies move and saves it, again on a version conflict (a request saved in between).
func (s *Service) update(ctx context.Context, id shared.ThreadID, op string, move func(*thread.Thread, time.Time) error) (Chat, error) {
	if err := id.Validate(); err != nil {
		return Chat{}, fmt.Errorf("%s: %w: %w", op, ErrInvalidInput, err)
	}
	for attempt := 1; ; attempt++ {
		th, err := s.chats.Get(ctx, id)
		if err != nil {
			return Chat{}, fmt.Errorf("%s: %w", op, err)
		}
		if err := move(th, s.clock.Now()); err != nil {
			return Chat{}, fmt.Errorf("%s: %w", op, err)
		}
		err = s.chats.Save(ctx, th)
		if errors.Is(err, shared.ErrVersionConflict) && attempt < maxSaves {
			continue
		}
		if err != nil {
			return Chat{}, fmt.Errorf("%s: %w", op, err)
		}
		return chatOf(th), nil
	}
}

// Earlier returns the requests of a chat, oldest first, the current one last. Dropped requests are not returned.
func (s *Service) Earlier(ctx context.Context, id shared.ThreadID) ([]Request, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", opEarlier, ErrInvalidInput, err)
	}
	th, err := s.chats.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opEarlier, err)
	}
	current, ok := th.Turn()
	if !ok {
		return nil, nil
	}
	entries, err := s.history.Since(ctx, id, 1, earlierLimit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opEarlier, err)
	}

	var out []Request
	for _, g := range requestsOf(entries) {
		r := Request{Turn: g.turn, Text: studentText(g.text), Reply: replyLine(g.entries), Current: g.turn == current.Number()}
		if r.Current {
			r.Mentions = current.Mentions()
		} else if r.Mentions, err = s.chats.RequestMentions(ctx, id, g.turn); err != nil {
			return nil, fmt.Errorf("%s: request %d: %w", opEarlier, g.turn, err)
		}
		out = append(out, r)
	}
	return out, nil
}

func chatOf(th *thread.Thread) Chat {
	return Chat{ID: th.ID(), Title: th.Title(), Archived: th.Archived(), LastActive: th.LastActive(), Running: running(th)}
}

// running reports whether the chat's request is PLANNING or EXECUTING, as agentturn tells a running request.
func running(th *thread.Thread) bool {
	turn, ok := th.Turn()
	return ok && (turn.Status() == thread.StatusPlanning || turn.Status() == thread.StatusExecuting)
}
