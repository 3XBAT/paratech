package backoff

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Markers a handler wraps its error with. Handlers live in other use cases and cannot import the job runner
// (ARCH-20), so the markers are part of the domain.
var (
	// ErrTransient marks a failure that may go away: the job is retried with backoff.
	ErrTransient = errors.New("transient failure")

	// ErrPermanent marks a failure that retrying cannot fix: the job fails at once.
	ErrPermanent = errors.New("permanent failure")
)

// Class is the retry class of an error.
type Class int

// Classes of errors.
const (
	ClassTransient Class = iota
	ClassPermanent
)

// Transient wraps err as a transient failure.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrTransient, err)
}

// Permanent wraps err as a permanent failure.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}

// Classify returns the class of err. An error with no marker is transient, so a handler that forgets to classify
// still stops at the attempt cap (AC-10). ErrPermanent wins over ErrTransient when both are present.
func Classify(err error) Class {
	switch {
	case errors.Is(err, ErrPermanent),
		errors.Is(err, shared.ErrStoragePermission),
		errors.Is(err, shared.ErrInvalidID),
		errors.Is(err, shared.ErrInvalidArtifactRef):
		return ClassPermanent
	}
	return ClassTransient
}
