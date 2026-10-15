package delivery

// transitions is the status machine of a delivery (PRD sections 10 and 14 case 7, ADR-0004(c)): from -> allowed
// targets. A status without targets is terminal. Every status must be a key (a test compares the keys with Statuses
// and the schema).
var transitions = map[Status][]Status{
	StatusPending: {
		StatusSending, // an attempt starts; saved before the message is handed to the provider
		StatusFailed,  // the attachment is gone or changed before any attempt
	},
	StatusSending: {
		StatusSent,    // accepted by the provider
		StatusPending, // definitely not accepted (transient): a later attempt may send it
		StatusFailed,  // definitely refused (rejected), nothing was sent
		StatusUnknown, // ambiguous result, or interrupted (startup recovery)
	},
	StatusUnknown: {
		StatusSent,    // reconciliation found it, or the user marked it as sent after checking the mailbox
		StatusPending, // reconciliation found it was not accepted, or the user chose to send again
	},
	StatusSent:   nil,
	StatusFailed: nil,
}

// CanTransition reports whether the table allows moving a delivery from one status to another.
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

// IsTerminal reports whether no transition leaves the status: SENT and FAILED.
func (s Status) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}

// IsUnresolved reports whether a delivery in status s may still send its message or may already have sent it without
// the app knowing (PENDING, SENDING, UNKNOWN). While one is unresolved, no other delivery of the same assignment is
// created, so an unknown outcome cannot be bypassed by sending a new one (hard rule 7).
func (s Status) IsUnresolved() bool { return s.Valid() && !s.IsTerminal() }
