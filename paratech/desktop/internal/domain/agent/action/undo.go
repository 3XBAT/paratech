package action

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// maxIntentName bounds the name a create tool records in its intent.
const maxIntentName = 512

// Intent is what is known before the use case of an entry runs: the target it changes and the target's version then,
// the prior state Undo restores (Prior), the state the action leads to (Next) and the name a create tool makes.
// Recover compares the world with it after a crash between the two transactions (Settle). A zero Intent says nothing:
// the outcome then stays unknown.
type Intent struct {
	// Target is what the use case changes; zero when it creates it (create_subject) or starts a job. A create whose ID
	// is known before it runs (create_assignment, createsTarget) names that ID here, with Before 0: it did not exist.
	Target values.Ref
	Before int64        // version of Target before the action; 0 when Target is zero or not made yet
	Prior  Compensation // the compensation of the action as far as it is known before it runs; nil for creates
	// Next is the state the action leads to, written as the compensation that would restore it (set_lesson_info,
	// edit_due): only that state counts as the effect after a crash, not any change of the version.
	Next Compensation
	Name string // create_subject, create_folder: the name of what is made
}

// createsTarget are the create tools whose ID the desktop derives before the use case runs, so after a crash the
// target tells the outcome: there -> DONE, absent -> FAILED (ADR-0029).
var createsTarget = map[values.Tool]values.IDKind{values.ToolCreateAssignment: values.KindAssignment}

// IsZero reports whether the intent says nothing.
func (in Intent) IsZero() bool { return in == Intent{} }

func (in Intent) validate(t values.Tool) error {
	if in.IsZero() {
		return nil
	}
	bad := func(what string) error { return fmt.Errorf("%w: intent: %s", ErrInvalidAction, what) }
	if kind, ok := createsTarget[t]; ok {
		if in.Target.Kind() != kind || in.Before != 0 {
			return bad("a create names the id it makes, with no version before")
		}
	} else if in.Target.IsZero() != (in.Before == 0) || in.Before < 0 {
		return bad("a target exactly with its version before")
	}
	if len(in.Name) > maxIntentName || !utf8.ValidString(in.Name) {
		return bad("name too long or not UTF-8")
	}
	for _, c := range []Compensation{in.Prior, in.Next} {
		if c == nil {
			continue
		}
		if err := checkKind(t, c); err != nil {
			return err
		}
	}
	return nil
}

// Observation is what Recover saw of the target of a PENDING entry.
type Observation struct {
	Known        bool         // false: the effect cannot be told (a paid job, no intent)
	Version      int64        // current version of the target, or of what the create made
	Effect       bool         // the effect of the action is there
	Compensation Compensation // the compensation of the effect, when Effect
}

// Settle resolves a PENDING entry left by a crash between its two transactions: the effect is there -> DONE with its
// compensation; the target still has its version before (or a create made nothing) -> FAILED; else UNKNOWN (no Undo).
func (a *Action) Settle(o Observation, now time.Time) error {
	if a.status != StatusPending {
		return fmt.Errorf("%w: settle %s", ErrInvalidTransition, a.status)
	}
	switch {
	case !o.Known || a.intent.IsZero():
		return a.move(StatusUnknown, now)
	case o.Effect:
		if err := a.Complete(o.Compensation, o.Version, now); err != nil {
			return a.move(StatusUnknown, now)
		}
		return nil
	case a.intent.Target.IsZero() || o.Version == a.intent.Before:
		return a.Fail(now)
	}
	return a.move(StatusUnknown, now)
}

// Target is the state of the target of a compensation as the Compensator reads it.
type Target struct {
	Version     int64 // current version
	Compensated bool  // already in the state Undo leads to (a crash after the compensation, or the student did it)
	Final       bool  // a paid job finished: its result stays, the previous one is not kept (D7)
}

// UndoRefusal is why Undo was refused.
type UndoRefusal string

// Refusals of Undo.
const (
	UndoChanged     UndoRefusal = "changed"      // «Изменено после — отменить нельзя»
	UndoResultReady UndoRefusal = "result_ready" // the paid result is ready and the previous one is not kept
	UndoCannotStop  UndoRefusal = "cannot_stop"  // the job runs and cannot be stopped now
)

func (r UndoRefusal) valid() bool {
	return r == UndoChanged || r == UndoResultReady || r == UndoCannotStop
}

// UndoRefusalOf maps an error of the compensation to the refusal it means.
func UndoRefusalOf(err error) (UndoRefusal, bool) {
	switch {
	case errors.Is(err, ErrTargetChanged):
		return UndoChanged, true
	case errors.Is(err, ErrResultReady):
		return UndoResultReady, true
	case errors.Is(err, ErrCannotStop):
		return UndoCannotStop, true
	}
	return "", false
}

// PlanUndo decides the compensation of an UNDOING entry from the state of its target: nothing to apply when the
// target is already compensated; refused (an error UndoRefusalOf knows) when a paid result is final or the target
// changed since the action; else apply, compare-and-swap on TargetVersion.
func (a *Action) PlanUndo(t Target) (apply bool, err error) {
	if a.status != StatusUndoing || a.comp == nil {
		return false, fmt.Errorf("%w: plan undo in %s", ErrInvalidTransition, a.status)
	}
	switch {
	case t.Compensated:
		return false, nil
	case t.Final:
		return false, ErrResultReady
	case t.Version != a.targetVersion:
		return false, ErrTargetChanged
	}
	return true, nil
}

// NoUndo is why «Отменить» is not offered for an entry; empty when it is.
type NoUndo string

// Reasons.
const (
	NoUndoNone         NoUndo = ""
	NoUndoRunning      NoUndo = "running"      // PENDING or UNDOING
	NoUndoFailed       NoUndo = "failed"       // nothing changed
	NoUndoUnknown      NoUndo = "unknown"      // the outcome is not known
	NoUndoUndone       NoUndo = "undone"       // already undone
	NoUndoIrreversible NoUndo = "irreversible" // a proposal: «Отменить нельзя»
	NoUndoChanged      NoUndo = "changed"      // «Изменено после — отменить нельзя»
	NoUndoResultReady  NoUndo = "result_ready"
	NoUndoCannotStop   NoUndo = "cannot_stop"
)

// UndoNeedsTarget reports whether Undoable and CheckUndo depend on the state of the target: a DONE paid job, whose
// result may be ready by now. Other entries take the zero Target.
func (a *Action) UndoNeedsTarget() bool {
	_, job := a.comp.(CancelJob)
	return a.status == StatusDone && job
}

// CheckUndo is the check before StartUndo: ErrResultReady when the paid job of a DONE entry finished (t.Final), so
// «Отменить» is refused without touching the entry. A job that finishes after it is refused by PlanUndo.
func (a *Action) CheckUndo(t Target) error {
	if a.UndoNeedsTarget() && t.Final {
		return ErrResultReady
	}
	return nil
}

// Undoable reports whether «Отменить» is offered at now and, if not, why; t is the state of the target when
// UndoNeedsTarget, else the zero Target. A stale UNDOING entry is offered again (its retry, StartUndo).
func (a *Action) Undoable(now time.Time, t Target) (bool, NoUndo) {
	switch {
	case a.CheckUndo(t) != nil:
		return false, NoUndoResultReady
	case a.status == StatusDone, a.undoStale(now):
		return true, NoUndoNone
	}
	switch a.status {
	case StatusPending, StatusUndoing:
		return false, NoUndoRunning
	case StatusFailed:
		return false, NoUndoFailed
	case StatusUndone:
		return false, NoUndoUndone
	case StatusProposed:
		return false, NoUndoIrreversible
	case StatusUndoRefused:
		return false, NoUndo(a.refusal)
	}
	return false, NoUndoUnknown
}
