package agentjournal

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service. Compensator, Resolver, Threads and Processing may be nil (no Undo, PENDING
// entries become UNKNOWN at start, journal lines without a subject, no processing lines); Log may be nil.
type Deps struct {
	Actions     ActionRepository
	Compensator Compensator
	Resolver    Resolver
	Threads     Threads
	Processing  Processing
	Clock       Clock
	IDs         IDGenerator
	Log         *slog.Logger
}

// BeginInput names the call an entry records and what is known before its use case runs (Intent, for Recover).
type BeginInput struct {
	Thread shared.ThreadID
	Turn   int
	CallID string
	Tool   values.Tool
	Intent action.Intent
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
	ID           shared.ActionID
	Thread       shared.ThreadID
	Turn         int
	CallID       string
	Tool         values.Tool
	Status       action.Status
	CanUndo      bool
	NoUndo       action.NoUndo       // why «Отменить» is not offered; empty when it is
	Compensation action.Compensation // DONE and later: what Undo does (the Driver rebuilds a repeated call's result)
	Key          string              // idempotency key of the use case run between Begin and Finish
	Proposal     values.Ref          // PROPOSED only
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LineKind tells a journal line of the panel: an action of paratech or a processing job.
type LineKind string

// Line kinds.
const (
	LineAction     LineKind = "action"
	LineProcessing LineKind = "processing"
)

// Line is one line of «Журнал»: the actions of paratech (undone ones included) and the processing history, newest
// first. Only IDs and enums: the panel renders the texts.
type Line struct {
	Kind      LineKind
	At        time.Time        // when the action was taken or the job created
	UpdatedAt time.Time        // the last change (an undo, the end of a job)
	Subject   shared.SubjectID // the subject of the action's thread; empty for none or a job

	// An action line.
	Action  shared.ActionID
	Thread  shared.ThreadID
	Turn    int
	Tool    values.Tool
	Status  action.Status
	CanUndo bool
	NoUndo  action.NoUndo

	// A processing line.
	Job       shared.JobID
	JobType   job.Type
	JobStatus job.Status
}

// RecoverReport counts what Recover did.
type RecoverReport struct {
	Done    int // PENDING entries whose effect was found: DONE
	Failed  int // PENDING entries whose use case changed nothing: FAILED
	Unknown int // PENDING entries whose outcome cannot be told: UNKNOWN
	Undone  int // UNDOING entries whose compensation finished
	Refused int // UNDOING entries refused because the target changed
	Left    int // entries left for the next start (no compensator, or reading or compensating failed)
}

func entryOf(a *action.Action) Entry {
	st := a.State()
	can, why := a.Undoable()
	return Entry{
		ID: st.ID, Thread: st.Thread, Turn: st.Turn, CallID: st.CallID, Tool: st.Tool, Status: st.Status,
		CanUndo: can, NoUndo: why, Compensation: st.Compensation, Key: a.Key(), Proposal: st.Proposal,
		CreatedAt: st.CreatedAt, UpdatedAt: st.UpdatedAt,
	}
}
