package work

import "slices"

// Status is the state of a work.
type Status string

// Statuses.
const (
	StatusTriage    Status = "triage"    // the reader call of the current round is due (asked again on a retry)
	StatusQuestions Status = "questions" // the reader asked (round 1 or 2): the student answers or skips
	StatusReview    Status = "review"    // «Как я понял задание» is ready: the student edits and confirms
	StatusConfirmed Status = "confirmed" // the requirements are confirmed: the work may be written (TASK-105)
	StatusWriting   Status = "writing"
	StatusDone      Status = "done"
	StatusHelper    Status = "helper" // the app cannot do the work: the helper mode
	StatusFailed    Status = "failed" // the reader refused MaxAttempts answers of a round
)

// Statuses returns every status, for the schema checks.
func Statuses() []Status {
	return []Status{
		StatusTriage, StatusQuestions, StatusReview, StatusConfirmed, StatusWriting, StatusDone, StatusHelper,
		StatusFailed,
	}
}

// transitions is the status machine: from -> allowed targets. A status without targets is terminal. No status leads
// to a third question round: questions → triage raises the round, and an answer of the final round with questions is
// refused (ErrTooManyRounds).
var transitions = map[Status][]Status{
	StatusTriage: {
		StatusQuestions, // the reader asked, in round 1 or 2
		StatusReview,    // the reader understood the work without questions, or the final round answered
		StatusHelper,    // the work is impossible as a whole
		StatusFailed,    // the reader refused its last attempt
	},
	StatusQuestions: {
		StatusTriage, // answered or «Пропустить»: the next round's reader call
	},
	StatusReview: {
		StatusConfirmed, // «Подтвердить»
	},
	StatusConfirmed: {
		StatusWriting,
	},
	StatusWriting: {
		StatusDone,
	},
	StatusDone:   nil,
	StatusHelper: nil,
	StatusFailed: nil,
}

// CanTransition reports whether the table allows moving a work from one status to another.
func CanTransition(from, to Status) bool { return slices.Contains(transitions[from], to) }

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
