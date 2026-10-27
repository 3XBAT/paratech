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
	// and by DeleteUnused when a draft, a delivery, a work or a live proposal does; it is kept. The soft delete
	// (ADR-0029) refuses with it while something in progress uses the assignment.
	ErrInUse = errors.New("assignment is in use")

	// ErrDeleted is returned by a change of a deleted assignment, including a second Delete (ADR-0029).
	ErrDeleted = errors.New("assignment is deleted")

	// ErrNotDeleted is returned by Restore of an assignment that is not deleted.
	ErrNotDeleted = errors.New("assignment is not deleted")
)

// The field of an assignment the student adds that NewByStudent or StudentDue refused (ADR-0029). Each wraps
// ErrInvalidDetails, so the form can mark the field and other callers check ErrInvalidDetails alone.
var (
	ErrInvalidTitle = fmt.Errorf("%w: title", ErrInvalidDetails)
	ErrInvalidNote  = fmt.Errorf("%w: note", ErrInvalidDetails)
	ErrInvalidDue   = fmt.Errorf("%w: deadline", ErrInvalidDetails)
	ErrInvalidTime  = fmt.Errorf("%w: time of day", ErrInvalidDetails)
)
