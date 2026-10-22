package action

// Status is the state of a journal entry.
type Status string

// Action statuses.
const (
	StatusPending     Status = "PENDING"
	StatusDone        Status = "DONE"
	StatusFailed      Status = "FAILED"
	StatusUnknown     Status = "UNKNOWN"
	StatusUndoing     Status = "UNDOING"
	StatusUndone      Status = "UNDONE"
	StatusUndoRefused Status = "UNDO_REFUSED"
	StatusProposed    Status = "PROPOSED"
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{
		StatusPending, StatusDone, StatusFailed, StatusUnknown, StatusUndoing, StatusUndone, StatusUndoRefused,
		StatusProposed,
	}
}

// transitions is the journal status machine: from -> allowed targets. A status without targets is terminal.
var transitions = map[Status][]Status{
	StatusPending: {
		StatusDone,    // the use case succeeded
		StatusFailed,  // the use case failed, nothing changed
		StatusUnknown, // startup recovery: the outcome is not known, no Undo
	},
	StatusDone: {
		StatusUndoing, // «Отменить»
	},
	StatusUndoing: {
		StatusUndone,      // compensation applied
		StatusUndoRefused, // the target changed since the action
	},
	StatusFailed:      nil,
	StatusUnknown:     nil,
	StatusUndone:      nil,
	StatusUndoRefused: nil,
	StatusProposed:    nil, // the proposal has its own life cycle
}

// CanTransition reports whether the table allows moving an action from one status to another.
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

// String returns the status as stored.
func (s Status) String() string { return string(s) }
