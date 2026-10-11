package sttmodel

import "paratech/desktop/internal/domain/shared"

// Model is an installed speech-to-text model (PRD section 8). Skeleton: see the package comment.
type Model struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.SttModelID
}

// ID returns the model identifier.
func (m *Model) ID() shared.SttModelID { return m.id }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (m *Model) Version() int64 { return m.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (m *Model) Persisted(version int64) { m.version = version }
