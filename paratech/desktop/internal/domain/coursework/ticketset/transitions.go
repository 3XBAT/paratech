package ticketset

import "slices"

// Status is the state of a ticket set.
type Status string

// Statuses.
const (
	StatusProposed  Status = "proposed"  // «Найдено N билетов — подтвердить»: nothing is paid yet
	StatusConfirmed Status = "confirmed" // the student confirmed the run: its job is queued
	StatusAnswering Status = "answering" // the tickets are answered one per job step
	StatusDone      Status = "done"      // every ticket has its result and the file is filed
	StatusFailed    Status = "failed"    // the file could not be built
	StatusCancelled Status = "cancelled" // the student cancelled the set
)

// Statuses returns every status, for the schema checks.
func Statuses() []Status {
	return []Status{StatusProposed, StatusConfirmed, StatusAnswering, StatusDone, StatusFailed, StatusCancelled}
}

// transitions is the status machine: from -> allowed targets. A status without targets is terminal. Only the
// student's Confirm leaves proposed for a paid run.
var transitions = map[Status][]Status{
	StatusProposed:  {StatusConfirmed, StatusCancelled},
	StatusConfirmed: {StatusAnswering, StatusFailed, StatusCancelled},
	StatusAnswering: {StatusDone, StatusFailed, StatusCancelled},
	StatusDone:      nil,
	StatusFailed:    nil,
	StatusCancelled: nil,
}

// CanTransition reports whether the table allows moving a set from one status to another.
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
