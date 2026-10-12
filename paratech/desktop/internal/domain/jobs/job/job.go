package job

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Status is the state of a job (PRD section 7.2), mirrored by the CHECK of jobs.status; a test keeps both in sync.
//
// Allowed transitions are the table in transitions.go.
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

// ActiveStatuses returns the non-terminal statuses, for example for the check that a model is still pinned.
func ActiveStatuses() []Status {
	var out []Status
	for _, s := range Statuses() {
		if !s.IsTerminal() {
			out = append(out, s)
		}
	}
	return out
}

// Type names the kind of work a job performs and selects its handler, for example "stt" or "summarize".
// The set is open: the task that adds a handler defines its type name.
type Type string

// Limits of free-form fields.
const (
	MaxKeyBytes     = 256
	MaxPayloadBytes = 64 * 1024
	MaxErrorBytes   = 1024
)

// msgAttemptsExhausted prefixes the last error of a job that failed for lack of attempts.
const msgAttemptsExhausted = "attempts exhausted: "

// Job is a durable record of work that must be completed; the worker is ephemeral, the job is durable
// (PRD section 7.1). Fields change only through the methods below.
type Job struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.JobID
	typ     Type
	status  Status
	key     string
	payload string
	pinned  shared.SttModelID

	attempts    int
	maxAttempts int
	nextRunAt   time.Time
	runNow      bool
	progress    int
	checkpoint  string
	lastError   string

	createdAt  time.Time
	startedAt  time.Time
	finishedAt time.Time
	updatedAt  time.Time
}

// Spec is what the creator of a job provides to New.
type Spec struct {
	ID            shared.JobID
	Type          Type
	Key           string            // idempotency key: one key is one piece of work for ever
	Payload       string            // JSON with ids and pinned versions only; empty means {}
	PinnedModelID shared.SttModelID // optional
	MaxAttempts   int               // at least 1
	RunAt         time.Time         // first eligible time; zero means now
	RunNow        bool
}

// New creates a QUEUED job.
func New(spec Spec, now time.Time) (*Job, error) {
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidJob)
	}
	runAt := spec.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	payload := spec.Payload
	if payload == "" {
		payload = "{}"
	}
	j := &Job{
		id: spec.ID, typ: spec.Type, status: StatusQueued, key: spec.Key, payload: payload, pinned: spec.PinnedModelID,
		maxAttempts: spec.MaxAttempts, nextRunAt: runAt, runNow: spec.RunNow,
		createdAt: now, updatedAt: now,
	}
	if err := j.validate(); err != nil {
		return nil, err
	}
	return j, nil
}

// State is the stored form of a job, for Restore.
type State struct {
	Version       int64
	ID            shared.JobID
	Type          Type
	Status        Status
	Key           string
	Payload       string
	PinnedModelID shared.SttModelID
	Attempts      int
	MaxAttempts   int
	NextRunAt     time.Time
	RunNow        bool
	Percent       int
	Checkpoint    string
	LastError     string
	CreatedAt     time.Time
	StartedAt     time.Time // zero = never started
	FinishedAt    time.Time // zero = not finished
	UpdatedAt     time.Time
}

// Restore rebuilds a stored job without side effects; it only checks that the state is consistent.
func Restore(s State) (*Job, error) {
	j := &Job{
		version: s.Version, id: s.ID, typ: s.Type, status: s.Status, key: s.Key, payload: s.Payload, pinned: s.PinnedModelID,
		attempts: s.Attempts, maxAttempts: s.MaxAttempts, nextRunAt: s.NextRunAt, runNow: s.RunNow,
		progress: s.Percent, checkpoint: s.Checkpoint, lastError: s.LastError,
		createdAt: s.CreatedAt, startedAt: s.StartedAt, finishedAt: s.FinishedAt, updatedAt: s.UpdatedAt,
	}
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidJob, s.Version)
	}
	if !s.Status.Valid() {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidJob, s.Status)
	}
	if s.Attempts < 0 || s.Attempts > s.MaxAttempts {
		return nil, fmt.Errorf("%w: attempts %d of %d", ErrInvalidJob, s.Attempts, s.MaxAttempts)
	}
	if s.Status.IsTerminal() && s.FinishedAt.IsZero() {
		return nil, fmt.Errorf("%w: %s job without finish time", ErrInvalidJob, s.Status)
	}
	if !s.Status.IsTerminal() && !s.FinishedAt.IsZero() {
		return nil, fmt.Errorf("%w: %s job with finish time", ErrInvalidJob, s.Status)
	}
	if s.Status == StatusRunning && s.StartedAt.IsZero() {
		return nil, fmt.Errorf("%w: RUNNING job without start time", ErrInvalidJob)
	}
	if _, err := NewProgress(s.Percent, s.Checkpoint); err != nil {
		return nil, err
	}
	if err := j.validate(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Job) validate() error {
	if err := j.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidJob, err)
	}
	if !validName(string(j.typ)) {
		return fmt.Errorf("%w: blank or malformed type %q", ErrInvalidJob, string(j.typ))
	}
	if j.key == "" || len(j.key) > MaxKeyBytes || !utf8.ValidString(j.key) || strings.ContainsFunc(j.key, unicode.IsControl) {
		return fmt.Errorf("%w: malformed idempotency key", ErrInvalidJob)
	}
	if len(j.payload) > MaxPayloadBytes || !json.Valid([]byte(j.payload)) {
		return fmt.Errorf("%w: payload is not valid JSON of at most %d bytes", ErrInvalidJob, MaxPayloadBytes)
	}
	if j.pinned != "" {
		if err := j.pinned.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidJob, err)
		}
	}
	if j.maxAttempts < 1 {
		return fmt.Errorf("%w: max attempts %d", ErrInvalidJob, j.maxAttempts)
	}
	if j.nextRunAt.IsZero() || j.createdAt.IsZero() || j.updatedAt.IsZero() {
		return fmt.Errorf("%w: missing time", ErrInvalidJob)
	}
	return nil
}

// ID returns the job identifier.
func (j *Job) ID() shared.JobID { return j.id }

// Type returns the job type.
func (j *Job) Type() Type { return j.typ }

// Status returns the current status.
func (j *Job) Status() Status { return j.status }

// Key returns the idempotency key.
func (j *Job) Key() string { return j.key }

// Payload returns the JSON payload.
func (j *Job) Payload() string { return j.payload }

// PinnedModelID returns the STT model the job is pinned to, empty if none.
func (j *Job) PinnedModelID() shared.SttModelID { return j.pinned }

// Attempts returns the number of started attempts that still count.
func (j *Job) Attempts() int { return j.attempts }

// MaxAttempts returns the bound of attempts.
func (j *Job) MaxAttempts() int { return j.maxAttempts }

// NextRunAt returns when the job becomes eligible (QUEUED) or ends its backoff (RETRY_WAIT).
func (j *Job) NextRunAt() time.Time { return j.nextRunAt }

// RunNow reports the "Run now" override of the power wait (PRD section 7.4).
func (j *Job) RunNow() bool { return j.runNow }

// Progress returns the last reported percentage and checkpoint.
func (j *Job) Progress() Progress { return Progress{percent: j.progress, checkpoint: j.checkpoint} }

// LastError returns the sanitized message of the last failure.
func (j *Job) LastError() string { return j.lastError }

// CreatedAt returns the creation time.
func (j *Job) CreatedAt() time.Time { return j.createdAt }

// StartedAt returns when the job was first claimed; zero if never.
func (j *Job) StartedAt() time.Time { return j.startedAt }

// FinishedAt returns when the job reached a terminal status; zero if not yet.
func (j *Job) FinishedAt() time.Time { return j.finishedAt }

// UpdatedAt returns the time of the last change.
func (j *Job) UpdatedAt() time.Time { return j.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (j *Job) Version() int64 { return j.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only (ADR-0004); use cases and handlers never call it.
func (j *Job) Persisted(version int64) { j.version = version }

// move applies one table transition.
func (j *Job) move(to Status, now time.Time) error {
	if !CanTransition(j.status, to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, j.status, to)
	}
	j.status = to
	j.updatedAt = now
	if to.IsTerminal() {
		j.finishedAt = now
	}
	return nil
}

// Claim moves a due QUEUED job to RUNNING and counts the attempt.
func (j *Job) Claim(now time.Time) error {
	if !CanTransition(j.status, StatusRunning) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, j.status, StatusRunning)
	}
	if j.nextRunAt.After(now) {
		return ErrNotDue
	}
	if j.attempts >= j.maxAttempts {
		return ErrAttemptsExhausted
	}
	if err := j.move(StatusRunning, now); err != nil {
		return err
	}
	j.attempts++
	j.runNow = false // the override covers one start; a later power wait must hold again
	if j.startedAt.IsZero() {
		j.startedAt = now
	}
	return nil
}

// ReportProgress stores progress of a RUNNING job.
func (j *Job) ReportProgress(p Progress, now time.Time) error {
	if j.status != StatusRunning {
		return fmt.Errorf("%w: progress of a %s job", ErrInvalidTransition, j.status)
	}
	j.progress, j.checkpoint, j.updatedAt = p.percent, p.checkpoint, now
	return nil
}

// Complete moves a RUNNING job to COMPLETED.
func (j *Job) Complete(now time.Time) error {
	if err := j.move(StatusCompleted, now); err != nil {
		return err
	}
	j.progress = 100
	j.lastError = ""
	return nil
}

// Retry records a transient failure of a RUNNING job: RETRY_WAIT until runAt, or FAILED when no attempt is left.
func (j *Job) Retry(now, runAt time.Time, msg string) error {
	to := StatusRetryWait
	if j.attempts >= j.maxAttempts {
		to = StatusFailed
	}
	if err := j.move(to, now); err != nil {
		return err
	}
	j.nextRunAt = runAt
	if runAt.Before(now) {
		j.nextRunAt = now
	}
	j.lastError = limitText(msg)
	if to == StatusFailed {
		j.lastError = limitText(msgAttemptsExhausted + msg)
	}
	return nil
}

// Fail moves a RUNNING job to FAILED after a permanent error.
func (j *Job) Fail(now time.Time, msg string) error {
	if err := j.move(StatusFailed, now); err != nil {
		return err
	}
	j.lastError = limitText(msg)
	return nil
}

// Cancel moves any non-terminal job to CANCELLED (user cancellation).
func (j *Job) Cancel(now time.Time) error { return j.move(StatusCancelled, now) }

// WaitFor moves the job to one of the WAITING_FOR_* statuses. Waiting is not a failure: the attempt of a RUNNING
// job is given back.
func (j *Job) WaitFor(reason Status, now time.Time) error {
	if !reason.IsWaiting() {
		return fmt.Errorf("%w: %s is not a waiting status", ErrInvalidTransition, reason)
	}
	wasRunning := j.status == StatusRunning
	if err := j.move(reason, now); err != nil {
		return err
	}
	if wasRunning && j.attempts > 0 {
		j.attempts--
	}
	j.runNow = false // a power wait must hold again after the override
	return nil
}

// Requeue moves a RETRY_WAIT or WAITING_* job back to QUEUED, and also a stale RUNNING job at startup recovery;
// the crashed attempt counts, so a stale RUNNING job without attempts left becomes FAILED. A RETRY_WAIT job keeps its
// backoff time and is refused with ErrNotDue before it elapses; the others are due at once.
func (j *Job) Requeue(now time.Time) error {
	switch {
	case j.status == StatusRunning && j.attempts >= j.maxAttempts:
		if err := j.move(StatusFailed, now); err != nil {
			return err
		}
		j.lastError = msgAttemptsExhausted + "interrupted before it finished"
		return nil
	case j.status == StatusRetryWait:
		if j.nextRunAt.After(now) {
			return ErrNotDue
		}
		return j.move(StatusQueued, now)
	}
	if err := j.move(StatusQueued, now); err != nil {
		return err
	}
	j.nextRunAt = now
	return nil
}

// ForceRun sets the "Run now" override of the power wait: a WAITING_FOR_POWER job is requeued, a QUEUED job keeps its place.
func (j *Job) ForceRun(now time.Time) error {
	if j.status == StatusQueued {
		j.runNow, j.updatedAt = true, now
		return nil
	}
	if j.status != StatusWaitingForPower {
		return fmt.Errorf("%w: run now of a %s job", ErrInvalidTransition, j.status)
	}
	if err := j.Requeue(now); err != nil {
		return err
	}
	j.runNow = true
	return nil
}

// limitText bounds a message to MaxErrorBytes, cutting at a rune boundary.
func limitText(s string) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= MaxErrorBytes {
		return s
	}
	cut := MaxErrorBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
