package shared

import (
	"errors"
	"fmt"
)

// Base errors that cross port boundaries. Adapters translate driver errors into these values
// (ARCH-32) and use cases check them with errors.Is (ARCH-18).
var (
	// ErrNotFound means the requested aggregate or artifact does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict means a write was rejected because of the current state of the data. It is the general class:
	// callers that only need "reload and decide again" check it. Two narrower errors wrap it and tell the cause, so
	// errors.Is(err, ErrConflict) is true for both: ErrVersionConflict (the aggregate was changed or removed since it
	// was loaded) and ErrDuplicate (a uniqueness or idempotency key is already taken). A plain ErrConflict is also
	// returned for other state preconditions of a port, for example a foreign key that still references the row
	// (deleting an STT model that is the default) or purging a lesson that is not soft-deleted.
	ErrConflict = errors.New("conflict")

	// ErrNetworkUnavailable means a cloud call could not reach the network; an LLM job then waits in
	// WAITING_FOR_NETWORK (PRD section 7.2).
	ErrNetworkUnavailable = errors.New("network unavailable")

	// ErrQuotaExceeded means the Hub rejected a quota reservation; an LLM job then waits in
	// WAITING_FOR_QUOTA (PRD sections 7.2 and 9.2).
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrInvalidID is returned by identifier Validate methods.
	ErrInvalidID = errors.New("invalid identifier")

	// ErrInvalidArtifactRef is returned for artifact references that are malformed or try to leave
	// the artifact root (the reference convention is defined by TASK-006).
	ErrInvalidArtifactRef = errors.New("invalid artifact reference")
)

// Conflict causes. They wrap ErrConflict (errors.Is(err, ErrConflict) holds) and are distinct from each other, so a
// caller can pick the right recovery: reload for a version conflict, look up the existing row for a duplicate.
var (
	// ErrVersionConflict is returned by a repository Save (and ApplyChanges, Delete of a versioned aggregate) when
	// the compare-and-swap on the aggregate version failed: the stored row has another version than the aggregate
	// was loaded with, or the aggregate has a version but its row no longer exists (ADR-0004). Nothing was written.
	// The use case reloads the aggregate and decides again.
	ErrVersionConflict = fmt.Errorf("%w: aggregate version mismatch", ErrConflict)

	// ErrDuplicate is returned when a write would violate a uniqueness rule of the data rather than a stale version,
	// for example a second delivery with an existing idempotency key (PRD section 7.3,
	// hard rule 7) or a second installed model with the same name and version. The use case then loads the
	// existing row (for example GetByIdempotencyKey) instead of creating another.
	ErrDuplicate = fmt.Errorf("%w: duplicate key", ErrConflict)
)
