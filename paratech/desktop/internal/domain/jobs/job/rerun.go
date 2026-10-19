package job

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Rerun returns a new QUEUED job that does the work of the finished job j again, under id and key: the same type,
// payload, pinned model and attempt bound, with fresh attempts (TASK-086: «Повторить», «Сделать конспект заново»).
// j itself is not changed, so a finished job stays finished and its key keeps naming its own work. An active job is
// refused with ErrStillActive: the work is already pending or running.
func (j *Job) Rerun(id shared.JobID, key string, now time.Time) (*Job, error) {
	if !j.status.IsTerminal() {
		return nil, fmt.Errorf("%w: %s", ErrStillActive, j.status)
	}
	return New(Spec{
		ID: id, Type: j.typ, Key: key, Payload: j.payload, PinnedModelID: j.pinned, MaxAttempts: j.maxAttempts,
	}, now)
}

// RetryFailed is Rerun for a job that FAILED or was CANCELLED; a COMPLETED one is refused with ErrNotFailed.
func (j *Job) RetryFailed(id shared.JobID, key string, now time.Time) (*Job, error) {
	if j.status == StatusCompleted {
		return nil, fmt.Errorf("%w: %s", ErrNotFailed, j.status)
	}
	return j.Rerun(id, key, now)
}
