package limits

import (
	"errors"
	"fmt"
	"time"
)

// Bounds of the hard cap. A cap outside them is a configuration mistake, not a user choice.
const (
	DefaultMaxDuration = 3 * time.Hour
	MinMaxDuration     = time.Minute
	MaxMaxDuration     = 12 * time.Hour
)

// maxInProgress is how many recordings may be captured or finalized at the same time.
const maxInProgress = 1

// Errors of the limits service (ARCH-18).
var (
	// ErrInvalidPolicy is returned by New for a cap outside the bounds.
	ErrInvalidPolicy = errors.New("invalid recording limits")

	// ErrRecordingInProgress means a new recording was refused because another one is not finished yet.
	ErrRecordingInProgress = errors.New("another recording is in progress")
)

// Policy holds the recording limits.
type Policy struct{ maxDuration time.Duration }

// New validates the hard cap and builds a policy.
func New(maxDuration time.Duration) (Policy, error) {
	if maxDuration < MinMaxDuration || maxDuration > MaxMaxDuration {
		return Policy{}, fmt.Errorf("%w: hard cap %v is outside %v..%v", ErrInvalidPolicy, maxDuration, MinMaxDuration, MaxMaxDuration)
	}
	return Policy{maxDuration: maxDuration}, nil
}

// Default returns the policy with the default cap of 3 hours.
func Default() Policy { return Policy{maxDuration: DefaultMaxDuration} }

// MaxDuration returns the hard cap for a new recording.
func (p Policy) MaxDuration() time.Duration { return p.maxDuration }

// AllowStart decides whether a new recording may start while inProgress recordings are still captured or finalized.
func (p Policy) AllowStart(inProgress int) error {
	if inProgress >= maxInProgress {
		return fmt.Errorf("%w: %d of %d", ErrRecordingInProgress, inProgress, maxInProgress)
	}
	return nil
}
