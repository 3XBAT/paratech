package assignment

import "errors"

// Errors of the assignment aggregate (ARCH-18). Messages name fields, never their content.
var (
	// ErrInvalidAssignment is returned by NewExtracted and Restore for an inconsistent assignment.
	ErrInvalidAssignment = errors.New("invalid assignment")

	// ErrInvalidDetails is returned by NewDetails for a title, description or deadline the assignment cannot hold.
	ErrInvalidDetails = errors.New("invalid assignment details")

	// ErrNotExtracted is returned by Reextract for an assignment that was not created from an extraction.
	ErrNotExtracted = errors.New("assignment was not extracted from a transcript")

	// ErrInUse is returned by a repository Delete when other records (a delivery) still reference the assignment;
	// it is kept.
	ErrInUse = errors.New("assignment is in use")
)
