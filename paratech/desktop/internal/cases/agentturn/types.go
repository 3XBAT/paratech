package agentturn

import (
	"context"
	"log/slog"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service; every one except Log is required.
type Deps struct {
	Model    AgentModel
	Threads  ThreadRepository
	History  History
	Subjects Subjects
	Facts    CallFacts
	Tx       Tx
	Clock    Clock
	IDs      IDGenerator
	Log      *slog.Logger
}

// AskInput is a request typed in the panel.
type AskInput struct {
	Scope   values.Scope // the context chip picks the thread; only this subject is sent
	Text    string
	Uploads []values.Ref // files dropped with the request (upload IDs)
}

// TurnRef names the request a button belongs to; a button of an older request is ErrStaleTurn.
type TurnRef struct {
	Thread shared.ThreadID
	Turn   int
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

// Step is the state of the request after an operation: the calls to run and the cards to show, in order.
type Step struct {
	Thread     shared.ThreadID
	Turn       int
	Status     thread.Status
	StopReason values.StopReason
	Calls      []Call
	Cards      []values.Card
}

// UnknownFacts is the CallFacts of a desktop that cannot estimate calls yet: the cost is unknown (so every paid
// write needs a plan), no ready result and no triage are known. The agent input layer replaces it.
type UnknownFacts struct{}

// Facts returns zero facts.
func (UnknownFacts) Facts(context.Context, values.Call) (thread.Facts, error) { return thread.Facts{}, nil }
