package values

// Operation names an LLM operation (PRD sections 9.1 and 9.2). TASK-041 defines the operations:
// summary, extract_homework_deadlines and homework_draft.
type Operation string

// LLMRequest is what the core sends to the Hub for one LLM operation (PRD sections 4.4, 9.1 and 9.2).
//
// Instructions are trusted text written by the app. UntrustedData carries user data such as transcript
// excerpts; it is kept separate from the instructions and must never be interpreted as instructions
// (hard rule 3, PRD section 13.3). Raw audio is never part of a request (AC-06).
type LLMRequest struct {
	Operation      Operation
	ModelAlias     string // a Hub-side alias, never a provider model name or a URL (PRD section 13.2)
	Instructions   string
	UntrustedData  string
	IdempotencyKey string // stable across retries of the same job so the Hub never charges twice (AC-11)
}

// LLMResult is the answer of the Hub. Output is UNTRUSTED model output: the caller must validate it
// against a strict schema and trusted local state before any side effect (hard rule 3, AC-18).
type LLMResult struct {
	Output       string
	InputTokens  int64
	OutputTokens int64
}
