package shared

import "errors"

// Base errors that cross port boundaries. Adapters translate driver errors into these values
// (ARCH-32) and use cases check them with errors.Is (ARCH-18).
var (
	// ErrNotFound means the requested aggregate or artifact does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict means a uniqueness or idempotency constraint rejected a write, for example a second
	// delivery with the same idempotency key (PRD section 7.3, hard rule 7).
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
	// the artifact root (PRD section 5.2).
	ErrInvalidArtifactRef = errors.New("invalid artifact reference")
)
