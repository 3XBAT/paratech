package shared

// ThreadID identifies an agent thread: one per subject plus the thread without a subject (ADR-0021).
type ThreadID string

// String returns the identifier as a plain string.
func (id ThreadID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable, a safe file-name segment).
func (id ThreadID) Validate() error { return validateID("thread", string(id)) }

// ActionID identifies an entry of the agent action journal (ADR-0021).
type ActionID string

// String returns the identifier as a plain string.
func (id ActionID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable, a safe file-name segment).
func (id ActionID) Validate() error { return validateID("action", string(id)) }

// NameSegmentProblem returns why s cannot be a single Windows file-name segment, or "" when it can: forbidden or
// control characters, "." or "..", a trailing dot or space, a reserved device name. s must be non-empty. Contexts
// use it to check names that may later become folder or file names (ADR-0022).
func NameSegmentProblem(s string) string { return nameProblem(s) }
