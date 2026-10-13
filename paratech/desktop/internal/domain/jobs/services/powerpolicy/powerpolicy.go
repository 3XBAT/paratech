package powerpolicy

import (
	"errors"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
)

// ErrPowerLost is what the progress report of a heavy job returns when AC power is gone and the job must wait: the
// progress is saved, and the handler stops at this checkpoint and returns the error. Handlers live in other use cases
// and cannot import the job runner (ARCH-20), so the marker is part of the domain.
var ErrPowerLost = errors.New("AC power lost: stop at this checkpoint")

// Source is the power source as far as it is known.
type Source int

// Power sources. The zero value is unknown, which counts as AC power (PRD section 7.4).
const (
	SourceUnknown Source = iota
	SourceAC
	SourceBattery
)

// SourceOf maps the answer of a power monitor to a source.
func SourceOf(onAC bool) Source {
	if onAC {
		return SourceAC
	}
	return SourceBattery
}

// Conditions are the inputs of a decision at one moment.
type Conditions struct {
	OnlyOnACPower bool // the user setting process_only_on_ac_power
	Source        Source
}

// Restricted reports whether heavy work must wait: the setting is on and the computer is known to run on battery.
func (c Conditions) Restricted() bool { return c.OnlyOnACPower && c.Source == SourceBattery }

// Class is the weight of a job type.
type Class int

// Classes of job types.
const (
	ClassLight Class = iota // LLM requests, email, calendar sync: never restricted
	ClassHeavy              // local CPU-bound work such as STT
)

// Decision is the answer of the policy for one job.
type Decision int

// Decisions.
const (
	Allow Decision = iota
	Wait
)

// Decide is the whole rule: only a heavy job without the "Run now" override waits, and only under restriction.
func Decide(class Class, c Conditions, override bool) Decision {
	if class == ClassHeavy && !override && c.Restricted() {
		return Wait
	}
	return Allow
}

// Policy applies Decide to the job types of one runner.
type Policy struct {
	types      map[job.Type]Class
	open       job.ClaimFilter // heavy types allowed
	restricted job.ClaimFilter // heavy types only with run_now
}

// New builds a policy for the claimable types, of which heavy are subject to the power restriction. The lists follow
// the rules of job.NewClaimFilter.
func New(types, heavy []job.Type) (Policy, error) {
	open, err := job.NewClaimFilter(types, heavy, true)
	if err != nil {
		return Policy{}, err
	}
	restricted, err := job.NewClaimFilter(types, heavy, false)
	if err != nil {
		return Policy{}, err
	}

	classes := make(map[job.Type]Class, len(types))
	for _, t := range types {
		classes[t] = ClassLight
	}
	for _, t := range heavy {
		classes[t] = ClassHeavy
	}
	return Policy{types: classes, open: open, restricted: restricted}, nil
}

// ClassOf returns the class of a type; an unknown type is light.
func (p Policy) ClassOf(t job.Type) Class { return p.types[t] }

// Decide applies the rule to a job of type t.
func (p Policy) Decide(t job.Type, c Conditions, override bool) Decision {
	return Decide(p.ClassOf(t), c, override)
}

// Filter returns which QUEUED jobs may be claimed under c (ADR-0004: run_now is admitted by the filter itself).
func (p Policy) Filter(c Conditions) job.ClaimFilter {
	if c.Restricted() {
		return p.restricted
	}
	return p.open
}

// StartedByOverride reports whether a job of type t claimed with Filter(c) can only have been admitted by its
// "Run now" override. The claim clears run_now, so this is how the running attempt keeps the override.
func (p Policy) StartedByOverride(t job.Type, c Conditions) bool {
	return p.Decide(t, c, false) == Wait
}

// Park reports whether a QUEUED job moves to WAITING_FOR_POWER now: it is due, heavy, has no override and heavy
// work must wait (PRD section 7.2: QUEUED to WAITING_FOR_POWER). A job that is not due yet keeps its run time.
func (p Policy) Park(j *job.Job, c Conditions, now time.Time) bool {
	return j.Status() == job.StatusQueued && !j.NextRunAt().After(now) && p.Decide(j.Type(), c, j.RunNow()) == Wait
}

// Release reports whether a WAITING_FOR_POWER job of a known type goes back to QUEUED: AC power is back, the setting
// is off, or the type is light. Jobs of types this policy does not know are left for a runner that knows them.
func (p Policy) Release(j *job.Job, c Conditions) bool {
	_, known := p.types[j.Type()]
	return known && j.Status() == job.StatusWaitingForPower && p.Decide(j.Type(), c, false) == Allow
}
