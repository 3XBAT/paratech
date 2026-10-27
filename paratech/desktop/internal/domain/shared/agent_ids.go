package shared

// ThreadID identifies an agent thread, one per chat (ADR-0028).
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

// WorkID identifies a turnkey work (coursework/work, TASK-129).
type WorkID string

// String returns the identifier as a plain string.
func (id WorkID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable, a safe file-name segment).
func (id WorkID) Validate() error { return validateID("work", string(id)) }

// AnswerID identifies an answer with citations to a student's question (coursework/answer, TASK-118).
type AnswerID string

// String returns the identifier as a plain string.
func (id AnswerID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable, a safe file-name segment).
func (id AnswerID) Validate() error { return validateID("answer", string(id)) }

// TicketSetID identifies a set of exam tickets with their answers (coursework/ticketset, TASK-133).
type TicketSetID string

// String returns the identifier as a plain string.
func (id TicketSetID) String() string { return string(id) }

// Validate reports whether the identifier is well formed (non-empty, bounded, printable, a safe file-name segment).
func (id TicketSetID) Validate() error { return validateID("ticket set", string(id)) }

// NameSegmentProblem returns why s cannot be a single Windows file-name segment, or "" when it can: forbidden or
// control characters, "." or "..", a trailing dot or space, a reserved device name. s must be non-empty. Contexts
// use it to check names that may later become folder or file names (ADR-0022).
func NameSegmentProblem(s string) string { return nameProblem(s) }
