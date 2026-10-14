package contextselection

import "errors"

// Errors of Select (ARCH-18). Messages name fields, never their content.
var (
	// ErrUnknownOperation is returned for an operation outside the values.Operation catalog.
	ErrUnknownOperation = errors.New("unknown LLM operation")

	// ErrMissingContext is returned when the material lacks a field the operation requires.
	ErrMissingContext = errors.New("missing context for the LLM operation")

	// ErrInvalidMaterial is returned for text that is not valid UTF-8, holds control characters or exceeds a limit.
	ErrInvalidMaterial = errors.New("invalid LLM context material")
)
