package agentturn

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// AgentModel runs one planner step on the Hub (GatewayService.AgentStep, ADR-0023). Implemented by the hubclient
// adapter. Errors are those of Complete: shared.ErrNetworkUnavailable, shared.ErrServiceNotConfigured,
// shared.ErrServiceUnavailable, shared.ErrQuotaExceeded, backoff-classified transient and permanent failures, and
// values.ErrTurnGone when the Hub no longer keeps the model turn of the last echo. A repeated key returns the stored
// reply (Replayed), so «Продолжить» after a lost answer does not pay twice. The reply is untrusted: every call goes
// through values.ParseCall and the thread's Admit before anything runs (hard rule 3).
type AgentModel interface {
	Step(ctx context.Context, req values.StepRequest) (values.StepReply, error)
}

// Progress learns that a step of a request goes to the Hub, after the open step is stored and before the call, so the
// panel names the request (and offers «Стоп») before the step returns (ADR-0026). It is called under the service's
// lock: it must only publish, never block and never call back into the service. Implemented by input/agent over its
// Sink; optional.
type Progress interface {
	Started(ctx context.Context, st Step)
}

// Effects tells «Повторить» of a finished request (Rerun) whether the request left effects a repeat would double:
// Live is true when request turn of the thread has a journal entry that is not FAILED or UNDONE (an action that ran,
// one whose outcome is unknown, a proposal). Implemented by cases/agentjournal and wired by app (ARCH-20).
type Effects interface {
	Live(ctx context.Context, thread shared.ThreadID, turn int) (bool, error)
}

// ThreadRepository stores agent threads (one per chat, ADR-0028) in the local database. Implemented by the sqlite
// adapter.
type ThreadRepository interface {
	// Get loads a thread. It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.ThreadID) (*thread.Thread, error)

	// Current loads the current chat: the unarchived one with the newest last activity. It returns an error wrapping
	// shared.ErrNotFound if there is none.
	Current(ctx context.Context) (*thread.Thread, error)

	// Save inserts a new thread (version 0) or updates a stored one with a compare-and-swap on its version; a stale
	// version or an id stored first is shared.ErrVersionConflict.
	Save(ctx context.Context, t *thread.Thread) error

	// ListRunning returns the threads whose current request is PLANNING or EXECUTING, for startup recovery.
	ListRunning(ctx context.Context) ([]*thread.Thread, error)
}

// History reads and appends the history entries of a thread (student texts, sealed calls, results).
type History interface {
	// Append adds an entry of request turn at the end of the thread's history.
	Append(ctx context.Context, id shared.ThreadID, turn int, entry values.HistoryTurn) error

	// Since returns the last limit entries of the requests numbered from on, oldest first.
	Since(ctx context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error)
}

// Subjects gives the subject list of the shared base: IDs and the names the student gave.
type Subjects interface {
	BaseSubjects(ctx context.Context) ([]values.BaseSubject, error)
}

// CallFacts tells what the agent domain cannot see in a call before it runs: the cost estimate of a paid call,
// whether it would replace a ready result, and the triage outcome of the request. Facts.Context is filled by the
// case. Implemented by input/agent from the use cases that know (ADR-0021); an unknown cost stays zero (needs a plan).
type CallFacts interface {
	Facts(ctx context.Context, call values.Call) (thread.Facts, error)
}

// Tx runs fn in one write transaction: a thread and the history entries of the same change are saved together.
type Tx interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock returns the current time in the student's time zone (ARCH-24): today and the zone of the base come from it.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates thread IDs.
type IDGenerator interface {
	NewThreadID() shared.ThreadID
}
