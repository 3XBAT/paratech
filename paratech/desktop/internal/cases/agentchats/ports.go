package agentchats

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// ChatRepository stores the agent chats (threads) in the local database. Implemented by the sqlite adapter.
type ChatRepository interface {
	// Get loads a chat. It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.ThreadID) (*thread.Thread, error)

	// Current loads the unarchived chat with the newest last activity; none wraps shared.ErrNotFound.
	Current(ctx context.Context) (*thread.Thread, error)

	// List returns every chat: the unarchived ones by last activity, newest first, then the archived ones in the same
	// order.
	List(ctx context.Context) ([]*thread.Thread, error)

	// RequestMentions returns the mentions stored with request turn of a chat; a missing request wraps
	// shared.ErrNotFound.
	RequestMentions(ctx context.Context, id shared.ThreadID, turn int) ([]values.Mention, error)

	// Save inserts a new chat (version 0) or updates a stored one with a compare-and-swap on its version; a stale
	// version or an id stored first is shared.ErrVersionConflict.
	Save(ctx context.Context, t *thread.Thread) error
}

// History reads the history entries of a chat.
type History interface {
	// Since returns the last limit entries of the requests numbered from on, oldest first; dropped requests are left
	// out.
	Since(ctx context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error)
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates chat IDs.
type IDGenerator interface {
	NewThreadID() shared.ThreadID
}
