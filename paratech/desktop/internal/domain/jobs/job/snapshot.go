package job

import "paratech/desktop/internal/domain/shared"

// Snapshot is an immutable copy of the state of a job at one moment. The job runner hands it to a Handler instead of
// the *Job it owns, so a handler can read what it needs but has no way to call a mutator or to save the aggregate
// (ADR-0004 job ownership, ARCH-14). It is a plain value: it has accessors only, no setters, and it does not follow
// later changes of the job it was taken from.
//
// Skeleton: it carries what the Job skeleton has (identity, type, status). TASK-008 adds the payload, the pinned
// model version, the attempt number and the last checkpoint together with the same fields of Job.
type Snapshot struct {
	id     shared.JobID
	typ    Type
	status Status
}

// Snapshot returns a read-only copy of the current state of the job.
func (j *Job) Snapshot() Snapshot {
	return Snapshot{id: j.id, typ: j.typ, status: j.status}
}

// ID returns the identifier of the job.
func (s Snapshot) ID() shared.JobID { return s.id }

// Type returns the job type, which selected the handler.
func (s Snapshot) Type() Type { return s.typ }

// Status returns the status the job had when the snapshot was taken.
func (s Snapshot) Status() Status { return s.status }
