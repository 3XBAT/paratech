package prompts

import "errors"

// Errors of this package (ARCH-18). Messages name fields and item positions, never model or user text.
var (
	// ErrNoContext is returned when a request is built or an answer is checked without the context it needs (the
	// zero contextselection.Context, or no lesson start for checking deadlines).
	ErrNoContext = errors.New("no LLM context")

	// ErrMalformedOutput is returned when the model answer does not match the schema of its operation.
	ErrMalformedOutput = errors.New("model output does not match the schema")

	// ErrTruncatedOutput is returned, together with ErrMalformedOutput, when the JSON object of an answer starts but
	// never ends: the answer was most likely cut at the output limit of the model alias on the Hub.
	ErrTruncatedOutput = errors.New("model output is truncated")
)
