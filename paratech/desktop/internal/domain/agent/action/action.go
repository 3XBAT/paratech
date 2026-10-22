package action

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Action is one journal entry of an agent call with an effect. Fields change only through its methods (ARCH-14).
type Action struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.ActionID
	thread  shared.ThreadID
	turn    int
	callID  string
	tool    values.Tool
	status  Status

	comp          Compensation // DONE and later: how to undo
	targetVersion int64        // DONE and later: version of the target right after the action
	proposal      values.Ref   // PROPOSED: the proposal or preview

	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of an entry provides.
type Spec struct {
	ID     shared.ActionID
	Thread shared.ThreadID
	Turn   int
	CallID string
	Tool   values.Tool
}

func (s Spec) validate() error {
	if err := s.ID.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAction, err)
	}
	if err := s.Thread.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAction, err)
	}
	if s.Turn < 1 || s.CallID == "" || len(s.CallID) > values.MaxCallIDBytes {
		return fmt.Errorf("%w: turn or call id out of range", ErrInvalidAction)
	}
	return nil
}

// New records a U call before its use case runs: PENDING.
func New(spec Spec, now time.Time) (*Action, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	if r, err := actionpolicy.Of(spec.Tool); err != nil || r.Class != actionpolicy.ClassU {
		return nil, fmt.Errorf("%w: only a U tool is journaled as an action", ErrInvalidAction)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no time", ErrInvalidAction)
	}
	return &Action{
		id: spec.ID, thread: spec.Thread, turn: spec.Turn, callID: spec.CallID, tool: spec.Tool,
		status: StatusPending, createdAt: now, updatedAt: now,
	}, nil
}

// NewProposed records a C call (or a U call that would replace a ready result) that created a proposal or a preview:
// PROPOSED. Nothing ran; the student decides on the card.
func NewProposed(spec Spec, proposal values.Ref, now time.Time) (*Action, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	if r, err := actionpolicy.Of(spec.Tool); err != nil || (r.Class != actionpolicy.ClassC && r.Class != actionpolicy.ClassU) {
		return nil, fmt.Errorf("%w: only a C tool, or a U tool replacing a ready result, is proposed", ErrInvalidAction)
	}
	if proposal.Kind() != values.KindProposal && proposal.Kind() != values.KindPreview {
		return nil, fmt.Errorf("%w: a proposed action refers to a proposal or a preview", ErrInvalidAction)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no time", ErrInvalidAction)
	}
	return &Action{
		id: spec.ID, thread: spec.Thread, turn: spec.Turn, callID: spec.CallID, tool: spec.Tool,
		status: StatusProposed, proposal: proposal, createdAt: now, updatedAt: now,
	}, nil
}

// State is the stored form of an action, for Restore.
type State struct {
	Version       int64
	ID            shared.ActionID
	Thread        shared.ThreadID
	Turn          int
	CallID        string
	Tool          values.Tool
	Status        Status
	Compensation  Compensation // nil unless DONE or later
	TargetVersion int64
	Proposal      values.Ref // zero unless PROPOSED
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Restore rebuilds a stored action without side effects and checks its invariants.
func Restore(s State) (*Action, error) {
	bad := func(what string) (*Action, error) { return nil, fmt.Errorf("%w: %s", ErrInvalidAction, what) }
	if s.Version < 1 {
		return bad("stored version must be at least 1")
	}
	if !s.Status.Valid() {
		return bad("unknown status")
	}
	var (
		a   *Action
		err error
	)
	spec := Spec{ID: s.ID, Thread: s.Thread, Turn: s.Turn, CallID: s.CallID, Tool: s.Tool}
	if s.Status == StatusProposed {
		a, err = NewProposed(spec, s.Proposal, s.CreatedAt)
	} else {
		a, err = New(spec, s.CreatedAt)
		if err == nil && !s.Proposal.IsZero() {
			return bad("a proposal without PROPOSED")
		}
	}
	if err != nil {
		return nil, err
	}
	undoable := s.Status == StatusDone || s.Status == StatusUndoing || s.Status == StatusUndone || s.Status == StatusUndoRefused
	if undoable {
		if err := checkCompensation(s.Tool, s.Compensation, s.TargetVersion); err != nil {
			return nil, err
		}
	} else if s.Compensation != nil || s.TargetVersion != 0 {
		return bad("a compensation exists only after DONE")
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return bad("updated before created")
	}
	a.version, a.status, a.comp, a.targetVersion, a.updatedAt = s.Version, s.Status, s.Compensation, s.TargetVersion, s.UpdatedAt
	return a, nil
}

// State returns the stored form of the action.
func (a *Action) State() State {
	return State{
		Version: a.version, ID: a.id, Thread: a.thread, Turn: a.turn, CallID: a.callID, Tool: a.tool,
		Status: a.status, Compensation: a.comp, TargetVersion: a.targetVersion, Proposal: a.proposal,
		CreatedAt: a.createdAt, UpdatedAt: a.updatedAt,
	}
}

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004).
func (a *Action) Persisted(version int64) { a.version = version }

// Version returns the persisted version: 0 for an entry that was never stored.
func (a *Action) Version() int64 { return a.version }

// ID returns the action ID.
func (a *Action) ID() shared.ActionID { return a.id }

// Thread returns the thread of the entry.
func (a *Action) Thread() shared.ThreadID { return a.thread }

// Turn returns the request number of the entry.
func (a *Action) Turn() int { return a.turn }

// CallID returns the call ID of the planner.
func (a *Action) CallID() string { return a.callID }

// CreatedAt returns when the entry was recorded.
func (a *Action) CreatedAt() time.Time { return a.createdAt }

// Status returns the status.
func (a *Action) Status() Status { return a.status }

// Tool returns the tool.
func (a *Action) Tool() values.Tool { return a.tool }

// Compensation returns how to undo a DONE action, or nil.
func (a *Action) Compensation() Compensation { return a.comp }

// Proposal returns the proposal or preview of a PROPOSED action.
func (a *Action) Proposal() values.Ref { return a.proposal }

// Key is the idempotency key of the use case run between PENDING and the outcome, namespaced by thread and action
// (not the call ID alone, which the planner could repeat in another thread).
func (a *Action) Key() string { return fmt.Sprintf("ag:%s:act:%s", a.thread, a.id) }

// CanUndo reports whether «Отменить» is offered.
func (a *Action) CanUndo() bool { return a.status == StatusDone }

func (a *Action) move(to Status, now time.Time) error {
	if !CanTransition(a.status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.status, to)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidAction)
	}
	a.status = to
	a.updatedAt = now
	return nil
}

func checkCompensation(t values.Tool, c Compensation, targetVersion int64) error {
	want, ok := compensationOf[t]
	if !ok || c == nil || c.Kind() != want {
		return fmt.Errorf("%w: %s needs %s", ErrInvalidCompensation, t, want)
	}
	if targetVersion < 1 {
		return fmt.Errorf("%w: no target version", ErrInvalidCompensation)
	}
	return c.validate()
}

// Complete records success: PENDING -> DONE with the compensation of the tool and the target version right after
// the action.
func (a *Action) Complete(c Compensation, targetVersion int64, now time.Time) error {
	if err := checkCompensation(a.tool, c, targetVersion); err != nil {
		return err
	}
	if err := a.move(StatusDone, now); err != nil {
		return err
	}
	a.comp, a.targetVersion = c, targetVersion
	return nil
}

// Fail records that the use case failed and changed nothing: PENDING -> FAILED.
func (a *Action) Fail(now time.Time) error { return a.move(StatusFailed, now) }

// Recover is startup recovery: PENDING -> UNKNOWN (no Undo). It returns true for UNDOING, whose compensation is
// retried; other statuses are left as they are.
func (a *Action) Recover(now time.Time) (retryUndo bool, err error) {
	switch a.status {
	case StatusPending:
		return false, a.move(StatusUnknown, now)
	case StatusUndoing:
		return true, nil
	}
	return false, nil
}

// BeginUndo is «Отменить»: DONE -> UNDOING, saved before the compensation runs.
func (a *Action) BeginUndo(now time.Time) error { return a.move(StatusUndoing, now) }

// CheckTarget compares the current version of the target with the one right after the action.
func (a *Action) CheckTarget(current int64) error {
	if a.comp == nil {
		return fmt.Errorf("%w: no compensation", ErrInvalidTransition)
	}
	if current != a.targetVersion {
		return ErrTargetChanged
	}
	return nil
}

// TargetVersion returns the version of the target right after the action (0 before DONE). The compensation applies
// only while the target still has it.
func (a *Action) TargetVersion() int64 { return a.targetVersion }

// FinishUndo records the applied compensation: UNDOING -> UNDONE.
func (a *Action) FinishUndo(now time.Time) error { return a.move(StatusUndone, now) }

// RefuseUndo records that the target changed since: UNDOING -> UNDO_REFUSED.
func (a *Action) RefuseUndo(now time.Time) error { return a.move(StatusUndoRefused, now) }
