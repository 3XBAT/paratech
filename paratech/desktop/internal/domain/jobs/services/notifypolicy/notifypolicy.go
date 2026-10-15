package notifypolicy

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
)

// ErrInvalidPolicy is returned by New for an unusable set of job types (ARCH-18).
var ErrInvalidPolicy = errors.New("invalid notification policy")

// Policy knows which job types notify when they finish.
type Policy struct {
	types map[job.Type]bool
}

// New builds a policy for the given job types; at least one is required, each non-blank.
func New(types []job.Type) (Policy, error) {
	if len(types) == 0 {
		return Policy{}, fmt.Errorf("%w: no job types", ErrInvalidPolicy)
	}

	set := make(map[job.Type]bool, len(types))
	for _, t := range types {
		if err := notice.ValidateType(t); err != nil {
			return Policy{}, fmt.Errorf("%w: %w", ErrInvalidPolicy, err)
		}
		set[t] = true
	}
	return Policy{types: set}, nil
}

// Concerns reports whether a job of type t that reached status s notifies the user when the setting is on. It needs no
// setting, so callers check it first and read the settings only when it holds. The zero Policy concerns nothing.
func (p Policy) Concerns(t job.Type, s job.Status) bool {
	return notice.Notifiable(s) && p.types[t]
}

// Notifies reports whether a job of type t that reached status s notifies the user; enabled is the user setting.
func (p Policy) Notifies(enabled bool, t job.Type, s job.Status) bool {
	return enabled && p.Concerns(t, s)
}
