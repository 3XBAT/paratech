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
	startedAt  time.Time
	updatedAt  time.Time
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
	return t
}
