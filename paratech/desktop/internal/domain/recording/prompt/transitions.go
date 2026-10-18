package prompt

// State is the state of a prompt (PRD section 6.1). Allowed transitions are the table below.
type State string

// Prompt states.
const (
	StateOffered  State = "OFFERED"  // the popup shows the offer
	StateStarting State = "STARTING" // a start of the recording runs
	StateStarted  State = "STARTED"  // the recording started
	StateSkipped  State = "SKIPPED"  // the user declined
	StateExpired  State = "EXPIRED"  // the lesson ended
)

// States returns every state.
func States() []State {
	return []State{StateOffered, StateStarting, StateStarted, StateSkipped, StateExpired}
}

// String returns the state name.
func (s State) String() string { return string(s) }

// transitions is the state machine: from -> allowed targets. A state without targets is terminal. Every state must
// be a key (a test compares the keys with States). STARTING -> OFFERED is a failed start.
var transitions = map[State][]State{
	StateOffered:  {StateStarting, StateSkipped, StateExpired},
	StateStarting: {StateStarted, StateOffered},
	StateStarted:  nil,
	StateSkipped:  nil,
	StateExpired:  nil,
}

// CanTransition reports whether the table allows moving a prompt from one state to another.
func CanTransition(from, to State) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Valid reports whether s is one of the states of the machine.
func (s State) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// IsTerminal reports whether no transition leaves the state: the prompt is over.
func (s State) IsTerminal() bool {
	targets, ok := transitions[s]
	return ok && len(targets) == 0
}

// Visible reports whether the popup shows a prompt in this state.
func (s State) Visible() bool { return s == StateOffered || s == StateStarting }
