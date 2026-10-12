package job

import "paratech/desktop/internal/domain/shared"

// Snapshot is an immutable copy of the state of a job at one moment. The job runner hands it to a Handler instead of
// the *Job it owns, so a handler can read but cannot mutate or save the aggregate (ADR-0004 job ownership, ARCH-14).
type Snapshot struct {
	id         shared.JobID
	typ        Type
	status     Status
	payload    string
	pinned     shared.SttModelID
	attempt    int
	checkpoint string
}

// Snapshot returns a read-only copy of the current state of the job.
func (j *Job) Snapshot() Snapshot {
	return Snapshot{
		id: j.id, typ: j.typ, status: j.status, payload: j.payload, pinned: j.pinned,
		attempt: j.attempts, checkpoint: j.checkpoint,
	}
}

// ID returns the identifier of the job.
func (s Snapshot) ID() shared.JobID { return s.id }

// Type returns the job type, which selected the handler.
func (s Snapshot) Type() Type { return s.typ }

// Status returns the status the job had when the snapshot was taken.
func (s Snapshot) Status() Status { return s.status }

// Payload returns the JSON payload of the job.
func (s Snapshot) Payload() string { return s.payload }

// PinnedModelID returns the pinned STT model, empty if none.
func (s Snapshot) PinnedModelID() shared.SttModelID { return s.pinned }

// Attempt returns the number of the current attempt, starting at 1 for a claimed job.
func (s Snapshot) Attempt() int { return s.attempt }

// Checkpoint returns the last stored resume information, empty if none.
func (s Snapshot) Checkpoint() string { return s.checkpoint }
