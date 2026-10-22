package wails

import (
	"context"
	"log/slog"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// AgentUpdateEvent is the Wails event of the agent panel; its data is one AgentUpdateView.
const AgentUpdateEvent = "agent:update"

// Operation names of the log.
const (
	opAgentAsk      = "ui: agent ask"
	opAgentApprove  = "ui: agent approve plan"
	opAgentRevise   = "ui: agent revise plan"
	opAgentAnswer   = "ui: agent answer questions"
	opAgentStop     = "ui: agent stop"
	opAgentContinue = "ui: agent continue"
	opAgentRetry    = "ui: agent retry"
	opAgentRestart  = "ui: agent restart"
	opAgentCancel   = "ui: agent cancel"
	opAgentLink     = "ui: agent hub link"
)

// Problems of the agent calls (AgentResult.Problem). The outcome of an accepted call comes as an AgentUpdateEvent.
const (
	AgentProblemNone        = ""
	AgentProblemInvalid     = "invalid"     // malformed ids, turn or answers
	AgentProblemOffline     = "offline"     // Ask while the Hub is not set up (AgentState says why); the panel is read-only
	AgentProblemClosed      = "closed"      // the app is closing
	AgentProblemUnavailable = "unavailable" // not available yet (Undo, History)
	AgentProblemFailed      = "failed"
)

// Hub link states of AgentState.
const (
	AgentLinkReady      = "ready"
	AgentLinkNoHub      = "no_hub"       // no Hub address
	AgentLinkNoKey      = "no_key"       // no Hub key
	AgentLinkNoKeyStore = "no_key_store" // no credential store on this system
	AgentLinkUnknown    = "unknown"      // the settings or the key store could not be read
)

// agentUpdateProblems are the problems of the Driver the panel knows; any other becomes "failed".
var agentUpdateProblems = map[string]bool{
	"busy": true, "stale": true, "invalid": true, "budget": true, "retry_required": true, "restart_required": true,
	"failed": true,
}

// AgentDriver is the agent loop (input/agent.Driver) as the panel uses it (ARCH-42); app wires it in, as input/wails
// must not import input/agent. Each call starts the operation and returns; its updates go to the AgentUpdate sink.
type AgentDriver interface {
	Ask(ctx context.Context, in agentturn.AskInput) error
	ApprovePlan(ctx context.Context, ref agentturn.TurnRef) error
	RevisePlan(ctx context.Context, ref agentturn.TurnRef, wish string) error
	AnswerQuestions(ctx context.Context, ref agentturn.TurnRef, answers []values.Answer) error
	Stop(ctx context.Context, ref agentturn.TurnRef) error
	Continue(ctx context.Context, ref agentturn.TurnRef) error
	Retry(ctx context.Context, ref agentturn.TurnRef) error
	Restart(ctx context.Context, ref agentturn.TurnRef) error
	Cancel(ctx context.Context, ref agentturn.TurnRef) error
}

// AgentLink tells whether the Hub is set up; satisfied by connections.Service.
type AgentLink interface {
	Get(ctx context.Context) (connections.View, error)
}

// AgentCore is what the agent panel needs. Life must live as long as the app: the Driver's operations outlive the
// Wails call that starts them (TASK-117 handoff).
type AgentCore struct {
	Driver AgentDriver
	Link   AgentLink
	Panel  *AgentPanel
	Life   context.Context
}

func (c AgentCore) complete() bool {
	return c.Driver != nil && c.Link != nil && c.Panel != nil && c.Life != nil
}

// AgentAskInput is a request typed in the panel with the context the UI chose. Empty ids mean none; an empty
// subjectId is the thread «Без предмета». Screen is "", today, subjects, assignments, schedule, journal or settings.
type AgentAskInput struct {
	Text         string `json:"text"`
	SubjectID    string `json:"subjectId"`
	LessonID     string `json:"lessonId"`
	AssignmentID string `json:"assignmentId"`
	Screen       string `json:"screen"`
}

// AgentTurnRef names the request a button belongs to.
type AgentTurnRef struct {
	ThreadID string `json:"threadId"`
	Turn     int    `json:"turn"`
}

// AgentAnswerInput is the answer to question Question: Option of a choice, or Text the student typed.
type AgentAnswerInput struct {
	Question int    `json:"question"`
	Option   int    `json:"option"`
	Text     string `json:"text"`
}

// AgentResult is the answer of a panel call: Problem is "" when the operation started.
type AgentResult struct {
	Problem string `json:"problem"`
}

// AgentState is what the panel shows before any request: ReadOnly when the Hub is not set up («Нет связи с
// paratech»); Link says why.
type AgentState struct {
	Link     string `json:"link"`
	ReadOnly bool   `json:"readOnly"`
}

// AgentHistoryView is the stored requests of a thread, oldest first. Problem is "unavailable" until the history read
// exists.
type AgentHistoryView struct {
	Problem  string            `json:"problem"`
	Requests []AgentUpdateView `json:"requests"`
}

// AgentService is bound to the agent panel ("<package path>.AgentService.<Method>").
type AgentService struct {
	driver AgentDriver
	link   AgentLink
	life   context.Context
	log    *slog.Logger
}

// NewAgentService returns the binding of the agent panel; OpenServices checks the core first. The shell attaches
// c.Panel to its events itself.
func NewAgentService(c AgentCore, log *slog.Logger) *AgentService {
	return &AgentService{driver: c.Driver, link: c.Link, life: c.Life, log: log}
}

// State returns the Hub link of the panel; it calls no Hub.
func (s *AgentService) State(ctx context.Context) AgentState {
	link := s.linkState(ctx)
	return AgentState{Link: link, ReadOnly: link != AgentLinkReady}
}

// Ask starts a request; refused with "offline" while the Hub is not set up.
func (s *AgentService) Ask(ctx context.Context, in AgentAskInput) AgentResult {
	scope, ok := scopeOf(in)
	if !ok {
		return AgentResult{Problem: AgentProblemInvalid}
	}
	if s.linkState(ctx) != AgentLinkReady {
		return AgentResult{Problem: AgentProblemOffline}
	}
	return s.started(opAgentAsk, s.driver.Ask(s.life, agentturn.AskInput{Scope: scope, Text: in.Text}))
}

// ApprovePlan is «Да» on the plan card.
func (s *AgentService) ApprovePlan(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentApprove, ref, s.driver.ApprovePlan)
}

// RevisePlan is «Изменить» on the plan card with the change the student typed.
func (s *AgentService) RevisePlan(_ context.Context, ref AgentTurnRef, wish string) AgentResult {
	return s.onTurn(opAgentRevise, ref, func(ctx context.Context, r agentturn.TurnRef) error {
		return s.driver.RevisePlan(ctx, r, wish)
	})
}

// Answer sends the answers of a questions card.
func (s *AgentService) Answer(_ context.Context, ref AgentTurnRef, answers []AgentAnswerInput) AgentResult {
	if len(answers) == 0 || len(answers) > values.MaxQuestions {
		return AgentResult{Problem: AgentProblemInvalid}
	}
	out := make([]values.Answer, 0, len(answers))
	for _, a := range answers {
		out = append(out, values.Answer{Question: a.Question, Option: a.Option, Text: values.Text(a.Text)})
	}
	return s.onTurn(opAgentAnswer, ref, func(ctx context.Context, r agentturn.TurnRef) error {
		return s.driver.AnswerQuestions(ctx, r, out)
	})
}

// Stop is «Стоп».
func (s *AgentService) Stop(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentStop, ref, s.driver.Stop)
}

// Continue is «Продолжить» after a stop, a lost connection or the quota.
func (s *AgentService) Continue(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentContinue, ref, s.driver.Continue)
}

// Retry is «Повторить» after a step failed for good.
func (s *AgentService) Retry(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentRetry, ref, s.driver.Retry)
}

// Restart starts an expired request again from its text.
func (s *AgentService) Restart(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentRestart, ref, s.driver.Restart)
}

// Cancel is «Отмена».
func (s *AgentService) Cancel(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentCancel, ref, s.driver.Cancel)
}

// Undo is «Отменить» of an action line; not available yet (TASK-123).
func (s *AgentService) Undo(context.Context, string) AgentResult {
	return AgentResult{Problem: AgentProblemUnavailable}
}

// History returns the stored requests of the thread of subjectID ("" «Без предмета»); not available yet: there is
// no read use case of the stored requests.
func (s *AgentService) History(context.Context, string) AgentHistoryView {
	return AgentHistoryView{Problem: AgentProblemUnavailable, Requests: []AgentUpdateView{}}
}

// onTurn checks ref and runs op on the app-lifetime ctx: the call's ctx ends with the call.
func (s *AgentService) onTurn(op string, ref AgentTurnRef, run func(context.Context, agentturn.TurnRef) error) AgentResult {
	r := agentturn.TurnRef{Thread: shared.ThreadID(ref.ThreadID), Turn: ref.Turn}
	if r.Thread.Validate() != nil || r.Turn < 1 {
		return AgentResult{Problem: AgentProblemInvalid}
	}
	return s.started(op, run(s.life, r))
}

func (s *AgentService) started(op string, err error) AgentResult {
	if err == nil {
		return AgentResult{}
	}
	// The app closes its Driver only after Life ended.
	if s.life.Err() != nil {
		return AgentResult{Problem: AgentProblemClosed}
	}
	s.log.Warn(op, "err", err)
	return AgentResult{Problem: AgentProblemFailed}
}

// linkState reads whether the Hub address and key are set; a failure is logged and reads as unknown.
func (s *AgentService) linkState(ctx context.Context) string {
	v, err := s.link.Get(ctx)
	if err != nil {
		s.log.Warn(opAgentLink, "err", err)
		return AgentLinkUnknown
	}
	switch {
	case v.HubURL == "":
		return AgentLinkNoHub
	case v.HubKey == connections.SecretSet:
		return AgentLinkReady
	case v.HubKey == connections.SecretUnset:
		return AgentLinkNoKey
	case v.HubKey == connections.SecretUnsupported:
		return AgentLinkNoKeyStore
	}
	return AgentLinkUnknown
}

// scopeOf builds the scope of a request from the ids the UI chose.
func scopeOf(in AgentAskInput) (values.Scope, bool) {
	subject, ok1 := refOf(values.KindSubject, in.SubjectID)
	lesson, ok2 := refOf(values.KindLesson, in.LessonID)
	assignment, ok3 := refOf(values.KindAssignment, in.AssignmentID)
	if !ok1 || !ok2 || !ok3 {
		return values.Scope{}, false
	}
	scope, err := values.NewScope(subject, values.Screen(in.Screen), lesson, assignment)
	return scope, err == nil
}

// refOf parses an id of kind; "" is none.
func refOf(kind values.IDKind, id string) (values.Ref, bool) {
	if id == "" {
		return values.Ref{}, true
	}
	r, err := values.NewRef(kind, id)
	return r, err == nil
}
