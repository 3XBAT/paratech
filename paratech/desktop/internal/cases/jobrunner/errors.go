package jobrunner

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the job runner (ARCH-18).
var (
	// ErrInvalidConfig is returned by New and Register for unusable settings.
	ErrInvalidConfig = errors.New("invalid job runner configuration")

	// ErrNoHandlers is returned by Start when no handler is registered, so there is nothing to claim.
	ErrNoHandlers = errors.New("no job handlers registered")

	// ErrAlreadyStarted is returned by Start called twice, and by Register called after Start.
	ErrAlreadyStarted = errors.New("job runner already started")

	// ErrStopped is returned by Start after Stop.
	ErrStopped = errors.New("job runner stopped")

	// ErrNotCancellable means the job cannot be cancelled now: it is finished, or it runs a handler that is not
	// safe to interrupt.
	ErrNotCancellable = errors.New("job cannot be cancelled")

	// ErrJobFinished is what a progress report returns after its job has left RUNNING in this runner. It wraps
	// shared.ErrConflict, so a handler that stops on conflicts stops on this too.
	ErrJobFinished = fmt.Errorf("%w: job is no longer running", shared.ErrConflict)
)

// Reasons a running handler is stopped; they are context causes, never returned to callers.
var (
	errCancelled = errors.New("cancelled by the user")
	errTimeout   = errors.New("job timed out")
	errLost      = errors.New("job changed elsewhere")
	errShutdown  = errors.New("job runner is stopping")
	errFinished  = errors.New("handler returned")
)
