package lesson

import "paratech/desktop/internal/domain/shared"

// Lesson is one schedule entry (PRD section 11). Skeleton: see the package comment.
type Lesson struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.LessonID
}

// ID returns the lesson identifier.
func (l *Lesson) ID() shared.LessonID { return l.id }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (l *Lesson) Version() int64 { return l.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (l *Lesson) Persisted(version int64) { l.version = version }
