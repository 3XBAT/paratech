package notifypolicy

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

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
		if t == "" || strings.ContainsFunc(string(t), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return Policy{}, fmt.Errorf("%w: blank or malformed job type %q", ErrInvalidPolicy, string(t))
		}
		set[t] = true
	}
	return Policy{types: set}, nil
}

// Notifies reports whether a job of type t that reached status s notifies the user; enabled is the user setting.
// The zero Policy notifies about nothing.
func (p Policy) Notifies(enabled bool, t job.Type, s job.Status) bool {
	return enabled && notice.Notifiable(s) && p.types[t]
}
