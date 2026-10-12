package backoff

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidPolicy is returned by New for a policy that would retry wrongly or forever.
var ErrInvalidPolicy = errors.New("invalid backoff policy")

// Spec is the configuration of a Policy.
type Spec struct {
	Base        time.Duration // delay after the first failure
	Factor      float64       // growth per further failure, at least 1
	MaxDelay    time.Duration // cap of one delay, at least Base
	MaxAttempts int           // at least 1: attempts after which a job is not retried
	Jitter      float64       // 0..1: a delay shrinks by up to this fraction; 0 means none
}

// DefaultSpec returns the policy for jobs without a special need.
func DefaultSpec() Spec {
	return Spec{Base: 5 * time.Second, Factor: 2, MaxDelay: 5 * time.Minute, MaxAttempts: 5, Jitter: 0.2}
}

// Policy decides how long to wait after a failed attempt and whether to try again at all.
type Policy struct{ spec Spec }

// New validates the spec and builds a policy.
func New(spec Spec) (Policy, error) {
	switch {
	case spec.Base <= 0:
		return Policy{}, fmt.Errorf("%w: base %v", ErrInvalidPolicy, spec.Base)
	case math.IsNaN(spec.Factor) || math.IsInf(spec.Factor, 0) || spec.Factor < 1:
		return Policy{}, fmt.Errorf("%w: factor %v", ErrInvalidPolicy, spec.Factor)
	case spec.MaxDelay < spec.Base:
		return Policy{}, fmt.Errorf("%w: max delay %v is below base %v", ErrInvalidPolicy, spec.MaxDelay, spec.Base)
	case spec.MaxAttempts < 1:
		return Policy{}, fmt.Errorf("%w: max attempts %d", ErrInvalidPolicy, spec.MaxAttempts)
	case math.IsNaN(spec.Jitter) || spec.Jitter < 0 || spec.Jitter > 1:
		return Policy{}, fmt.Errorf("%w: jitter %v is outside 0..1", ErrInvalidPolicy, spec.Jitter)
	}
	return Policy{spec: spec}, nil
}

// Spec returns the configuration of the policy.
func (p Policy) Spec() Spec { return p.spec }

// Delay is the wait after the n-th failed attempt (n starts at 1): Base * Factor^(n-1), capped at MaxDelay, then
// reduced by Jitter * unit. unit is a random number in [0, 1) supplied by the caller; values outside are clamped.
func (p Policy) Delay(failures int, unit float64) time.Duration {
	d := float64(p.spec.Base)
	limit := float64(p.spec.MaxDelay)
	for i := 1; i < failures && d < limit && p.spec.Factor > 1; i++ {
		d *= p.spec.Factor
	}
	d = math.Min(d, limit)
	if math.IsNaN(unit) {
		unit = 0
	}
	unit = math.Max(0, math.Min(unit, 1))
	return time.Duration(d * (1 - p.spec.Jitter*unit))
}

// Next decides after a failed attempt: attempts is the number of attempts started so far. retry is false once the
// attempts are used up, so a job never retries forever.
func (p Policy) Next(attempts int, unit float64) (delay time.Duration, retry bool) {
	if attempts >= p.spec.MaxAttempts {
		return 0, false
	}
	return p.Delay(attempts, unit), true
}
