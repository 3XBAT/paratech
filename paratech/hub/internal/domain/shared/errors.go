package shared

import "errors"

// Base errors shared by contexts and repositories, checked with errors.Is (ARCH-18).
var (
	// ErrInvalidID means an identifier does not have the expected shape.
	ErrInvalidID = errors.New("invalid identifier")

	// ErrNotFound means a repository has no record with the requested identifier.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists means a repository already holds a record with the identifier being created.
	ErrAlreadyExists = errors.New("already exists")
)
