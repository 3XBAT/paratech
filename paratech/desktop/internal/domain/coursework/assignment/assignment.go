package assignment

import "paratech/desktop/internal/domain/shared"

// Assignment is a homework item (PRD section 10). Skeleton: see the package comment.
type Assignment struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.AssignmentID
}

// ID returns the assignment identifier.
func (a *Assignment) ID() shared.AssignmentID { return a.id }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (a *Assignment) Version() int64 { return a.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (a *Assignment) Persisted(version int64) { a.version = version }
