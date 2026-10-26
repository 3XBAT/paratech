package assignment

import (
	"errors"
	"fmt"
)

// Errors of the assignment aggregate (ARCH-18). Messages name fields, never their content.
var (
	// ErrInvalidAssignment is returned by NewExtracted and Restore for an inconsistent assignment.
	ErrInvalidAssignment = errors.New("invalid assignment")

	// ErrInvalidDetails is returned by NewDetails for a title, description or deadline the assignment cannot hold.
	ErrInvalidDetails = errors.New("invalid assignment details")

	// ErrNotExtracted is returned by Reextract for an assignment that was not created from an extraction.
	ErrNotExtracted = errors.New("assignment was not extracted from a transcript")

	// ErrInUse is returned by a repository Delete when other records (a delivery) still reference the assignment,
	// and by DeleteUnused when a draft, a delivery, a work or a live proposal does; it is kept.
	ErrInUse = errors.New("assignment is in use")
)

// The field of an assignment the student adds that NewByStudent or StudentDue refused (ADR-0029). Each wraps
// ErrInvalidDetails, so the form can mark the field and other callers check ErrInvalidDetails alone.
var (
	ErrInvalidTitle = fmt.Errorf("%w: title", ErrInvalidDetails)
	ErrInvalidNote  = fmt.Errorf("%w: note", ErrInvalidDetails)
	ErrInvalidDue   = fmt.Errorf("%w: deadline", ErrInvalidDetails)
	ErrInvalidTime  = fmt.Errorf("%w: time of day", ErrInvalidDetails)
)
