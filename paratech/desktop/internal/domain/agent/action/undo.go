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
// the prior state Undo restores (Prior) and the name a create tool makes. Recover compares the world with it after a
// crash between the two transactions (Settle). A zero Intent says nothing: the outcome then stays unknown.
type Intent struct {
	Target values.Ref   // what the use case changes; zero when it creates it (create_subject) or starts a job
	Before int64        // version of Target before the action; 0 when Target is zero
	Prior  Compensation // the compensation of the action as far as it is known before it runs; nil for creates
	Name   string       // create_subject, create_folder: the name of what is made
}

// IsZero reports whether the intent says nothing.
func (in Intent) IsZero() bool { return in == Intent{} }

func (in Intent) validate(t values.Tool) error {
	if in.IsZero() {
		return nil
	}
	bad := func(what string) error { return fmt.Errorf("%w: intent: %s", ErrInvalidAction, what) }
	if in.Target.IsZero() != (in.Before == 0) || in.Before < 0 {
		return bad("a target exactly with its version before")
	}
	if len(in.Name) > maxIntentName || !utf8.ValidString(in.Name) {
		return bad("name too long or not UTF-8")
	}
	if in.Prior != nil {
		if err := checkKind(t, in.Prior); err != nil {
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

// Undoable reports whether «Отменить» is offered and, if not, why.
func (a *Action) Undoable() (bool, NoUndo) {
	switch a.status {
	case StatusDone:
		return true, NoUndoNone
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
