package agentjournal

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service. Compensator may be nil until Undo is wired; Log may be nil.
type Deps struct {
	Actions     ActionRepository
	Compensator Compensator
	Clock       Clock
	IDs         IDGenerator
	Log         *slog.Logger
}

// BeginInput names the call an entry records.
type BeginInput struct {
	Thread shared.ThreadID
	Turn   int
	CallID string
	Tool   values.Tool
}

// Outcome is the result of the use case the entry recorded: success with its compensation and the version of the
// target right after the action, or a failure that changed nothing.
type Outcome struct {
	Failed        bool
	Compensation  action.Compensation
	TargetVersion int64
}

// Entry is a journal line for the panel and the Driver.
type Entry struct {
	ID        shared.ActionID
	Thread    shared.ThreadID
	Turn      int
	CallID    string
	Tool      values.Tool
	Status    action.Status
	CanUndo   bool
	Key       string     // idempotency key of the use case run between Begin and Finish
	Proposal  values.Ref // PROPOSED only
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RecoverReport counts what Recover did.
type RecoverReport struct {
	Unknown int // PENDING entries marked UNKNOWN
	Undone  int // UNDOING entries whose compensation finished
	Refused int // UNDOING entries refused because the target changed
	Left    int // UNDOING entries left for the next start (no compensator or it failed)
}

func entryOf(a *action.Action) Entry {
	st := a.State()
	return Entry{
		ID: st.ID, Thread: st.Thread, Turn: st.Turn, CallID: st.CallID, Tool: st.Tool, Status: st.Status,
		CanUndo: a.CanUndo(), Key: a.Key(), Proposal: st.Proposal, CreatedAt: st.CreatedAt, UpdatedAt: st.UpdatedAt,
	}
}
