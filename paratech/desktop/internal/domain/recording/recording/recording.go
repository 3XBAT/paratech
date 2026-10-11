package recording

import "paratech/desktop/internal/domain/shared"

// Status is the lifecycle state of a recording (PRD section 6.2). The set of values is mirrored by the
// CHECK constraint of recordings.status in docs/contracts/schema.sql; a test keeps both in sync.
//
// Values only: transition rules are added by TASK-013, not here.
type Status string

// Recording statuses in the order of PRD section 6.2.
const (
	StatusStarting         Status = "STARTING"
	StatusRecording        Status = "RECORDING"
	StatusStopping         Status = "STOPPING"
	StatusFinalizing       Status = "FINALIZING"
	StatusCompleted        Status = "COMPLETED"
	StatusFailed           Status = "FAILED"
	StatusRecoveryRequired Status = "RECOVERY_REQUIRED"
)

// Statuses returns every status, in the order of PRD section 6.2.
func Statuses() []Status {
	return []Status{
		StatusStarting, StatusRecording, StatusStopping, StatusFinalizing,
		StatusCompleted, StatusFailed, StatusRecoveryRequired,
	}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// Recording is one capture of system audio of an online class (PRD section 6.2).
// Skeleton: see the package comment.
type Recording struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.RecordingID
	status  Status
}

// ID returns the recording identifier.
func (r *Recording) ID() shared.RecordingID { return r.id }

// Status returns the current lifecycle state.
func (r *Recording) Status() Status { return r.status }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (r *Recording) Version() int64 { return r.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (r *Recording) Persisted(version int64) { r.version = version }
