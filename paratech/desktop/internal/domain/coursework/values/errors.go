package values

import "errors"

// Errors of the value objects of this package (ARCH-18).
var (
	// ErrInvalidLLMRequest is returned by NewLLMRequest for a request the core must not send.
	ErrInvalidLLMRequest = errors.New("invalid LLM request")

	// ErrInvalidLLMResult is returned by NewLLMResult for malformed accounting data.
	ErrInvalidLLMResult = errors.New("invalid LLM result")

	// ErrInvalidPostProcessRequest is returned for a post-processing job payload that is not a valid request.
	ErrInvalidPostProcessRequest = errors.New("invalid post-processing request")

	// ErrInvalidDraftRequest is returned for a homework_draft job payload or instruction that is not valid.
	ErrInvalidDraftRequest = errors.New("invalid homework draft request")
)
