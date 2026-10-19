package recording

// transitions is the status machine of PRD section 6.2: from -> allowed targets. A status without targets is
// terminal. Every status must be a key (a test compares the keys with Statuses and the schema).
var transitions = map[Status][]Status{
	StatusStarting:         {StatusRecording, StatusFailed},
	StatusRecording:        {StatusStopping, StatusFinalizing, StatusRecoveryRequired},
	StatusStopping:         {StatusFinalizing},
	StatusFinalizing:       {StatusCompleted, StatusFailed},
	StatusRecoveryRequired: {StatusFinalizing, StatusFailed},
	StatusCompleted:        nil,
	StatusFailed:           nil,
}

// CanTransition reports whether the table allows moving a recording from one status to another.
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

// IsTerminal reports whether no transition leaves the status: COMPLETED and FAILED.
func (s Status) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}

// InProgress reports whether the recording is being captured or finalized right now. RECOVERY_REQUIRED is not: it
// waits for a recovery or a decision of the user.
func (s Status) InProgress() bool {
	return s.Valid() && !s.IsTerminal() && s != StatusRecoveryRequired
}

// CanSwitchSource reports whether the user may switch a recording in status s to another source (TASK-082): only
// while it is captured. The session keeps the source; the recording goes on with the same chunks.
func (s Status) CanSwitchSource() bool { return s == StatusRecording }

// UnfinishedStatuses returns the non-terminal statuses, for the startup recovery of interrupted recordings.
func UnfinishedStatuses() []Status {
	var out []Status
	for _, s := range Statuses() {
		if !s.IsTerminal() {
			out = append(out, s)
		}
	}
	return out
}
