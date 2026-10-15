package account

import "errors"

// Domain errors of the aggregate (ARCH-18).
var (
	// ErrInvalidArgument means a request value breaks its format rule (unit, amount, operation, alias, key, id).
	ErrInvalidArgument = errors.New("invalid quota request")

	// ErrQuotaExceeded means the remaining quota is smaller than the requested amount. Nothing was held or recorded.
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrIdempotencyConflict means an idempotency key (or a settled reservation) was used with different arguments.
	ErrIdempotencyConflict = errors.New("idempotency key used with a different request")

	// ErrReservationNotFound means the account has no reservation with the id. A reservation of another tester is
	// reported the same way, so its existence is never revealed.
	ErrReservationNotFound = errors.New("reservation not found")

	// ErrReleased means Settle addressed a reservation that was released by Release or after a failed call.
	ErrReleased = errors.New("reservation was released")

	// ErrAttemptRunning means the latest attempt of a key is still RESERVED and unexpired: the call is in progress.
	ErrAttemptRunning = errors.New("an attempt with this idempotency key is still running")

	// ErrResultMissing means the latest attempt of a key COMPLETED but its result is not stored, so it cannot be
	// replayed (it was settled outside the gateway).
	ErrResultMissing = errors.New("the result of the completed attempt is not stored")

	// ErrInvalidTransition means a reservation state change is not in the transition table.
	ErrInvalidTransition = errors.New("invalid reservation state transition")

	// ErrInvalidPolicy means a quota policy value is out of range.
	ErrInvalidPolicy = errors.New("invalid quota policy")

	// ErrInvalidRecord means Restore got stored data that breaks an invariant of the aggregate.
	ErrInvalidRecord = errors.New("invalid quota record")
)
