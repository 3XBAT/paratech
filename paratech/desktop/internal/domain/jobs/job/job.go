package job

import "paratech/desktop/internal/domain/shared"

// Status is the state of a job (PRD section 7.2). The set of values is mirrored by the CHECK constraint
// of jobs.status in docs/contracts/schema.sql; a test keeps both in sync.
//
// Values only: transition rules are added by TASK-008, not here.
type Status string

// Job statuses in the order of PRD section 7.2.
const (
	StatusQueued            Status = "QUEUED"
	StatusRunning           Status = "RUNNING"
	StatusCompleted         Status = "COMPLETED"
	StatusRetryWait         Status = "RETRY_WAIT"
	StatusCancelled         Status = "CANCELLED"
	StatusFailed            Status = "FAILED"
	StatusWaitingForNetwork Status = "WAITING_FOR_NETWORK"
	StatusWaitingForQuota   Status = "WAITING_FOR_QUOTA"
	StatusWaitingForPower   Status = "WAITING_FOR_POWER"
)

// Statuses returns every status, in the order of PRD section 7.2.
func Statuses() []Status {
	return []Status{
		StatusQueued, StatusRunning, StatusCompleted, StatusRetryWait, StatusCancelled,
		StatusFailed, StatusWaitingForNetwork, StatusWaitingForQuota, StatusWaitingForPower,
	}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether no transition leaves the status (PRD section 7.2): COMPLETED, FAILED and CANCELLED.
// This is a classification of the enumeration, not a transition rule; the transition table arrives with TASK-008.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// ActiveStatuses returns the statuses of unfinished jobs, those that are not terminal: QUEUED, RUNNING, RETRY_WAIT
// and the three WAITING_* statuses. Use cases and repositories use it wherever "an unfinished job" matters, for
// example the check that a model is still pinned (PRD section 14 case 11), so the notion lives in the domain
// and not in an adapter query.
func ActiveStatuses() []Status {
	var out []Status
	for _, s := range Statuses() {
		if !s.IsTerminal() {
			out = append(out, s)
		}
	}
	return out
}

// Type names the kind of work a job performs and selects its handler, for example "stt" (TASK-029),
// "summarize" and "extract_homework" (TASK-042). The set is open: the task that adds a handler defines
// its type name.
type Type string

// Job is a durable record of work that must be completed; the worker is ephemeral, the job is durable
// (PRD section 7.1). Skeleton: see the package comment.
type Job struct {
	id     shared.JobID
	typ    Type
	status Status
}

// ID returns the job identifier.
func (j *Job) ID() shared.JobID { return j.id }

// Type returns the job type.
func (j *Job) Type() Type { return j.typ }

// Status returns the current status.
func (j *Job) Status() Status { return j.status }
