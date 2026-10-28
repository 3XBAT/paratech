package thread

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// MaxSeenRefs bounds the IDs a thread remembers from tool results.
const MaxSeenRefs = 4096

// Thread is an agent chat (ADR-0028): its title, archive mark and requests of any subjects; the subject belongs to
// the scope of each request. Fields change only through its methods (ARCH-14).
type Thread struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.ThreadID
	turns   int   // number of the last request; 0 = none yet
	current *Turn // the last request; nil only when turns == 0
	seen    values.RefSet
	// historyFrom is the first request whose history is sent to the planner; a restarted request sends no older
	// echo, whose model turn the Hub no longer keeps (ADR-0023). 0 means all.
	historyFrom int
	summary     values.HistorySummary // of the earlier requests 1…Through; zero: none (Compact)
	summaryAt   time.Time             // when the summary was made; zero exactly when there is none
	title       string
	archivedAt  time.Time // zero: not archived
	lastActive  time.Time // start of the last request; creation before the first
	createdAt   time.Time
	updatedAt   time.Time
}

// New creates an empty chat titled «Новый чат».
func New(id shared.ThreadID, now time.Time) (*Thread, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidThread, err)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidThread)
	}
	return &Thread{
		id: id, seen: values.RefSet{}, title: DefaultTitle, lastActive: now, createdAt: now, updatedAt: now,
	}, nil
}

// TurnState is the stored form of the current request.
type TurnState struct {
	Number       int
	Scope        values.Scope
	Mentions     []values.Mention
	DroppedBy    int // always 0 for the current request: only a later request drops one
	Status       Status
	Resume       Status
	StopReason   values.StopReason
	Budget       Budget
	StepOpen     bool
	ProposedPlan []values.PlanStep
	ApprovedPlan []values.PlanStep
	Used         []bool
	Retry        int
	Open         []OpenCall
	Created      []Created
	ReplyLine    string // set only while the request is final (FreezeReply)
	StartedAt    time.Time
	UpdatedAt    time.Time
}

// State is the stored form of a thread, for Restore.
type State struct {
	Version     int64
	ID          shared.ThreadID
	Turns       int
	Current     *TurnState // nil when Turns == 0
	Seen        []values.Ref
	HistoryFrom int
	Summary     values.HistorySummary // zero: none
	SummaryAt   time.Time             // zero exactly when Summary is
	Title       string
	ArchivedAt  time.Time // zero: not archived
	LastActive  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Restore rebuilds a stored thread without side effects and checks its invariants.
func Restore(s State) (*Thread, error) {
	bad := func(what string) (*Thread, error) { return nil, fmt.Errorf("%w: %s", ErrInvalidThread, what) }
	if s.Version < 1 {
		return bad("stored version must be at least 1")
	}
	th, err := New(s.ID, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return bad("updated before created")
	}
	if err := checkTitle(s.Title); err != nil {
		return bad(err.Error())
	}
	if s.LastActive.IsZero() || s.LastActive.Before(s.CreatedAt) {
		return bad("no last activity, or before created")
	}
	if !s.ArchivedAt.IsZero() && s.ArchivedAt.Before(s.CreatedAt) {
		return bad("archived before created")
	}
	if s.Turns < 0 || (s.Turns == 0) != (s.Current == nil) {
		return bad("a thread has a current request exactly when it has requests")
	}
	if len(s.Seen) > MaxSeenRefs {
		return bad("too many seen ids")
	}
	if s.HistoryFrom < 0 || s.HistoryFrom > s.Turns {
		return bad("history start out of range")
	}
	for _, r := range s.Seen {
		if r.IsZero() {
			return bad("zero seen id")
		}
	}
	if s.Current != nil {
		t, err := restoreTurn(*s.Current, s.Turns)
		if err != nil {
			return nil, err
		}
		if !s.ArchivedAt.IsZero() && !t.final() {
			return bad("an archived chat with a request that is not final")
		}
		th.current = &t
	}
	if err := checkSummary(s); err != nil {
		return bad(err.Error())
	}
	th.summary, th.summaryAt = s.Summary, s.SummaryAt
	th.version, th.turns, th.historyFrom, th.updatedAt = s.Version, s.Turns, s.HistoryFrom, s.UpdatedAt
	th.title, th.archivedAt, th.lastActive = s.Title, s.ArchivedAt, s.LastActive
	th.seen.Add(s.Seen...)
	return th, nil
}

func restoreTurn(s TurnState, number int) (Turn, error) {
	bad := func(what string) (Turn, error) { return Turn{}, fmt.Errorf("%w: request: %s", ErrInvalidThread, what) }
	switch {
	case s.Number != number:
		return bad("number is not the last request")
	case !s.Status.Valid():
		return bad("unknown status")
	case !s.Budget.valid():
		return bad("budget out of range")
	case s.StartedAt.IsZero() || s.UpdatedAt.Before(s.StartedAt):
		return bad("inconsistent timestamps")
	case (s.Status == StatusStopped) != (s.Resume != ""):
		return bad("a resume status exists exactly for a stopped request")
	case s.Status == StatusStopped && (!s.Resume.stepping() || !s.StopReason.Valid()):
		return bad("bad resume status or stop reason")
	case s.Status != StatusStopped && s.StopReason != "":
		return bad("a stop reason without STOPPED")
	case s.StepOpen && !s.Status.stepping() && s.Status != StatusStopped:
		return bad("an open step outside PLANNING, EXECUTING or STOPPED")
	case (s.Status == StatusAwaitingPlan) != (len(s.ProposedPlan) > 0):
		return bad("a proposed plan exists exactly while awaiting the plan")
	case len(s.Used) != len(s.ApprovedPlan):
		return bad("used flags do not match the approved plan")
	case s.Retry < 0 || s.Retry > MaxRetries:
		return bad("retry count out of range")
	case s.StepOpen && len(s.Open) > 0:
		return bad("an open step while results are pending")
	case !validOpen(s.Open, len(s.ApprovedPlan)):
		return bad("bad open calls")
	case !validCreated(s.Created, len(s.ApprovedPlan)):
		return bad("bad created ids")
	case s.DroppedBy != 0:
		return bad("the current request is dropped")
	case s.ReplyLine != "" && !s.Status.final():
		return bad("a reply line before the request is final")
	case s.ReplyLine != "" && (values.HistoryTurn{AgentReply: s.ReplyLine}).Validate() != nil:
		return bad("bad reply line")
	}
	if err := values.CheckMentions(s.Mentions); err != nil {
		return bad(err.Error())
	}
	return Turn{
		number: s.Number, scope: s.Scope, mentions: slices.Clone(s.Mentions), status: s.Status, resume: s.Resume,
		stopReason: s.StopReason, budget: s.Budget, stepOpen: s.StepOpen, proposed: slices.Clone(s.ProposedPlan), approved: slices.Clone(s.ApprovedPlan),
		used: slices.Clone(s.Used), retry: s.Retry, open: slices.Clone(s.Open), created: slices.Clone(s.Created),
		replyLine: s.ReplyLine, startedAt: s.StartedAt, updatedAt: s.UpdatedAt,
	}, nil
}

// State returns the stored form of the thread.
func (th *Thread) State() State {
	s := State{
		Version: th.version, ID: th.id, Turns: th.turns, Seen: th.seen.Sorted(),
		HistoryFrom: th.historyFrom, Summary: th.summary, SummaryAt: th.summaryAt, Title: th.title,
		ArchivedAt: th.archivedAt, LastActive: th.lastActive, CreatedAt: th.createdAt, UpdatedAt: th.updatedAt,
	}
	if th.current != nil {
		t := th.current
		s.Current = &TurnState{
			Number: t.number, Scope: t.scope, Mentions: slices.Clone(t.mentions), Status: t.status, Resume: t.resume,
			StopReason: t.stopReason, Budget: t.budget, StepOpen: t.stepOpen, ProposedPlan: slices.Clone(t.proposed), ApprovedPlan: slices.Clone(t.approved),
			Used: slices.Clone(t.used), Retry: t.retry, Open: slices.Clone(t.open), Created: slices.Clone(t.created),
			ReplyLine: t.replyLine, StartedAt: t.startedAt, UpdatedAt: t.updatedAt,
		}
	}
	return s
}

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004).
func (th *Thread) Persisted(version int64) { th.version = version }

// HistoryFrom returns the first request whose history is sent to the planner (0: all).
func (th *Thread) HistoryFrom() int { return th.historyFrom }

// ID returns the thread ID.
func (th *Thread) ID() shared.ThreadID { return th.id }

// Version returns the persisted version.
func (th *Thread) Version() int64 { return th.version }

// Turn returns a read-only copy of the current request.
func (th *Thread) Turn() (Turn, bool) {
	if th.current == nil {
		return Turn{}, false
	}
	return th.current.clone(), true
}

// Seen reports whether a ref appeared in tool results of this thread.
func (th *Thread) Seen(r values.Ref) bool { return th.seen.Has(r) }

// --- requests ----------------------------------------------------------------------------------------

// StartTurn starts a new request. A previous request that waits for the student (a plan, answers, «Продолжить») is
// cancelled; one that is still running is ErrTurnInProgress.
func (th *Thread) StartTurn(now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if t := th.current; t != nil && !t.status.IsTerminal() && t.status.stepping() {
		return ErrTurnInProgress
	}
	// The results of the last calls belong to the history before the new text (Abandon, TakeResults).
	if t := th.current; t != nil && len(t.open) > 0 {
		return ErrResultsPending
	}
	if t := th.current; t != nil && !t.status.IsTerminal() {
		if err := th.Cancel(now); err != nil {
			return err
		}
	}
	th.turns++
	th.current = &Turn{number: th.turns, status: StatusPlanning, startedAt: now, updatedAt: now}
	// «Старый чат можно продолжить»: a new request brings an archived chat back (ADR-0028).
	th.archivedAt, th.lastActive, th.updatedAt = time.Time{}, now, now
	return nil
}

// Request is what the student sends: the scope of the context chip (any subject: a chat spans subjects), the
// mentioned objects and the text. The text only names a chat still titled «Новый чат»; the case stores it in the
// history.
type Request struct {
	Scope    values.Scope
	Mentions []values.Mention
	Text     string
}

// Ask starts a request (StartTurn) with its scope and mentions. Mentions of the same object count once; more than
// values.MaxMentions distinct ones are refused and nothing changes.
func (th *Thread) Ask(r Request, now time.Time) error {
	var mentions []values.Mention
	for _, m := range r.Mentions {
		if !slices.ContainsFunc(mentions, func(x values.Mention) bool { return x.Ref() == m.Ref() }) {
			mentions = append(mentions, m)
		}
	}
	if err := values.CheckMentions(mentions); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidThread, err)
	}
	if err := th.StartTurn(now); err != nil {
		return err
	}
	th.current.scope, th.current.mentions = r.Scope, mentions
	if th.title == DefaultTitle {
		if title := autoTitle(r.Text); title != "" {
			th.title = title
		}
	}
	return nil
}

// StartRequest is Ask in a scope, without mentions or text.
func (th *Thread) StartRequest(scope values.Scope, now time.Time) error {
	return th.Ask(Request{Scope: scope}, now)
}

func (th *Thread) turn() (*Turn, error) {
	if th.current == nil {
		return nil, ErrNoTurn
	}
	return th.current, nil
}

// move applies a status change of the current request through the table.
func (th *Thread) move(to Status, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if !CanTransition(t.status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.status, to)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if t.status == StatusStopped {
		t.resume, t.stopReason = "", ""
	}
	// A stopped request that goes on is no longer final: its line is frozen again when it ends, and a summary that
	// covers it is rebuilt by the next compaction.
	if !to.final() {
		t.replyLine = ""
		th.clearSummaryFrom(t.number)
	}
	t.status = to
	t.updatedAt = now
	th.updatedAt = now
	return nil
}

// ready returns the current request if the planner is being called and no step is open.
func (th *Thread) ready() (*Turn, error) {
	t, err := th.turn()
	if err != nil {
		return nil, err
	}
	if !t.status.stepping() || t.stepOpen {
		return nil, fmt.Errorf("%w: not between steps (status %s, open step %v)", ErrInvalidTransition, t.status, t.stepOpen)
	}
	return t, nil
}

// stopBudget stops the request for good and returns ErrBudgetExceeded.
func (th *Thread) stopBudget(what string, now time.Time) error {
	if err := th.Stop(values.StopBudget, now); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrBudgetExceeded, what)
}

// --- steps -------------------------------------------------------------------------------------------

// BeginStep opens the next planner step and returns its key ag:<thread>:<turn>:<step>. Over the step or input
// token limit (stepLimits) the request stops (budget) and ErrBudgetExceeded is returned.
func (th *Thread) BeginStep(now time.Time) (string, error) {
	t, err := th.ready()
	if err != nil {
		return "", err
	}
	if len(t.open) > 0 {
		return "", ErrResultsPending
	}
	maxSteps, maxTokens := t.stepLimits()
	if t.budget.Steps >= maxSteps {
		return "", th.stopBudget("steps", now)
	}
	if t.budget.InputTokens >= maxTokens {
		return "", th.stopBudget("input tokens", now)
	}
	t.budget.Steps++
	t.stepOpen = true
	t.retry = 0
	t.updatedAt, th.updatedAt = now, now
	return th.stepKey(t), nil
}

// StepKey returns the key of the open step, so «Продолжить» repeats the same Hub call.
func (th *Thread) StepKey() (string, error) {
	t, err := th.turn()
	if err != nil {
		return "", err
	}
	if !t.stepOpen {
		return "", fmt.Errorf("%w: no open step", ErrInvalidTransition)
	}
	return th.stepKey(t), nil
}

// stepKey is ag:<thread>:<turn>:<step>, plus :r<N> after the student's N-th «Повторить» of a failed step: the Hub
// replays a failure of a key for a day, so a retry needs a new key, while «Продолжить» after a lost answer must
// repeat the same one (free replay).
func (th *Thread) stepKey(t *Turn) string {
	if t.retry > 0 {
		return fmt.Sprintf("ag:%s:%d:%d:r%d", th.id, t.number, t.budget.Steps, t.retry)
	}
	return fmt.Sprintf("ag:%s:%d:%d", th.id, t.number, t.budget.Steps)
}

// EndStep closes the open step with the number of calls the Hub returned and the planner input tokens it used. More
// than MaxCallsPerStep calls stop the request (budget).
func (th *Thread) EndStep(calls, inputTokens int, now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if !t.stepOpen || !t.status.stepping() {
		return fmt.Errorf("%w: no open step", ErrInvalidTransition)
	}
	if calls < 1 || inputTokens < 0 || now.IsZero() {
		return fmt.Errorf("%w: a step has at least one call and non-negative usage", ErrInvalidThread)
	}
	t.stepOpen = false
	t.budget.InputTokens += inputTokens
	t.updatedAt, th.updatedAt = now, now
	if calls > MaxCallsPerStep {
		return th.stopBudget("calls per step", now)
	}
	return nil
}

// Observe remembers the IDs of tool results of this thread for the provenance check.
func (th *Thread) Observe(refs ...values.Ref) error {
	for _, r := range refs {
		if r.IsZero() {
			return fmt.Errorf("%w: zero id in results", ErrInvalidThread)
		}
	}
	add := values.NewRefSet(refs...)
	n := len(th.seen)
	for r := range add {
		if !th.seen.Has(r) {
			n++
		}
	}
	if n > MaxSeenRefs {
		return fmt.Errorf("%w: too many ids in the thread", ErrInvalidThread)
	}
	th.seen.Add(refs...)
	return nil
}

// --- calls -------------------------------------------------------------------------------------------

// Facts are what the case knows about a call before it runs.
type Facts struct {
	Context       values.RefSet // IDs of the base and the scope of this request
	CostMicroUSD  int64         // estimate for a paid call; zero means unknown
	ReplacesReady bool          // the call would replace a ready result
	Triage        actionpolicy.Triage
	// FreeWorkNotice: the student wants the free-work card before a work on a free topic (the setting, on by default;
	// off after «Больше не показывать», TASK-188).
	FreeWorkNotice bool
}

// Admission is the decision on one call.
type Admission struct {
	Class    actionpolicy.Class
	Paid     bool
	PlanStep int // index of the approved step it executes; -1 when there is no approved plan
}

// Admit decides whether a call may run (ADR-0021, ADR-0022): every ID must have typed provenance; a U call must
// match an unused step when a plan is approved (not counted: the «Да» covers the whole plan, ADR-0033 §4) and
// otherwise must not need a plan (replacing a ready result needs one too) and counts as a write; a paid U call
// without a plan counts toward MaxPaidUnplanned. C calls only create a proposal (Admission.Class tells), UI calls are
// handled by ProposePlan, AskQuestions and Complete. A refused call is never executed; over the write limit the
// request stops (budget).
//
// A work on a free topic (actionpolicy.FreeTopic, TASK-188) outside an approved plan is admitted as class UI while
// the student wants the free-work card: nothing runs and nothing is counted; the card's «Сделать» is the student's one
// confirmation, no plan card comes on top. Without the card it is the paid job without a plan (counted); a second
// write of the request still needs a plan. As a step of an approved plan it runs as any step.
func (th *Thread) Admit(call values.Call, f Facts, now time.Time) (Admission, error) {
	t, err := th.ready()
	if err != nil {
		return Admission{}, err
	}
	rule, err := actionpolicy.Of(call.Tool())
	if err != nil {
		return Admission{}, err
	}
	class := rule.Class
	if err := actionpolicy.CheckProvenance(call.Refs(), th.known(f.Context)); err != nil {
		return Admission{}, err
	}
	free := actionpolicy.FreeTopic(call)
	if free && len(t.approved) == 0 && f.FreeWorkNotice {
		adm := Admission{Class: actionpolicy.ClassUI, PlanStep: -1}
		t.admitted(call, adm)
		return adm, nil
	}
	adm := Admission{Class: class, Paid: rule.Paid, PlanStep: -1}
	if !class.IsWrite() {
		// Paid R calls (answer_question, inspect_upload) are reads: they do not count as the paid job without a plan.
		t.admitted(call, adm)
		return adm, nil
	}

	if len(t.approved) > 0 {
		for i, s := range t.approved {
			if !t.used[i] && s.Matches(call) && t.deferredBound(s, i, call) {
				adm.PlanStep = i
				break
			}
		}
		if adm.PlanStep < 0 {
			return Admission{}, ErrNotInPlan
		}
		t.used[adm.PlanStep] = true
	} else {
		if t.budget.Writes >= MaxWrites {
			return Admission{}, th.stopBudget("writes", now)
		}
		reason, err := actionpolicy.NeedsPlan(call.Tool(), actionpolicy.Facts{
			WritesBefore: t.budget.Writes, CostMicroUSD: f.CostMicroUSD, Triage: f.Triage, ReplacesReady: f.ReplacesReady,
			FreeTopic: free,
		})
		if err != nil {
			return Admission{}, err
		}
		if reason != actionpolicy.ReasonNone {
			return Admission{}, fmt.Errorf("%w: %s", ErrPlanRequired, reason)
		}
		if rule.Paid {
			if t.budget.PaidUnplanned >= MaxPaidUnplanned {
				return Admission{}, fmt.Errorf("%w: paid job limit without a plan", ErrPlanRequired)
			}
			t.budget.PaidUnplanned++
		}
		t.budget.Writes++
	}
	if t.status == StatusPlanning {
		if err := th.move(StatusExecuting, now); err != nil {
			return Admission{}, err
		}
	}
	t.admitted(call, adm)
	t.updatedAt, th.updatedAt = now, now
	return adm, nil
}

// admitted marks the open call (if the step's calls were recorded with Expect) as allowed to run.
func (t *Turn) admitted(call values.Call, adm Admission) {
	if oc := t.openCall(call.ID()); oc != nil && !oc.Answered {
		oc.Tool, oc.PlanStep, oc.Admitted = call.Tool(), adm.PlanStep, true
	}
}

// known is what the provenance check accepts: the IDs of tool results of this chat, the context of the step and the
// mentions of the current request, which count like the scope (ADR-0028).
func (th *Thread) known(context values.RefSet) values.RefSet {
	k := th.seen.Clone()
	for r := range context {
		k.Add(r)
	}
	if th.current != nil {
		k.Add(values.MentionRefs(th.current.mentions)...)
	}
	return k
}

// CheckRefs is the provenance check of the IDs a card shows (reply, cannot_do): each must appear with its kind in the
// context of the step, among the mentions of the request or in a tool result of this thread, as for Admit (ADR-0022).
func (th *Thread) CheckRefs(refs []values.Ref, context values.RefSet) error {
	return actionpolicy.CheckProvenance(refs, th.known(context))
}

// --- plan --------------------------------------------------------------------------------------------

// ProposePlan records the steps of show_plan and waits for «Да». Steps are 1..values.MaxPlanSteps U or C tools,
// and their IDs pass the provenance check. The number of writes is not limited: the one «Да» covers them all.
func (th *Thread) ProposePlan(steps []values.PlanStep, context values.RefSet, now time.Time) error {
	if _, err := th.ready(); err != nil {
		return err
	}
	if len(steps) == 0 || len(steps) > values.MaxPlanSteps {
		return fmt.Errorf("%w: a plan has 1..%d steps", ErrInvalidPlan, values.MaxPlanSteps)
	}
	known := th.known(context)
	for _, s := range steps {
		r, err := actionpolicy.Of(s.Tool())
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidPlan, err)
		}
		if r.Class != actionpolicy.ClassU && r.Class != actionpolicy.ClassC {
			return fmt.Errorf("%w: step %s is not an action", ErrInvalidPlan, s.Tool())
		}
		if err := actionpolicy.CheckProvenance(s.Args().Refs(), known); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidPlan, err)
		}
	}
	if err := th.move(StatusAwaitingPlan, now); err != nil {
		return err
	}
	th.current.proposed = slices.Clone(steps)
	return nil
}

// ApprovePlan is «Да»: the proposed steps become the approved plan; each runs at most once.
func (th *Thread) ApprovePlan(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusAwaitingPlan {
		return fmt.Errorf("%w: no plan to approve", ErrInvalidTransition)
	}
	if err := th.move(StatusExecuting, now); err != nil {
		return err
	}
	t.approved, t.proposed = t.proposed, nil
	t.used = make([]bool, len(t.approved))
	t.created = nil // step indexes refer to the plan they were created under
	return nil
}

// RevisePlan is «Изменить»: the proposed plan and any earlier approved plan are dropped and the planner continues,
// so later writes need a new «Да». The request is back to the limits without a plan (stepLimits).
func (th *Thread) RevisePlan(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusAwaitingPlan {
		return fmt.Errorf("%w: no plan to revise", ErrInvalidTransition)
	}
	if err := th.move(StatusPlanning, now); err != nil {
		return err
	}
	// Plan steps are not counted as writes (only U calls mark a step used); one that ran still makes a later
	// unplanned write the second one, which needs a plan.
	if t.budget.Writes == 0 && slices.Contains(t.used, true) {
		t.budget.Writes = 1
	}
	t.proposed, t.approved, t.used, t.created = nil, nil, nil, nil
	return nil
}

// --- questions ---------------------------------------------------------------------------------------

// AskQuestions opens the next question round (1 or 2) and returns its number. A third round is ErrTooManyRounds and
// changes nothing.
func (th *Thread) AskQuestions(now time.Time) (int, error) {
	t, err := th.ready()
	if err != nil {
		return 0, err
	}
	if t.budget.QuestionRounds >= maxRounds {
		return 0, ErrTooManyRounds
	}
	if err := th.move(StatusAwaitingAnswers, now); err != nil {
		return 0, err
	}
	t.budget.QuestionRounds++
	return t.budget.QuestionRounds, nil
}

// AnswerQuestions records that the student answered; the planner continues.
func (th *Thread) AnswerQuestions(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusAwaitingAnswers {
		return fmt.Errorf("%w: no open questions", ErrInvalidTransition)
	}
	return th.move(StatusPlanning, now)
}

// --- end of a request --------------------------------------------------------------------------------

// Complete ends the request (reply, cannot_do or nothing left to do).
func (th *Thread) Complete(now time.Time) error {
	if _, err := th.ready(); err != nil {
		return err
	}
	return th.move(StatusDone, now)
}

// Fail ends the request after a permanent error of a step.
func (th *Thread) Fail(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if err := th.move(StatusFailed, now); err != nil {
		return err
	}
	t.stepOpen = false
	return nil
}

// Stop stops a running request. An open step stays open, so «Продолжить» repeats it with the same key.
func (th *Thread) Stop(reason values.StopReason, now time.Time) error {
	if !reason.Valid() {
		return fmt.Errorf("%w: unknown stop reason", ErrInvalidThread)
	}
	t, err := th.turn()
	if err != nil {
		return err
	}
	from := t.status
	if err := th.move(StatusStopped, now); err != nil {
		return err
	}
	t.resume, t.stopReason = from, reason
	return nil
}

// Continue is «Продолжить»: the request goes back where it stopped. A request stopped by its budget cannot continue.
func (th *Thread) Continue(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusStopped {
		return fmt.Errorf("%w: request is not stopped", ErrInvalidTransition)
	}
	switch t.stopReason {
	case values.StopBudget:
		return ErrBudgetExceeded
	case values.StopFailed:
		return ErrRetryRequired
	case values.StopExpired:
		return ErrRestartRequired
	}
	return th.move(t.resume, now)
}

// Retry is «Повторить» after a step failed for good: the open step is sent again under a new key (StepKey). After
// MaxRetries retries the request stops for good (budget).
func (th *Thread) Retry(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusStopped || t.stopReason != values.StopFailed || !t.stepOpen {
		return fmt.Errorf("%w: no failed step to retry", ErrInvalidTransition)
	}
	if t.retry >= MaxRetries {
		t.stopReason = values.StopBudget
		t.updatedAt, th.updatedAt = now, now
		return fmt.Errorf("%w: retries", ErrBudgetExceeded)
	}
	if err := th.move(t.resume, now); err != nil {
		return err
	}
	t.retry++
	return nil
}

// Restart starts the request again after its model turn expired on the Hub: the stopped request is cancelled, a new
// one starts with the same scope and mentions, and only its own history is sent from now on (HistoryFrom). The case
// adds the student's text again.
func (th *Thread) Restart(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if t.status != StatusStopped || t.stopReason != values.StopExpired {
		return fmt.Errorf("%w: the request did not expire", ErrInvalidTransition)
	}
	if len(t.open) > 0 {
		return ErrResultsPending
	}
	again := Request{Scope: t.scope, Mentions: t.mentions}
	if err := th.Cancel(now); err != nil {
		return err
	}
	if err := th.Ask(again, now); err != nil {
		return err
	}
	th.historyFrom = th.turns
	return nil
}

// Rerun is «Повторить» of the current request once it is DONE, FAILED, CANCELLED or STOPPED (ADR-0026): a stopped
// request is cancelled, its calls without a result are answered not done, and a new request (next turn, empty
// budget) starts in the same scope with the same mentions; the case adds the student's text again. live tells that
// the request left effects (a journal entry not FAILED or UNDONE, a proposal): a repeat would double them, so it is
// ErrRerunBlocked and nothing changes. A running request is ErrTurnInProgress (also ErrInvalidTransition). It returns the results of the
// closed calls: they belong to the history of the old request, before the new text.
func (th *Thread) Rerun(live bool, now time.Time) ([]values.ToolResult, error) {
	t, err := th.turn()
	if err != nil {
		return nil, err
	}
	switch t.status {
	case StatusDone, StatusFailed, StatusCancelled, StatusStopped:
	case StatusPlanning, StatusExecuting:
		return nil, fmt.Errorf("%w: %w: %s", ErrTurnInProgress, ErrInvalidTransition, t.status)
	default:
		return nil, fmt.Errorf("%w: rerun from %s", ErrInvalidTransition, t.status)
	}
	if live {
		return nil, ErrRerunBlocked
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no time", ErrInvalidThread)
	}

	again := Request{Scope: t.scope, Mentions: t.mentions}
	if t.status == StatusStopped {
		if err := th.Cancel(now); err != nil {
			return nil, err
		}
	}
	th.Abandon(values.RefuseNotDone, now)
	results, _ := th.TakeResults(now)
	if err := th.Ask(again, now); err != nil {
		return nil, err
	}
	return results, nil
}

// Interrupt closes a request whose Driver stops running its calls (its context ended, the app closes). Calls the
// Driver answered before (Answer) keep their results: a write that ran reports its real outcome, never «not done».
// Calls without a result are answered not done, and a running request stops as «нет связи», so «Продолжить» goes on;
// a request the student stopped keeps its reason.
func (th *Thread) Interrupt(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	th.Abandon(values.RefuseNotDone, now)
	if t.status == StatusStopped {
		return nil
	}
	return th.Stop(values.StopOffline, now)
}

// Cancel ends a request that is not finished.
func (th *Thread) Cancel(now time.Time) error {
	t, err := th.turn()
	if err != nil {
		return err
	}
	if err := th.move(StatusCancelled, now); err != nil {
		return err
	}
	t.stepOpen = false
	t.proposed = nil
	th.Abandon(values.RefuseNotDone, now)
	return nil
}

// IsRefusal reports whether err refuses a single call (is_error for the planner) rather than ending the request.
func IsRefusal(err error) bool {
	_, ok := RefusalOf(err)
	return ok
}

// RefusalOf maps an error that refuses a single call to the enum the planner gets back (ADR-0022: never the text).
// A budget stop is a refusal of that call too; the request is STOPPED.
func RefusalOf(err error) (values.Refusal, bool) {
	switch {
	case errors.Is(err, ErrPlanRequired):
		return values.RefusePlanRequired, true
	case errors.Is(err, ErrNotInPlan):
		return values.RefuseNotInPlan, true
	case errors.Is(err, ErrTooManyRounds):
		return values.RefuseTooManyRounds, true
	case errors.Is(err, ErrInvalidPlan):
		return values.RefuseInvalidPlan, true
	case errors.Is(err, actionpolicy.ErrUnknownID):
		return values.RefuseUnknownID, true
	case errors.Is(err, values.ErrUnknownTool), errors.Is(err, actionpolicy.ErrUnknownTool):
		return values.RefuseUnknownTool, true
	case errors.Is(err, values.ErrInvalidArguments), errors.Is(err, values.ErrInvalidRef),
		errors.Is(err, values.ErrInvalidDate), errors.Is(err, values.ErrInvalidText):
		return values.RefuseInvalidArgs, true
	case errors.Is(err, ErrBudgetExceeded):
		return values.RefuseBudget, true
	}
	return "", false
}
