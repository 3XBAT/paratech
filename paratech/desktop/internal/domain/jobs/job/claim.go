package job

import (
	"fmt"
	"strings"
	"unicode"
)

// ClaimFilter says which QUEUED jobs a worker may claim right now (PRD sections 7.2 and 7.4). It is a value object:
// the use case builds it from the registered handlers, the power state and the user settings, and the repository
// adapter translates it into its query. It carries no clock and no I/O (ARCH-15).
//
//   - types are the job types that have a registered handler; a job of any other type is never claimed;
//   - heavy is the subset of types that must not start while the computer runs on battery with power-aware
//     processing on, for example local STT (PRD section 7.4, AC-21);
//   - heavyAllowed is true when heavy types may be claimed (on AC power, or power-aware processing is off).
//
// The "Run now" override (AC-22) is a property of the job, not of the filter: a job with run_now set is claimable
// even when its heavy type is otherwise filtered out. Admits is the reference semantics of the whole rule; a
// repository adapter that filters in SQL must agree with it, and a fake repository can simply call it.
//
// This type only answers "may this job be considered"; the QUEUED to RUNNING transition itself belongs to the
// job aggregate (TASK-008).
type ClaimFilter struct {
	types        []Type
	heavy        []Type
	heavyAllowed bool
}

// NewClaimFilter validates and builds a filter. types must be non-empty without duplicates or blank names, and
// every heavy type must also be in types.
func NewClaimFilter(types, heavy []Type, heavyAllowed bool) (ClaimFilter, error) {
	if len(types) == 0 {
		return ClaimFilter{}, fmt.Errorf("%w: no job types", ErrInvalidClaimFilter)
	}
	known := make(map[Type]bool, len(types))
	for _, t := range types {
		if err := validateType(t); err != nil {
			return ClaimFilter{}, err
		}
		if known[t] {
			return ClaimFilter{}, fmt.Errorf("%w: duplicate type %q", ErrInvalidClaimFilter, t)
		}
		known[t] = true
	}
	seenHeavy := make(map[Type]bool, len(heavy))
	for _, t := range heavy {
		if !known[t] {
			return ClaimFilter{}, fmt.Errorf("%w: heavy type %q is not among the claimable types", ErrInvalidClaimFilter, t)
		}
		if seenHeavy[t] {
			return ClaimFilter{}, fmt.Errorf("%w: duplicate heavy type %q", ErrInvalidClaimFilter, t)
		}
		seenHeavy[t] = true
	}
	return ClaimFilter{
		types:        append([]Type(nil), types...),
		heavy:        append([]Type(nil), heavy...),
		heavyAllowed: heavyAllowed,
	}, nil
}

func validateType(t Type) error {
	if t == "" || strings.IndexFunc(string(t), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: blank or malformed type %q", ErrInvalidClaimFilter, string(t))
	}
	return nil
}

// Types returns a copy of the claimable job types.
func (f ClaimFilter) Types() []Type { return append([]Type(nil), f.types...) }

// HeavyTypes returns a copy of the types that are subject to the power restriction.
func (f ClaimFilter) HeavyTypes() []Type { return append([]Type(nil), f.heavy...) }

// HeavyAllowed reports whether heavy types may be claimed without the "Run now" override.
func (f ClaimFilter) HeavyAllowed() bool { return f.heavyAllowed }

// IsZero reports whether f is the zero value, which admits nothing.
func (f ClaimFilter) IsZero() bool { return len(f.types) == 0 }

// Admits reports whether a QUEUED job of type t, with the given run_now flag, may be claimed.
func (f ClaimFilter) Admits(t Type, runNow bool) bool {
	listed := false
	for _, x := range f.types {
		if x == t {
			listed = true
			break
		}
	}
	if !listed {
		return false
	}
	for _, x := range f.heavy {
		if x == t {
			return f.heavyAllowed || runNow
		}
	}
	return true
}
