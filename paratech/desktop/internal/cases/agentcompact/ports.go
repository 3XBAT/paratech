package agentcompact

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

	// Save updates a stored chat with a compare-and-swap on its version; a stale version is shared.ErrVersionConflict.
	Save(ctx context.Context, t *thread.Thread) error
}

// History reads the history entries of a chat.
type History interface {
	// Since returns the last limit entries of the requests numbered from on, oldest first; dropped requests are left
	// out.
	Since(ctx context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error)
}

// Summarizer asks the Hub for the summary of a chat's earlier requests. Implemented by the hubclient adapter.
type Summarizer interface {
	// Summarize returns the summary text: untrusted model output. Errors match shared.ErrQuotaExceeded for a quota,
	// values.ErrSummaryRefused for an answer the Hub refused for good (the same key replays the refusal), and
	// backoff.ErrPermanent for any failure a retry cannot fix.
	Summarize(ctx context.Context, req values.SummaryRequest) (string, error)
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}
