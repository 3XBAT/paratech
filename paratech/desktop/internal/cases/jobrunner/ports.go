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
//
// Ownership (ARCH-14, ARCH-22, ADR-0004): from Claim until it leaves RUNNING, a job belongs to the jobrunner use
// case. Only the runner calls Save for a claimed job. A Handler never saves the job it works on; it reports
// progress through the callback it is given. Other use cases only Enqueue. This keeps one writer per running job,
// so progress writes and the final status transition cannot overwrite each other.
type JobRepository interface {
	// Enqueue stores a new job (Version() == 0) and, when it was created, calls Persisted(1) on it. If a job with the
	// same idempotency key exists, nothing is created, the aggregate stays at version 0 and the ID of the existing job
	// is returned with created == false, so a repeated request never duplicates work (PRD section 7.3, section 14
	// case 5, hard rule 7).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)

	// Get loads a job (PRD section 7.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.JobID) (*job.Job, error)

	// Save persists the full state of a job after an aggregate method changed it, including status, attempts,
	// next run time and progress (PRD section 7.3: persistent state transitions). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new
	// aggregate and is INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a
	// row that changed or was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the
	// adapter calls Persisted(newVersion) on the job, so the runner saves the same *job.Job repeatedly (one save per
	// progress report and a final one for the transition) without reloading. A stale job, for example one the user
	// cancelled meanwhile, yields shared.ErrVersionConflict (also errors.Is shared.ErrConflict); the runner then
	// reloads the job and decides again.
	Save(ctx context.Context, j *job.Job) error

	// Claim atomically takes the next job that is QUEUED, due at now and admitted by filter, and returns it already
	// moved to RUNNING. Two workers never receive the same job (PRD section 7.2). ok is false when nothing is eligible.
	//
	// The adapter does not set the status itself: it selects the candidate, restores the aggregate, calls the
	// domain transition QUEUED to RUNNING with now (the state machine lives in the aggregate, ARCH-14, ARCH-19, so
	// attempts and started_at follow its rules), and persists the result with a guarded
	// UPDATE ... WHERE id = ? AND status = 'QUEUED' AND version = ? and calls Persisted with the new version on the
	// returned job, so the runner can save it again at once. A raw "UPDATE jobs SET status = 'RUNNING'" is
	// not allowed. If the guarded update changes no row, another worker won the race and the adapter tries the next
	// candidate. filter.Admits is the reference semantics, including the "Run now" override: a run_now job is
	// claimable even when its heavy type is otherwise filtered out by the power state (PRD section 7.4, AC-22).
	Claim(ctx context.Context, now time.Time, filter job.ClaimFilter) (j *job.Job, ok bool, err error)

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
	//
	// snap is a read-only value copy of the claimed job (job.Snapshot): the handler reads from it what it needs, such
	// as the job type, and has no way to mutate or persist the aggregate, which the runner owns (see JobRepository);
	// TASK-008 adds the payload, pinned versions and checkpoint to the snapshot. report is the narrow
	// progress port: the handler calls it at safe points with the percentage and an opaque checkpoint (PRD
	// sections 7.3 and 7.4), and the runner applies them to the aggregate and saves with its version guard. report
	// is a plain function type on purpose, so that a handler in another use case satisfies this interface without
	// importing this package (ARCH-20). If report returns an error that wraps shared.ErrConflict (a version
	// conflict) or shared.ErrNotFound, the job was changed or removed elsewhere, for example cancelled: the handler
	// stops promptly and returns that error.
	Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, p job.Progress) error) error
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

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary
// is one use case method call and it changes one aggregate. The two documented exceptions (ADR-0004) are the batch
// import of LessonRepository.ApplyChanges and the deletion of a recording together with its transcripts
// (RecordingTranscripts.DeleteByRecording and RecordingRepository.Delete in one Tx.Do). Implemented by the sqlite
// adapter (TASK-005).
type Tx interface {
	// Do runs fn in one write transaction and commits if fn returns nil, otherwise it rolls back and returns the
	// error (PRD section 7.3: persistent state transitions). The sqlite adapter opens a write transaction up front
	// (BEGIN IMMEDIATE), so a read inside fn cannot be invalidated by another writer before the commit.
	// Repositories called with the context passed to fn take part in the transaction; calls with another context
	// do not. Nesting: a Do called with the context of a running fn joins the outer transaction (no savepoint) and
	// only the outermost Do commits or rolls back. fn performs no external I/O (network, e-mail, calendar, files,
	// subprocesses): it holds the database write lock and only reads and writes local aggregates.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
