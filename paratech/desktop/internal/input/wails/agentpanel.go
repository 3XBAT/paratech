package wails

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opAgentCard = "ui: agent card dropped"
	opAgentFill = "ui: agent context fill"
)

// fillTimeout bounds the local read of a chat's fill at the end of a request: the update is published without it
// rather than late.
const fillTimeout = 2 * time.Second

// AgentFills reads the context ring of a chat (ADR-0028 «The ring»); satisfied by agentchats.Service.
type AgentFills interface {
	Fill(ctx context.Context, id shared.ThreadID) (agentchats.Fill, error)
}

// AgentUpdate is one update of a request as the Driver publishes it; app maps the Driver's own type to it. Problem is
// one of the Driver's problem enums; Progress marks the update of a step that went to the Hub (no cards).
type AgentUpdate struct {
	Thread     shared.ThreadID
	Turn       int
	Scope      values.Scope
	Status     thread.Status
	StopReason values.StopReason
	Cards      []values.Card
	Problem    string
	Progress   bool
}

// EventEmitter sends a Wails event to the pages; satisfied by Wails' event manager.
type EventEmitter interface {
	Emit(name string, data ...any) bool
}

// AgentPanel turns the Driver's updates into AgentUpdateEvent events and its drafts into AgentDraftEvent events
// (agentdraft.go). Until the shell attaches its emitter (and after it detaches) they are dropped: no page is there to
// show them.
type AgentPanel struct {
	log   *slog.Logger
	clock draftClock

	mu     sync.Mutex
	emit   EventEmitter
	fills  AgentFills                     // nil: updates carry no fill
	drafts map[shared.ThreadID]*draftTurn // the drafts of the latest request of each thread
}

// NewAgentPanel returns a panel with no emitter and no fills.
func NewAgentPanel(log *slog.Logger) *AgentPanel {
	return &AgentPanel{log: log, clock: wallClock{}, drafts: map[shared.ThreadID]*draftTurn{}}
}

// SetFills sets where the fill of a request's chat is read when the request ends; nil turns it off.
func (p *AgentPanel) SetFills(f AgentFills) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fills = f
}

// Attach sets the emitter; nil detaches it.
func (p *AgentPanel) Attach(e EventEmitter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emit = e
}

// Publish emits u. It holds the lock while emitting, so no update is emitted after Attach(nil) returns and updates
// published from one Driver goroutine keep their order. Updates of different operations may interleave: consumers
// must not rely on arrival order across operations. The drafts of the request are settled first (draftsBefore).
// An update that ends a request reads the chat's fill from the local database on the Driver's goroutine, bounded by
// fillTimeout, before the lock is taken.
func (p *AgentPanel) Publish(u AgentUpdate) {
	view := p.view(context.Background(), u)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.draftsBefore(u)
	if p.emit != nil {
		p.emit.Emit(AgentUpdateEvent, view)
	}
}

// AgentUpdateView is the JSON form of an update: ids, enums and the checked plain texts of the cards, never HTML.
// The texts (the model's) must be rendered as plain text. Problem is "" or busy, stale, invalid, budget,
// retry_required, restart_required or failed; the panel words the status and the stop reason itself (offline:
// «Не завершено: нет связи · Продолжить»). Progress is true for the update of a step that went to the Hub: it has no
// cards, and the panel applies its status only and keeps the cards it shows. Fill is the context ring of the chat,
// set only when the request ended (see endsRequest); null otherwise and when it could not be read.
type AgentUpdateView struct {
	ThreadID     string          `json:"threadId"` // "" when an Ask failed before its thread existed
	Turn         int             `json:"turn"`
	SubjectID    string          `json:"subjectId"` // "" «Без предмета»
	LessonID     string          `json:"lessonId"`
	AssignmentID string          `json:"assignmentId"`
	Screen       string          `json:"screen"`
	Status       string          `json:"status"`
	StopReason   string          `json:"stopReason"`
	Problem      string          `json:"problem"`
	Progress     bool            `json:"progress"`
	Cards        []AgentCardView `json:"cards"`
	Fill         *AgentFillView  `json:"fill"`
}

// AgentFillView is the context ring of a chat: the share of the window its next request would use in whole percent
// (0–100; 100 on an overflow), the level normal (< 70), high (70–89) or full (≥ 90: «Начать новый чат»), and overflow
// when an earlier request is already left out.
type AgentFillView struct {
	Percent  int    `json:"percent"`
	Level    string `json:"level"`
	Overflow bool   `json:"overflow"`
}

func fillView(f agentchats.Fill) *AgentFillView {
	return &AgentFillView{Percent: f.Percent, Level: string(f.Level), Overflow: f.Overflow}
}

// AgentCardView is one card; Kind says which field is set: plan, questions, progress (jobId), result (artifact,
// "<kind>/<path>" of the artifact store), confirm (target), action_line (actionId; assignment for create_assignment),
// answer (answerId), cannot_do
// (cannotDo), reply (text and assignments), stopped (stopReason) or work (workId and outcome: questions, review,
// helper, pending, failed or confirmed; the texts come from TurnkeyService.Work) or free_work (freeWork: the call the
// card names, answered with AgentService.FreeWork; the page words the card itself, the topic never comes here).
type AgentCardView struct {
	Kind        string                `json:"kind"`
	Plan        *AgentPlanView        `json:"plan"`
	Questions   *AgentQuestionsView   `json:"questions"`
	JobID       string                `json:"jobId"`
	Artifact    string                `json:"artifact"`
	Target      *AgentRefView         `json:"target"`
	ActionID    string                `json:"actionId"`
	AnswerID    string                `json:"answerId"`
	CannotDo    *AgentCannotDoView    `json:"cannotDo"`
	Text        string                `json:"text"`
	Assignments []AgentAssignmentView `json:"assignments"` // a reply's list; null on other cards
	// Assignment is the assignment an action_line of create_assignment added (ADR-0029); null on other cards and when
	// it is gone.
	Assignment *AgentAddedAssignmentView `json:"assignment"`
	StopReason string                    `json:"stopReason"`
	WorkID     string                    `json:"workId"`
	Outcome    string                    `json:"outcome"`
	FreeWork   *AgentFreeWorkRef         `json:"freeWork"`
}

// AgentFreeWorkRef names a free-work card «Такого задания нет — могу сделать как свободную работу» (TASK-188): the
// request and the call that waits on it.
type AgentFreeWorkRef struct {
	ThreadID string `json:"threadId"`
	Turn     int    `json:"turn"`
	CallID   string `json:"callId"`
}

// AgentAssignmentView is an assignment a reply lists, from the local data, never from the model (TASK-173): its title
// (plain text only), its library subject ("" none) and its deadline (YYYY-MM-DD, "" none).
type AgentAssignmentView struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	SubjectID string `json:"subjectId"`
	Due       string `json:"due"`
}

// AgentAddedAssignmentView is the assignment create_assignment added, from the local data, never from the model: its
// title (plain text only), its library subject ("" none), its deadline date (YYYY-MM-DD, "" none) and time of day
// (HH:MM, "" none; 23:59 when the student named no time).
type AgentAddedAssignmentView struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	SubjectID string `json:"subjectId"`
	Due       string `json:"due"`
	Time      string `json:"time"`
}

// AgentRefView is a typed id: kind is subject, lesson, proposal, preview, ...
type AgentRefView struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// AgentPlanView is «Понял так»: the caption, the steps the panel words from templates, and notes.
type AgentPlanView struct {
	Summary string              `json:"summary"`
	Steps   []AgentPlanStepView `json:"steps"`
	Notes   []string            `json:"notes"`
}

// AgentPlanStepView is a planned tool call; deferred names the id arguments an earlier step creates.
type AgentPlanStepView struct {
	Tool     string         `json:"tool"`
	Args     []AgentArgView `json:"args"`
	Deferred []string       `json:"deferred"`
}

// AgentArgView is one argument of a plan step. Type is id (idKind, value), ids (idKind, values), date (value,
// YYYY-MM-DD), time (value, HH:MM), enum (value), label (value: a short text, plain text only) or query (value: the
// student's question or search text, up to 500 characters, plain text only).
type AgentArgView struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	IDKind string   `json:"idKind"`
	Value  string   `json:"value"`
	Values []string `json:"values"`
}

// AgentQuestionsView is a round of questions; the answers name questions and options by index.
type AgentQuestionsView struct {
	Round     int                 `json:"round"`
	Questions []AgentQuestionView `json:"questions"`
}

// AgentQuestionView is one question: kind choice (options, default index; -1 preselects nothing) or text.
type AgentQuestionView struct {
	Kind    string   `json:"kind"`
	Text    string   `json:"text"`
	Options []string `json:"options"`
	Default int      `json:"default"`
	CiteRef string   `json:"citeRef"`
}

// AgentCannotDoView is «Целиком не получится»: the student's part and the app's part.
type AgentCannotDoView struct {
	YourPart []string `json:"yourPart"`
	OurPart  []string `json:"ourPart"`
}

// view maps u; a card that breaks its rules or that the panel does not know is dropped and logged. It reads the fill
// of an ending request from the local database, so it runs outside p.mu.
func (p *AgentPanel) view(ctx context.Context, u AgentUpdate) AgentUpdateView {
	problem := u.Problem
	if problem != "" && !agentUpdateProblems[problem] {
		problem = AgentProblemFailed
	}
	v := AgentUpdateView{
		ThreadID: string(u.Thread), Turn: u.Turn, SubjectID: u.Scope.Subject().ID(), LessonID: u.Scope.Lesson().ID(),
		AssignmentID: u.Scope.Assignment().ID(), Screen: string(u.Scope.Screen()), Status: string(u.Status),
		StopReason: string(u.StopReason), Problem: problem, Progress: u.Progress, Cards: make([]AgentCardView, 0, len(u.Cards)),
	}
	for _, c := range u.Cards {
		cv, ok := cardView(c)
		if !ok {
			p.log.Warn(opAgentCard, "thread", u.Thread.String())
			continue
		}
		v.Cards = append(v.Cards, cv)
	}
	v.Fill = p.fill(ctx, u)
	return v
}

// endsRequest tells an update after which the ring is redrawn (ADR-0028: when a request ends, never during a step):
// the request reached DONE, STOPPED, FAILED or CANCELLED. A progress update is a step on its way; PLANNING, EXECUTING
// and AWAITING_* are not over (a plan or questions waiting add nothing to the history before the answer), and an update
// of a failed operation has no status. STOPPED counts as an end; a «Продолжить» sends that request as the current one,
// so the ring may overstate by that request's own pair until the next end.
func endsRequest(u AgentUpdate) bool {
	if u.Progress || u.Thread == "" {
		return false
	}
	switch u.Status {
	case thread.StatusDone, thread.StatusStopped, thread.StatusFailed, thread.StatusCancelled:
		return true
	}
	return false
}

// fill is the ring of the chat of an ending request; nil for any other update, without fills, or when the read failed
// (logged: the update still goes out).
func (p *AgentPanel) fill(ctx context.Context, u AgentUpdate) *AgentFillView {
	if !endsRequest(u) {
		return nil
	}
	p.mu.Lock()
	fills := p.fills
	p.mu.Unlock()
	if fills == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fillTimeout)
	defer cancel()
	f, err := fills.Fill(ctx, u.Thread)
	if err != nil {
		p.log.Warn(opAgentFill, "thread", u.Thread.String(), "err", err)
		return nil
	}
	return fillView(f)
}

func cardView(c values.Card) (AgentCardView, bool) {
	if c == nil || c.Validate() != nil {
		return AgentCardView{}, false
	}
	v := AgentCardView{Kind: string(c.Kind())}
	switch c := c.(type) {
	case values.PlanCard:
		v.Plan = planView(c)
	case values.QuestionsCard:
		v.Questions = questionsView(c)
	case values.ProgressCard:
		v.JobID = string(c.Job)
	case values.ResultCard:
		v.Artifact = c.Artifact.String()
	case values.ConfirmCard:
		v.Target = &AgentRefView{Kind: string(c.Target.Kind()), ID: c.Target.ID()}
	case values.ActionLine:
		v.ActionID = string(c.Action)
		if l := c.Assignment; !l.Assignment.IsZero() {
			v.Assignment = &AgentAddedAssignmentView{
				ID: l.Assignment.ID(), Title: l.Title, SubjectID: l.Subject.ID(), Due: l.Due.String(), Time: l.DueTime.String(),
			}
		}
	case values.AnswerCard:
		v.AnswerID = c.Answer.ID()
	case values.CannotDoCard:
		v.CannotDo = &AgentCannotDoView{YourPart: texts(c.YourPart), OurPart: texts(c.OurPart)}
	case values.ReplyCard:
		v.Text = string(c.Text)
		v.Assignments = make([]AgentAssignmentView, 0, len(c.Lines))
		for _, l := range c.Lines {
			v.Assignments = append(v.Assignments, AgentAssignmentView{
				ID: l.Assignment.ID(), Title: l.Title, SubjectID: l.Subject.ID(), Due: l.Due.String(),
			})
		}
	case values.StoppedCard:
		v.StopReason = string(c.Reason)
	case values.WorkCard:
		v.WorkID, v.Outcome = c.Work.ID(), string(c.Outcome)
	case values.FreeWorkCard:
		v.FreeWork = &AgentFreeWorkRef{ThreadID: string(c.Thread), Turn: c.Turn, CallID: c.CallID}
	default:
		return AgentCardView{}, false
	}
	return v, true
}

func planView(c values.PlanCard) *AgentPlanView {
	steps := make([]AgentPlanStepView, 0, len(c.Steps))
	for _, s := range c.Steps {
		args := s.Args()
		step := AgentPlanStepView{Tool: string(s.Tool()), Args: []AgentArgView{}, Deferred: s.Deferred()}
		if step.Deferred == nil {
			step.Deferred = []string{}
		}
		for _, name := range args.Names() {
			step.Args = append(step.Args, argView(args, name))
		}
		steps = append(steps, step)
	}
	return &AgentPlanView{Summary: string(c.Summary), Steps: steps, Notes: texts(c.Notes)}
}

// argView maps one plan step argument; a plan step holds only ids, dates, enums, labels and queries.
func argView(args values.Args, name string) AgentArgView {
	v := AgentArgView{Name: name, Values: []string{}}
	if r, ok := args.ID(name); ok {
		v.Type, v.IDKind, v.Value = "id", string(r.Kind()), r.ID()
		return v
	}
	if refs := args.IDs(name); refs != nil {
		v.Type = "ids"
		for _, r := range refs {
			v.IDKind = string(r.Kind())
			v.Values = append(v.Values, r.ID())
		}
		return v
	}
	if d, ok := args.Date(name); ok {
		v.Type, v.Value = "date", d.String()
		return v
	}
	if tod, ok := args.Time(name); ok {
		v.Type, v.Value = "time", tod.String()
		return v
	}
	if e, ok := args.Enum(name); ok {
		v.Type, v.Value = "enum", e
		return v
	}
	if l, ok := args.Label(name); ok {
		v.Type, v.Value = "label", string(l)
	}
	if q, ok := args.Query(name); ok {
		v.Type, v.Value = "query", string(q)
	}
	return v
}

func questionsView(c values.QuestionsCard) *AgentQuestionsView {
	qs := make([]AgentQuestionView, 0, len(c.Questions))
	for _, q := range c.Questions {
		qs = append(qs, AgentQuestionView{
			Kind: string(q.Kind), Text: string(q.Text), Options: texts(q.Options), Default: q.Default, CiteRef: q.CiteRef,
		})
	}
	return &AgentQuestionsView{Round: c.Round, Questions: qs}
}

func texts(in []values.Text) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, string(t))
	}
	return out
}
