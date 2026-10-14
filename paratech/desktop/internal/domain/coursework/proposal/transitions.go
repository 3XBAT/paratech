package proposal

// Status is the state of a proposal.
type Status string

// Proposal statuses.
const (
	StatusProposed  Status = "PROPOSED"  // parsed from model output, not yet checked against local state
	StatusValidated Status = "VALIDATED" // consistent with local state, waits for the user
	StatusRejected  Status = "REJECTED"  // inconsistent with local state; never executed
	StatusConfirmed Status = "CONFIRMED" // the user explicitly approved it (AC-14)
	StatusDeclined  Status = "DECLINED"  // the user said no; never executed
	StatusExecuted  Status = "EXECUTED"  // the side effect was performed
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{StatusProposed, StatusValidated, StatusRejected, StatusConfirmed, StatusDeclined, StatusExecuted}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// transitions is the status machine: from -> allowed targets. A status without targets is terminal.
var transitions = map[Status][]Status{
	StatusProposed:  {StatusValidated, StatusRejected},
	StatusValidated: {StatusConfirmed, StatusDeclined},
	StatusConfirmed: {StatusExecuted},
	StatusRejected:  nil,
	StatusDeclined:  nil,
	StatusExecuted:  nil,
}

// CanTransition reports whether the table allows moving a proposal from one status to another.
func CanTransition(from, to Status) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Valid reports whether s is one of the statuses of the machine.
func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// IsTerminal reports whether no transition leaves the status.
func (s Status) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}
