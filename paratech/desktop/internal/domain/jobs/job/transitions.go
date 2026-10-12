package job

// transitions is the status machine of PRD section 7.2: from -> allowed targets. A status without targets is
// terminal. Every status must be a key (a test compares the keys with Statuses and the schema).
var transitions = map[Status][]Status{
	StatusQueued: {StatusRunning, StatusWaitingForPower, StatusCancelled},
	StatusRunning: {
		StatusCompleted, StatusRetryWait, StatusFailed, StatusWaitingForNetwork, StatusWaitingForQuota,
		StatusWaitingForPower, StatusCancelled,
		StatusQueued, // startup recovery of a stale RUNNING job
	},
	StatusRetryWait:         {StatusQueued},
	StatusWaitingForNetwork: {StatusQueued},
	StatusWaitingForQuota:   {StatusQueued},
	StatusWaitingForPower:   {StatusQueued},
	StatusCompleted:         nil,
	StatusFailed:            nil,
	StatusCancelled:         nil,
}

// CanTransition reports whether the table allows moving a job from one status to another.
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

// IsTerminal reports whether no transition leaves the status: COMPLETED, FAILED and CANCELLED.
func (s Status) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}

// IsWaiting reports whether s is one of the WAITING_FOR_* statuses.
func (s Status) IsWaiting() bool {
	switch s {
	case StatusWaitingForNetwork, StatusWaitingForQuota, StatusWaitingForPower:
		return true
	}
	return false
}
