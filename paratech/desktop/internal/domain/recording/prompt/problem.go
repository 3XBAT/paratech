package prompt

import (
	"errors"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
)

// Problem is why the last start of a prompt failed; the popup shows a fixed text for each kind, never an error text.
type Problem string

// Problem kinds. ProblemNone means the last start did not fail (or none ran).
const (
	ProblemNone        Problem = ""
	ProblemUnavailable Problem = "unavailable" // this build cannot capture audio
	ProblemInProgress  Problem = "in_progress" // another recording is in progress
	ProblemRecovering  Problem = "recovering"  // interrupted recordings are being recovered; try again shortly
	ProblemFailed      Problem = "failed"      // any other failure
)

// Problems returns every problem kind except ProblemNone.
func Problems() []Problem {
	return []Problem{ProblemUnavailable, ProblemInProgress, ProblemRecovering, ProblemFailed}
}

// Valid reports whether p is a known kind other than ProblemNone.
func (p Problem) Valid() bool {
	switch p {
	case ProblemUnavailable, ProblemInProgress, ProblemRecovering, ProblemFailed:
		return true
	}
	return false
}

// String returns the problem code.
func (p Problem) String() string { return string(p) }

// ProblemOf classifies why a recording did not start: no capture in this build, another recording in progress, or
// anything else. A nil error has no problem.
func ProblemOf(err error) Problem {
	switch {
	case err == nil:
		return ProblemNone
	case errors.Is(err, recording.ErrCaptureUnavailable):
		return ProblemUnavailable
	case errors.Is(err, limits.ErrRecordingInProgress):
		return ProblemInProgress
	}
	return ProblemFailed
}
