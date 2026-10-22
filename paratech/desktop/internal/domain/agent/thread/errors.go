package thread

import "errors"

// Errors of the aggregate. ErrPlanRequired, ErrNotInPlan, ErrTooManyRounds and the provenance error of actionpolicy
// become an is_error result for the planner; the call is never executed.
var (
	// ErrInvalidThread is returned by New and Restore for data that breaks an invariant.
	ErrInvalidThread = errors.New("invalid agent thread")

	// ErrInvalidTransition is returned for a status change the transition table does not allow, or an operation the
	// current status or step does not allow.
	ErrInvalidTransition = errors.New("invalid agent turn transition")

	// ErrNoTurn means the thread has no request yet.
	ErrNoTurn = errors.New("thread has no request")

	// ErrTurnInProgress means a new request was started while the current one is still running.
	ErrTurnInProgress = errors.New("a request is in progress")

	// ErrBudgetExceeded means a limit of the request was reached; the request is STOPPED with reason budget.
	ErrBudgetExceeded = errors.New("request budget exceeded")

	// ErrPlanRequired means the write needs an approved plan first (show_plan).
	ErrPlanRequired = errors.New("plan required")

	// ErrNotInPlan means the write does not match an unused step of the approved plan.
	ErrNotInPlan = errors.New("write is not in the approved plan")

	// ErrTooManyRounds means a third question round was refused; the planner continues with explicit assumptions.
	ErrTooManyRounds = errors.New("too many question rounds")

	// ErrInvalidPlan means the proposed plan breaks a rule (size, tool class, provenance).
	ErrInvalidPlan = errors.New("invalid plan")
)
