package jobrunner

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// JobRepository is the durable job store (PRD sections 7.1 and 7.3): the worker is ephemeral, the job state is
// durable. Implemented by the sqlite adapter (TASK-008).
type JobRepository interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID
	// of the existing job is returned with created == false, so a repeated request never duplicates work
	// (PRD section 7.3, section 14 case 5, hard rule 7).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)

	// Get loads a job (PRD section 7.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.JobID) (*job.Job, error)

	// Save persists the full state of a job after an aggregate method changed it, including status,
	// attempts, next run time and progress (PRD section 7.3: persistent state transitions).
	Save(ctx context.Context, j *job.Job) error

	// Claim atomically takes the next job that is QUEUED and due at now, restricted to the given job types,
	// and returns it already moved to RUNNING. Two workers never receive the same job (PRD section 7.2).
	// ok is false when nothing is eligible.
	Claim(ctx context.Context, now time.Time, types []job.Type) (j *job.Job, ok bool, err error)

	// ListByStatus returns the jobs in any of the given statuses, for example stale RUNNING jobs at startup
	// recovery, or WAITING_FOR_POWER jobs when AC power returns (PRD sections 7.2 and 7.4).
	ListByStatus(ctx context.Context, statuses ...job.Status) ([]*job.Job, error)
}

// Handler executes jobs of one type; handlers are registered per job type when the app is wired
// (PRD section 7.1). It is implemented by other use cases (structurally, ARCH-20), for example transcription.
type Handler interface {
	// Handle performs the job and returns nil on success. It must honour cancellation of ctx and be
	// idempotent when the job has external side effects (PRD section 7.3, hard rule 7). How errors map to
	// statuses (transient, permanent, waiting for network, quota or power) is decided with TASK-009.
	Handle(ctx context.Context, j *job.Job) error
}

// PowerMonitor reports the power source of the computer (PRD sections 4.3 and 7.4, AC-21). Implemented by a Windows
// adapter (TASK-011) and by a fake elsewhere.
type PowerMonitor interface {
	// IsOnACPower reports whether the computer runs on AC power (PRD section 7.4). Where it cannot be determined,
	// such as a desktop without a battery, the adapter reports true.
	IsOnACPower(ctx context.Context) (bool, error)

	// Subscribe returns a channel that receives true when AC power is connected and false when it is lost
	// (PRD section 7.4: resume on AC power, stop at the next checkpoint when it is lost). The channel is closed when
	// ctx is cancelled.
	Subscribe(ctx context.Context) (<-chan bool, error)
}

// SettingsReader gives read access to the user settings, for example the "process only on AC power" switch
// (PRD section 7.4, AC-22). Implemented by the sqlite adapter.
type SettingsReader interface {
	// Load returns the current settings, defaults applied (PRD sections 7.4 and 15).
	Load(ctx context.Context) (*settings.Settings, error)
}

// Clock supplies the current time (ARCH-24), for backoff and next run times (PRD section 7.3). Implemented by
// the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 7.3).
	Now() time.Time
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary is one use case method call and it changes
// one aggregate. Implemented by the sqlite adapter (TASK-005).
type Tx interface {
	// Do runs fn in a database transaction and commits if it returns nil, otherwise it rolls back and returns
	// the error (PRD section 7.3: persistent state transitions). Repositories called with the context passed to
	// fn take part in the transaction; calls with another context do not.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
