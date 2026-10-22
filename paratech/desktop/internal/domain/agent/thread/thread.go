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

// Thread is the agent thread of one subject or of no subject. Fields change only through its methods (ARCH-14).
type Thread struct {
	version   int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id        shared.ThreadID
	key       Key
	turns     int   // number of the last request; 0 = none yet
	current   *Turn // the last request; nil only when turns == 0
	seen      values.RefSet
	createdAt time.Time
	updatedAt time.Time
}

// New creates an empty thread.
func New(id shared.ThreadID, key Key, now time.Time) (*Thread, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidThread, err)
	}
	if _, err := ParseKey(string(key)); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidThread)
	}
	return &Thread{id: id, key: key, seen: values.RefSet{}, createdAt: now, updatedAt: now}, nil
}

// TurnState is the stored form of the current request.
type TurnState struct {
	Number       int
	Status       Status
	Resume       Status
	StopReason   values.StopReason
	Budget       Budget
	StepOpen     bool
	ProposedPlan []values.PlanStep
	ApprovedPlan []values.PlanStep
	Used         []bool
	StartedAt    time.Time
	UpdatedAt    time.Time
}

// State is the stored form of a thread, for Restore.
type State struct {
	Version   int64
	ID        shared.ThreadID
	Key       Key
	Turns     int
	Current   *TurnState // nil when Turns == 0
	Seen      []values.Ref
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored thread without side effects and checks its invariants.
func Restore(s State) (*Thread, error) {
	bad := func(what string) (*Thread, error) { return nil, fmt.Errorf("%w: %s", ErrInvalidThread, what) }
	if s.Version < 1 {
		return bad("stored version must be at least 1")
	}
	th, err := New(s.ID, s.Key, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return bad("updated before created")
	}
	if s.Turns < 0 || (s.Turns == 0) != (s.Current == nil) {
		return bad("a thread has a current request exactly when it has requests")
	}
	if len(s.Seen) > MaxSeenRefs {
		return bad("too many seen ids")
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
		th.current = &t
	}
	th.version, th.turns, th.updatedAt = s.Version, s.Turns, s.UpdatedAt
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
	}
	return Turn{
		number: s.Number, status: s.Status, resume: s.Resume, stopReason: s.StopReason, budget: s.Budget,
		stepOpen: s.StepOpen, proposed: slices.Clone(s.ProposedPlan), approved: slices.Clone(s.ApprovedPlan),
		used: slices.Clone(s.Used), startedAt: s.StartedAt, updatedAt: s.UpdatedAt,
	}, nil
}

// State returns the stored form of the thread.
func (th *Thread) State() State {
	s := State{
		Version: th.version, ID: th.id, Key: th.key, Turns: th.turns, Seen: th.seen.Sorted(),
		CreatedAt: th.createdAt, UpdatedAt: th.updatedAt,
	}
	if th.current != nil {
		t := th.current
		s.Current = &TurnState{
			Number: t.number, Status: t.status, Resume: t.resume, StopReason: t.stopReason, Budget: t.budget,
			StepOpen: t.stepOpen, ProposedPlan: slices.Clone(t.proposed), ApprovedPlan: slices.Clone(t.approved),
			Used: slices.Clone(t.used), StartedAt: t.startedAt, UpdatedAt: t.updatedAt,
		}
	}
	return s
}

// ID returns the thread ID.
func (th *Thread) ID() shared.ThreadID { return th.id }

// Key returns the subject key of the thread.
func (th *Thread) Key() Key { return th.key }

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
	if t := th.current; t != nil && !t.status.IsTerminal() {
		if t.status.stepping() {
			return ErrTurnInProgress
		}
		if err := th.Cancel(now); err != nil {
			return err
		}
	}
	th.turns++
	th.current = &Turn{number: th.turns, status: StatusPlanning, startedAt: now, updatedAt: now}
	th.updatedAt = now
	return nil
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
// token limit the request stops (budget) and ErrBudgetExceeded is returned.
func (th *Thread) BeginStep(now time.Time) (string, error) {
	t, err := th.ready()
	if err != nil {
		return "", err
	}
	if t.budget.Steps >= MaxSteps {
		return "", th.stopBudget("steps", now)
	}
	if t.budget.InputTokens >= MaxInputTokens {
		return "", th.stopBudget("input tokens", now)
	}
	t.budget.Steps++
	t.stepOpen = true
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

func (th *Thread) stepKey(t *Turn) string {
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
}

// Admission is the decision on one call.
type Admission struct {
	Class    actionpolicy.Class
	Paid     bool
	PlanStep int // index of the approved step it executes; -1 when there is no approved plan
}

// Admit decides whether a call may run (ADR-0021, ADR-0022): every ID must have typed provenance; a U call counts
// as a write, must match an unused step when a plan is approved and otherwise must not need a plan. C calls only
// create a proposal (Admission.Class tells), UI calls are handled by ProposePlan, AskQuestions and Complete. A
// refused call is never executed; over the write limit the request stops (budget).
func (th *Thread) Admit(call values.Call, f Facts, now time.Time) (Admission, error) {
	t, err := th.ready()
	if err != nil {
		return Admission{}, err
	}
	class, err := actionpolicy.Classify(call.Tool(), f.ReplacesReady)
	if err != nil {
		return Admission{}, err
	}
	rule, _ := actionpolicy.Of(call.Tool())
	if err := actionpolicy.CheckProvenance(call.Refs(), th.known(f.Context)); err != nil {
		return Admission{}, err
	}
	adm := Admission{Class: class, Paid: rule.Paid, PlanStep: -1}
	if !class.IsWrite() {
		return adm, nil
	}

	if t.budget.Writes >= MaxWrites {
		return Admission{}, th.stopBudget("writes", now)
	}
	if len(t.approved) > 0 {
		for i, s := range t.approved {
			if !t.used[i] && s.Matches(call) {
				adm.PlanStep = i
				break
			}
		}
		if adm.PlanStep < 0 {
			return Admission{}, ErrNotInPlan
		}
		t.used[adm.PlanStep] = true
	} else {
		reason, err := actionpolicy.NeedsPlan(call.Tool(), actionpolicy.Facts{
			WritesBefore: t.budget.Writes, CostMicroUSD: f.CostMicroUSD, Triage: f.Triage,
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
	}
	t.budget.Writes++
	if t.status == StatusPlanning {
		if err := th.move(StatusExecuting, now); err != nil {
			return Admission{}, err
		}
	}
	t.updatedAt, th.updatedAt = now, now
	return adm, nil
}

func (th *Thread) known(context values.RefSet) values.RefSet {
	k := th.seen.Clone()
	for r := range context {
		k.Add(r)
	}
	return k
}

// --- plan --------------------------------------------------------------------------------------------

// ProposePlan records the steps of show_plan and waits for «Да». Steps are U or C tools only, at most MaxWrites
// writes, and their IDs pass the provenance check.
func (th *Thread) ProposePlan(steps []values.PlanStep, context values.RefSet, now time.Time) error {
	if _, err := th.ready(); err != nil {
		return err
	}
	if len(steps) == 0 || len(steps) > values.MaxPlanSteps {
		return fmt.Errorf("%w: a plan has 1..%d steps", ErrInvalidPlan, values.MaxPlanSteps)
	}
	writes := 0
	known := th.known(context)
	for _, s := range steps {
		r, err := actionpolicy.Of(s.Tool())
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidPlan, err)
		}
		if r.Class != actionpolicy.ClassU && r.Class != actionpolicy.ClassC {
			return fmt.Errorf("%w: step %s is not an action", ErrInvalidPlan, s.Tool())
		}
		if r.Class == actionpolicy.ClassU {
			writes++
		}
		if err := actionpolicy.CheckProvenance(s.Args().Refs(), known); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidPlan, err)
		}
	}
	if writes > MaxWrites {
		return fmt.Errorf("%w: more than %d writes", ErrInvalidPlan, MaxWrites)
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
	return nil
}

// RevisePlan is «Изменить»: the plan is dropped and the planner continues.
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
	t.proposed = nil
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
	if t.stopReason == values.StopBudget {
		return ErrBudgetExceeded
	}
	return th.move(t.resume, now)
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
	return nil
}

// IsRefusal reports whether err refuses a single call (is_error for the planner) rather than ending the request.
func IsRefusal(err error) bool {
	return errors.Is(err, ErrPlanRequired) || errors.Is(err, ErrNotInPlan) || errors.Is(err, ErrTooManyRounds) ||
		errors.Is(err, ErrInvalidPlan) || errors.Is(err, actionpolicy.ErrUnknownID) || errors.Is(err, values.ErrUnknownTool) ||
		errors.Is(err, values.ErrInvalidArguments)
}
