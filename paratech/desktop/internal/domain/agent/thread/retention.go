package thread

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// DetailsRetention is how long the technical details of a final, earlier request (its sealed calls and their results)
// are kept after it ended (ADR-0028 «Retention»). Its texts, the frozen reply line and the summary are kept forever.
const DetailsRetention = 90 * 24 * time.Hour

// DetailsCutoff is the bound of the purge at now: an earlier final request that ended before it loses its details, one
// that ended at it or later keeps them. The store re-checks a purge against it.
func DetailsCutoff(now time.Time) time.Time { return now.Add(-DetailsRetention) }

// EndedRequest is an earlier, final request of a chat whose details the purge may remove: its chat and number, when it
// ended (its own end time) and whether its reply line is frozen. The purge reads them in (EndedAt, Thread, Number)
// order; the last one read is where the next read goes on.
type EndedRequest struct {
	Thread      shared.ThreadID
	Number      int
	EndedAt     time.Time
	ReplyFrozen bool
}

// FrozenReply is the reply line a request keeps once its details are purged: Reply of its stored entries, the line
// Window builds for it while they are kept. A line that is not a valid agent reply is refused (nothing may be purged
// then).
func FrozenReply(request []values.HistoryTurn) (string, error) {
	line := Reply(request)
	if err := (values.HistoryTurn{AgentReply: line}).Validate(); err != nil {
		return "", fmt.Errorf("reply line: %w", err)
	}
	return line, nil
}
