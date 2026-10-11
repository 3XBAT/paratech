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
	id     shared.RecordingID
	status Status
}

// ID returns the recording identifier.
func (r *Recording) ID() shared.RecordingID { return r.id }

// Status returns the current lifecycle state.
func (r *Recording) Status() Status { return r.status }
