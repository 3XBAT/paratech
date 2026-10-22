package thread

// Status is the state of a request (turn).
type Status string

// Turn statuses.
const (
	StatusPlanning        Status = "PLANNING"
	StatusAwaitingPlan    Status = "AWAITING_PLAN"
	StatusAwaitingAnswers Status = "AWAITING_ANSWERS"
	StatusExecuting       Status = "EXECUTING"
	StatusDone            Status = "DONE"
	StatusStopped         Status = "STOPPED"
	StatusFailed          Status = "FAILED"
	StatusCancelled       Status = "CANCELLED"
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{
		StatusPlanning, StatusAwaitingPlan, StatusAwaitingAnswers, StatusExecuting,
		StatusDone, StatusStopped, StatusFailed, StatusCancelled,
	}
}

// transitions is the turn status machine: from -> allowed targets. A status without targets is terminal.
var transitions = map[Status][]Status{
	StatusPlanning: {
		StatusAwaitingPlan,    // show_plan
		StatusAwaitingAnswers, // ask_questions
		StatusExecuting,       // a write ran without a plan
		StatusDone,            // reply / cannot_do / nothing left
		StatusStopped,         // budget, offline, the student's «Стоп»
		StatusFailed,          // the step failed for good
		StatusCancelled,
	},
	StatusAwaitingPlan: {
		StatusExecuting, // «Да»
		StatusPlanning,  // «Изменить»
		StatusCancelled, // «Отмена»
	},
	StatusAwaitingAnswers: {
		StatusPlanning, // answered
		StatusCancelled,
	},
	StatusExecuting: {
		StatusAwaitingPlan, // a new plan replaces the approved one
		StatusDone,
		StatusStopped,
		StatusFailed,
		StatusCancelled,
	},
	StatusStopped: {
		StatusPlanning,  // «Продолжить» where it stopped
		StatusExecuting, // «Продолжить» where it stopped
		StatusCancelled,
	},
	StatusDone:      nil,
	StatusFailed:    nil,
	StatusCancelled: nil,
}

// CanTransition reports whether the table allows moving a turn from one status to another.
func CanTransition(from, to Status) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Valid reports whether s is one of the statuses.
func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// IsTerminal reports whether no transition leaves the status.
func (s Status) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}

// stepping reports whether the planner is being called in this status.
func (s Status) stepping() bool { return s == StatusPlanning || s == StatusExecuting }

// String returns the status as stored.
func (s Status) String() string { return string(s) }
