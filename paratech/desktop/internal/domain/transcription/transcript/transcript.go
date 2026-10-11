package transcript

import "paratech/desktop/internal/domain/shared"

// Transcript is the text result of transcribing one recording with a pinned model version
// (PRD section 8, AC-08). Skeleton: see the package comment.
type Transcript struct {
	version   int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id        shared.TranscriptID
	recording shared.RecordingID // reference by ID only (ARCH-13)
}

// ID returns the transcript identifier.
func (t *Transcript) ID() shared.TranscriptID { return t.id }

// RecordingID returns the recording this transcript was produced from.
func (t *Transcript) RecordingID() shared.RecordingID { return t.recording }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (t *Transcript) Version() int64 { return t.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (t *Transcript) Persisted(version int64) { t.version = version }
