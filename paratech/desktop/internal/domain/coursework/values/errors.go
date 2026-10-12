package values

import "errors"

// Errors of the value objects of this package (ARCH-18).
var (
	// ErrInvalidLLMRequest is returned by NewLLMRequest for a request the core must not send.
	ErrInvalidLLMRequest = errors.New("invalid LLM request")

	// ErrInvalidLLMResult is returned by NewLLMResult for malformed accounting data.
	ErrInvalidLLMResult = errors.New("invalid LLM result")
)
