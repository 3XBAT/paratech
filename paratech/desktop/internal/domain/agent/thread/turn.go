package thread

import (
	"slices"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

const maxRounds = values.MaxQuestionRounds

// Turn is the current request of a thread, an entity of the aggregate. It changes only through Thread methods; the
// copy returned by Thread.Turn is a read-only view.
type Turn struct {
	number     int
	status     Status
	resume     Status            // STOPPED only: where «Продолжить» goes back to
	stopReason values.StopReason // STOPPED only
	budget     Budget
	stepOpen   bool
	proposed   []values.PlanStep // AWAITING_PLAN: the plan on the card
	approved   []values.PlanStep // the plan approved with «Да»
	used       []bool            // per approved step: already executed
	retry      int               // «Повторить» of the open step after a permanent failure; part of its key
	open       []OpenCall        // calls of the last step waiting for results
	created    []Created         // IDs created by approved create steps of this request
	startedAt  time.Time
	updatedAt  time.Time
}

// Retry returns how many times the student retried the open step after a permanent failure.
func (t Turn) Retry() int { return t.retry }

// OpenCalls returns the calls of the last step that the thread waits results for.
func (t Turn) OpenCalls() []OpenCall { return slices.Clone(t.open) }

// Created returns the IDs created by approved create steps of this request.
func (t Turn) Created() []Created { return slices.Clone(t.created) }

// Accepting reports whether calls of the current step can still be admitted: the planner is being called. A call
// after a plan, questions or the end of the request in the same step is refused (values.RefuseNotNow).
func (t Turn) Accepting() bool { return t.status.stepping() && !t.stepOpen }

// CanRestartSilently reports whether a request whose model turn expired may start again without asking the
// student: nothing was done yet (first step, no write, no plan).
func (t Turn) CanRestartSilently() bool {
	return t.status == StatusStopped && t.stopReason == values.StopExpired && t.budget.Steps <= 1 &&
		t.budget.Writes == 0 && len(t.approved) == 0
}

// Number returns the request number in the thread, from 1.
func (t Turn) Number() int { return t.number }

// Status returns the status.
func (t Turn) Status() Status { return t.status }

// StopReason returns why a STOPPED request stopped.
func (t Turn) StopReason() values.StopReason { return t.stopReason }

// Budget returns what the request has used.
func (t Turn) Budget() Budget { return t.budget }

// StepOpen reports whether a step was begun and not ended (its Hub call may be repeated with the same key).
func (t Turn) StepOpen() bool { return t.stepOpen }

// ProposedPlan returns the plan waiting for «Да».
func (t Turn) ProposedPlan() []values.PlanStep { return slices.Clone(t.proposed) }

// ApprovedPlan returns the approved plan.
func (t Turn) ApprovedPlan() []values.PlanStep { return slices.Clone(t.approved) }

// StartedAt returns when the request started.
func (t Turn) StartedAt() time.Time { return t.startedAt }

// UpdatedAt returns the time of the last change.
func (t Turn) UpdatedAt() time.Time { return t.updatedAt }

func (t Turn) clone() Turn {
	t.proposed = slices.Clone(t.proposed)
	t.approved = slices.Clone(t.approved)
	t.used = slices.Clone(t.used)
	t.open = slices.Clone(t.open)
	t.created = slices.Clone(t.created)
	return t
}
