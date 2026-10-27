package agentturn

import (
	"log/slog"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service; every one except Progress, Effects, Mentions and Log is required.
type Deps struct {
	Model    AgentModel
	Threads  ThreadRepository
	History  History
	Subjects Subjects
	Facts    CallFacts
	Tx       Tx
	Clock    Clock
	IDs      IDGenerator
	Progress Progress  // nil: no update before a Hub step
	Effects  Effects   // nil: Rerun is refused with ErrInvalidConfig
	Mentions Mentioned // nil: no mention is kept, as none can be checked
	Log      *slog.Logger
}

// AskInput is a request typed in the panel.
type AskInput struct {
	ThreadID shared.ThreadID // the chat; empty: the current chat (a new one when there is none)
	Scope    values.Scope    // the context chip: the subject of this request, the only one sent
	Text     string
	Uploads  []values.Ref     // files dropped with the request (upload IDs)
	Mentions []values.Mention // «@» objects, at most values.MaxMentions; the ones that no longer exist are dropped
}

// TurnRef names the request a button belongs to; a button of an older request is ErrStaleTurn.
type TurnRef struct {
	Thread shared.ThreadID
	Turn   int
}

// IdleInput names the thread whose request an operation found running: by its ID, else the current chat (an Ask).
type IdleInput struct {
	Thread shared.ThreadID
}

// Outcome is what the Driver reports for one call it ran: the result built by the domain mapper (ADR-0022), the IDs
// it showed the planner (they become known to the provenance check) and the ID a create call made.
type Outcome struct {
	Result  values.ToolResult
	Seen    []values.Ref
	Created values.Ref
}

// SubmitInput is the results of the calls the Driver ran.
type SubmitInput struct {
	Turn     TurnRef
	Outcomes []Outcome
}

// Call is a call the Driver runs: R reads, U actions (journaled, with «Отменить») and C proposals (a confirm card;
// the action itself only on the student's click). The case runs no tool.
type Call struct {
	Call     values.Call
	Class    actionpolicy.Class
	Paid     bool
	PlanStep int // the approved step it executes; -1 none
}

// Step is the state of the request after an operation: the calls to run and the cards to show, in order. Scope is
// the scope of the request, the default subject of the read tools.
type Step struct {
	Thread     shared.ThreadID
	Turn       int
	Scope      values.Scope
	Status     thread.Status
	StopReason values.StopReason
	Calls      []Call
	Cards      []values.Card
}
