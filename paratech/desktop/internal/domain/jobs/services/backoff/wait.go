package backoff

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// WaitStatus returns the status a job waits in after an error that only the network or the quota coming back can fix
// (PRD sections 7.2 and 9.2): shared.ErrNetworkUnavailable gives WAITING_FOR_NETWORK, shared.ErrQuotaExceeded gives
// WAITING_FOR_QUOTA. ok is false for every other error, and for an error marked permanent, which still fails the job.
// Waiting is not a failure: the attempt is given back (job.WaitFor).
func WaitStatus(err error) (status job.Status, ok bool) {
	switch {
	case err == nil, errors.Is(err, ErrPermanent):
		return "", false
	case errors.Is(err, shared.ErrQuotaExceeded):
		return job.StatusWaitingForQuota, true
	case errors.Is(err, shared.ErrNetworkUnavailable):
		return job.StatusWaitingForNetwork, true
	}
	return "", false
}

// Rechecks says when a job waiting for the network or the quota is tried again. The desktop has no signal that either
// came back, so the job is requeued after a fixed wait; a try that still finds no network or quota waits again
// without using an attempt.
type Rechecks struct {
	Network time.Duration
	Quota   time.Duration
}

// DefaultRechecks returns the waits of the desktop: the network is checked every minute, the quota, which comes back
// when the tester's balance is topped up on the Hub, every 15 minutes.
func DefaultRechecks() Rechecks {
	return Rechecks{Network: time.Minute, Quota: 15 * time.Minute}
}

// Validate requires both waits to be positive.
func (r Rechecks) Validate() error {
	if r.Network <= 0 || r.Quota <= 0 {
		return fmt.Errorf("%w: recheck waits must be positive", ErrInvalidPolicy)
	}
	return nil
}

// Due reports whether a job that has waited in status since the time since is to be tried again at now. Statuses
// other than WAITING_FOR_NETWORK and WAITING_FOR_QUOTA are never due here: the power wait follows the power policy.
func (r Rechecks) Due(status job.Status, since, now time.Time) bool {
	var wait time.Duration
	switch status {
	case job.StatusWaitingForNetwork:
		wait = r.Network
	case job.StatusWaitingForQuota:
		wait = r.Quota
	default:
		return false
	}
	return !now.Before(since.Add(wait))
}
