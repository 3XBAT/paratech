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
	opAgentRerun    = "ui: agent rerun"
	opAgentCancel   = "ui: agent cancel"
	opAgentLink     = "ui: agent hub link"
	opAgentWithdraw = "ui: agent withdraw"
)

// Problems of the agent calls (AgentResult.Problem). The outcome of an accepted call comes as an AgentUpdateEvent.
const (
	AgentProblemNone        = ""
	AgentProblemInvalid     = "invalid"     // malformed ids, turn or answers
	AgentProblemOffline     = "offline"     // Ask or Rerun while the Hub is not set up (AgentState says why); the panel is read-only
	AgentProblemClosed      = "closed"      // the app is closing
	AgentProblemUnavailable = "unavailable" // Undo or ChatCurrent is not wired
	AgentProblemBusy        = "busy"        // UndoRequest met a concurrent change; nothing more was undone, try again
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
	"rerun_blocked": true, "failed": true,
}

// AgentDriver is the agent loop (input/agent.Driver) as the panel uses it (ARCH-42); app wires it in, as input/wails
// must not import input/agent. Each call starts the operation and returns; its updates go to the AgentUpdate sink. Ask
// returns the ticket of a question that waits for the chat's current request (empty: it started); Withdraw removes a
// waiting one at once.
type AgentDriver interface {
	Ask(ctx context.Context, in agentturn.AskInput) (agentturn.Queued, error)
	Withdraw(thread shared.ThreadID, ticket agentturn.Ticket) agentturn.WithdrawResult
	ApprovePlan(ctx context.Context, ref agentturn.TurnRef) error
	RevisePlan(ctx context.Context, ref agentturn.TurnRef, wish string) error
	AnswerQuestions(ctx context.Context, ref agentturn.TurnRef, answers []values.Answer) error
	Stop(ctx context.Context, ref agentturn.TurnRef) error
	Continue(ctx context.Context, ref agentturn.TurnRef) error
	Retry(ctx context.Context, ref agentturn.TurnRef) error
	Restart(ctx context.Context, ref agentturn.TurnRef) error
	Rerun(ctx context.Context, ref agentturn.TurnRef) error
	Cancel(ctx context.Context, ref agentturn.TurnRef) error
}

// AgentLink tells whether the Hub is set up; satisfied by connections.Service.
type AgentLink interface {
	Get(ctx context.Context) (connections.View, error)
}

// AgentCore is what the agent panel and «Журнал» need. Life must live as long as the app: the Driver's operations
// outlive the Wails call that starts them (TASK-117 handoff).
type AgentCore struct {
	Driver  AgentDriver
	Link    AgentLink
	Panel   *AgentPanel
	Life    context.Context
	Journal AgentJournal
	Clock   AgentClock
	Chats   AgentChats // the chat list (ADR-0028)
	// Mentions are what the «@» search reads (TASK-246); incomplete: MentionCandidates answers unavailable.
	Mentions AgentMentionSources
	// Current reads the current request of a chat (TASK-272); nil: ChatCurrent answers unavailable.
	Current AgentCurrent
}

func (c AgentCore) complete() bool {
	return c.Driver != nil && c.Link != nil && c.Panel != nil && c.Life != nil && c.Journal != nil && c.Clock != nil &&
		c.Chats != nil
}

// AgentAskInput is a request typed in the panel with the context the UI chose. Empty ids mean none; an empty
// subjectId is a request in no subject. Screen is "", today, subjects, assignments, schedule, journal or settings.
// ThreadID is the chat ("" the current chat, a new one when there is none); Mentions are the «@» objects, at most
// values.MaxMentions (the ones that no longer exist are dropped). RestartFrom is «Начать заново отсюда» → «Скрыть»:
// the number of the earlier request of chat ThreadID from which the agent forgets (0: an ordinary request); it needs
// threadId.
type AgentAskInput struct {
	Text         string         `json:"text"`
	SubjectID    string         `json:"subjectId"`
	LessonID     string         `json:"lessonId"`
	AssignmentID string         `json:"assignmentId"`
	Screen       string         `json:"screen"`
	ThreadID     string         `json:"threadId"`
	Mentions     []AgentMention `json:"mentions"`
	RestartFrom  int            `json:"restartFrom"`
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

// AgentAskResult is the answer of Ask: Problem as in AgentResult; Queued when the question waits for the chat's current
// request («ждёт предыдущий запрос»), with Ticket for Withdraw and to match the update of its start ("" otherwise).
type AgentAskResult struct {
	Problem string `json:"problem"`
	Queued  bool   `json:"queued"`
	Ticket  string `json:"ticket"`
}

// Results of Withdraw: the agentturn ones, plus invalid (a malformed chat id or ticket) and failed.
const (
	AgentWithdrawn       = "withdrawn" // it waited and is gone: never sent, never paid
	AgentWithdrawStarted = "started"   // it already began as a request
	AgentWithdrawMissing = "not_found"
	AgentWithdrawInvalid = "invalid"
	AgentWithdrawFailed  = "failed"
)

// AgentWithdrawResult is the answer of Withdraw.
type AgentWithdrawResult struct {
	Result string `json:"result"`
}

// AgentState is what the panel shows before any request: ReadOnly when the Hub is not set up («Нет связи с
// paratech»); Link says why.
type AgentState struct {
	Link     string `json:"link"`
	ReadOnly bool   `json:"readOnly"`
}

// AgentService is bound to the agent panel ("<package path>.AgentService.<Method>").
type AgentService struct {
	driver   AgentDriver
	link     AgentLink
	journal  AgentJournal
	clock    AgentClock
	chats    AgentChats
	mentions AgentMentionSources
	current  AgentCurrent
	panel    *AgentPanel
	life     context.Context
	log      *slog.Logger
}

// NewAgentService returns the binding of the agent panel; OpenServices checks the core first. The shell attaches
// c.Panel to its events itself.
func NewAgentService(c AgentCore, log *slog.Logger) *AgentService {
	return &AgentService{
		driver: c.Driver, link: c.Link, journal: c.Journal, clock: c.Clock, chats: c.Chats,
		mentions: c.Mentions, current: c.Current, panel: c.Panel, life: c.Life, log: log,
	}
}

// State returns the Hub link of the panel; it calls no Hub.
func (s *AgentService) State(ctx context.Context) AgentState {
	link := s.linkState(ctx)
	return AgentState{Link: link, ReadOnly: link != AgentLinkReady}
}

// Ask starts a request in the chat of in; refused with "offline" while the Hub is not set up. A chat that does not
// exist comes back as an update with problem "failed", like any outcome of the request: the update problems
// (agentUpdateProblems) have no not_found, unlike the chat calls. A question asked while the chat's request runs waits
// (queued, with its ticket); it starts when that request ends, and the updates of its start carry the ticket.
func (s *AgentService) Ask(ctx context.Context, in AgentAskInput) AgentAskResult {
	scope, ok := scopeOf(in)
	if !ok {
		return AgentAskResult{Problem: AgentProblemInvalid}
	}
	chat := shared.ThreadID(in.ThreadID)
	if chat != "" && chat.Validate() != nil {
		return AgentAskResult{Problem: AgentProblemInvalid}
	}
	if in.RestartFrom < 0 || (in.RestartFrom > 0 && chat == "") {
		return AgentAskResult{Problem: AgentProblemInvalid}
	}
	mentions, ok := mentionsOf(in.Mentions)
	if !ok {
		return AgentAskResult{Problem: AgentProblemInvalid}
	}
	if s.linkState(ctx) != AgentLinkReady {
		return AgentAskResult{Problem: AgentProblemOffline}
	}
	q, err := s.driver.Ask(s.life, agentturn.AskInput{
		ThreadID: chat, Scope: scope, Text: in.Text, Mentions: mentions, RestartFrom: in.RestartFrom,
	})
	if r := s.started(opAgentAsk, err); r.Problem != "" {
		return AgentAskResult{Problem: r.Problem}
	}
	return AgentAskResult{Queued: q.Ticket != "", Ticket: string(q.Ticket)}
}

// Withdraw is «Отменить этот запрос» of waiting question ticket of chat threadID: withdrawn (never sent, never paid),
// started (it already began: «Стоп» stops it), not_found, or invalid for a malformed id or ticket.
func (s *AgentService) Withdraw(_ context.Context, threadID, ticket string) AgentWithdrawResult {
	chat, t := shared.ThreadID(threadID), agentturn.Ticket(ticket)
	if chat.Validate() != nil || t.Validate() != nil {
		return AgentWithdrawResult{Result: AgentWithdrawInvalid}
	}
	switch r := s.driver.Withdraw(chat, t); r {
	case agentturn.WithdrawWithdrawn, agentturn.WithdrawStarted, agentturn.WithdrawNotFound:
		return AgentWithdrawResult{Result: string(r)}
	default:
		s.log.Warn(opAgentWithdraw, "result", string(r))
		return AgentWithdrawResult{Result: AgentWithdrawFailed}
	}
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

// Continue is «Повторить» of a stopped request after a stop, a lost connection or the quota (the same key).
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

// Rerun is «Повторить» of the last finished or stopped request: a new request with the same text and scope. Like Ask
// it is refused with "offline" while the Hub is not set up, so no new request starts only to stop.
func (s *AgentService) Rerun(ctx context.Context, ref AgentTurnRef) AgentResult {
	r := agentturn.TurnRef{Thread: shared.ThreadID(ref.ThreadID), Turn: ref.Turn}
	if r.Thread.Validate() != nil || r.Turn < 1 {
		return AgentResult{Problem: AgentProblemInvalid}
	}
	if s.linkState(ctx) != AgentLinkReady {
		return AgentResult{Problem: AgentProblemOffline}
	}
	return s.started(opAgentRerun, s.driver.Rerun(s.life, r))
}

// Cancel is «Отмена».
func (s *AgentService) Cancel(_ context.Context, ref AgentTurnRef) AgentResult {
	return s.onTurn(opAgentCancel, ref, s.driver.Cancel)
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
