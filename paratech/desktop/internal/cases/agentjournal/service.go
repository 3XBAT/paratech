package agentjournal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts.
const (
	opNew        = "agentjournal: new"
	opBegin      = "agentjournal: begin"
	opFinish     = "agentjournal: finish"
	opPropose    = "agentjournal: propose"
	opBeginUndo  = "agentjournal: begin undo"
	opFinishUndo = "agentjournal: finish undo"
	opList       = "agentjournal: list"
	opRecover    = "agentjournal: recover"
)

// MaxList bounds the entries List returns.
const MaxList = 200

// Service is the action journal. Each method is one short transaction on one entry (a single CAS save); the use case
// that acts runs between Begin and Finish, idempotent by Entry.Key (ADR-0021, ARCH-23).
type Service struct {
	actions ActionRepository
	comp    Compensator
	clock   Clock
	ids     IDGenerator
	log     *slog.Logger
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Actions == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%s: %w: Actions, Clock and IDs are required", opNew, ErrInvalidConfig)
	}
	s := &Service{actions: d.Actions, comp: d.Compensator, clock: d.Clock, ids: d.IDs, log: d.Log}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

func spec(id shared.ActionID, in BeginInput) action.Spec {
	return action.Spec{ID: id, Thread: in.Thread, Turn: in.Turn, CallID: in.CallID, Tool: in.Tool}
}

// Begin records a U call PENDING before its use case runs (first transaction). The use case then runs with
// Entry.Key as its idempotency key.
func (s *Service) Begin(ctx context.Context, in BeginInput) (Entry, error) {
	a, err := action.New(spec(s.ids.NewActionID(), in), s.clock.Now())
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	return entryOf(a), nil
}

// Finish records the outcome of the use case (second transaction): DONE with its compensation, or FAILED.
func (s *Service) Finish(ctx context.Context, id shared.ActionID, out Outcome) (Entry, error) {
	return s.update(ctx, id, opFinish, func(a *action.Action) error {
		if out.Failed {
			return a.Fail(s.clock.Now())
		}
		return a.Complete(out.Compensation, out.TargetVersion, s.clock.Now())
	})
}

// Propose records a C call (or a U call that would replace a ready result) that created a proposal or a preview:
// PROPOSED. Nothing ran; the student decides on the confirm card.
func (s *Service) Propose(ctx context.Context, in BeginInput, proposal values.Ref) (Entry, error) {
	a, err := action.NewProposed(spec(s.ids.NewActionID(), in), proposal, s.clock.Now())
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	return entryOf(a), nil
}

// BeginUndo is «Отменить»: DONE -> UNDOING, saved before the compensation runs.
func (s *Service) BeginUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, ErrUndoUnavailable)
	}
	return s.update(ctx, id, opBeginUndo, func(a *action.Action) error { return a.BeginUndo(s.clock.Now()) })
}

// FinishUndo runs the compensation of an UNDOING entry: refused (UNDO_REFUSED) when the target changed since the
// action, else applied (idempotent) and UNDONE. A target that no longer exists has nothing left to undo: UNDONE. A
// failed compensation leaves the entry UNDOING; Recover retries it at the next start.
func (s *Service) FinishUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, ErrUndoUnavailable)
	}
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	if err := s.compensate(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	return entryOf(a), nil
}

func (s *Service) compensate(ctx context.Context, a *action.Action) error {
	if a.Status() != action.StatusUndoing {
		return fmt.Errorf("%w: %s is not UNDOING", action.ErrInvalidTransition, a.Status())
	}
	if err := s.undo(ctx, a); err != nil {
		return err
	}
	return s.actions.Save(ctx, a)
}

// undo decides and applies the compensation of a; the caller saves the new status.
func (s *Service) undo(ctx context.Context, a *action.Action) error {
	c := a.Compensation()
	current, err := s.comp.TargetVersion(ctx, c)
	if errors.Is(err, shared.ErrNotFound) {
		return a.FinishUndo(s.clock.Now())
	}
	if err != nil {
		return err
	}
	err = a.CheckTarget(current)
	if errors.Is(err, action.ErrTargetChanged) {
		return a.RefuseUndo(s.clock.Now())
	}
	if err != nil {
		return err
	}
	if err := s.comp.Apply(ctx, c); err != nil {
		return err
	}
	return a.FinishUndo(s.clock.Now())
}

// Undo is BeginUndo and FinishUndo.
func (s *Service) Undo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if _, err := s.BeginUndo(ctx, id); err != nil {
		return Entry{}, err
	}
	return s.FinishUndo(ctx, id)
}

// List returns the entries of a thread, newest first, at most limit (1..MaxList).
func (s *Service) List(ctx context.Context, thread shared.ThreadID, limit int) ([]Entry, error) {
	if limit <= 0 || limit > MaxList {
		limit = MaxList
	}
	as, err := s.actions.ListByThread(ctx, thread, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	out := make([]Entry, len(as))
	for i, a := range as {
		out[i] = entryOf(a)
	}
	return out, nil
}

// Recover runs at start: a PENDING entry's outcome is not known (UNKNOWN, no Undo); an UNDOING one has its
// compensation retried.
func (s *Service) Recover(ctx context.Context) (RecoverReport, error) {
	var rep RecoverReport
	as, err := s.actions.ListUnfinished(ctx)
	if err != nil {
		return rep, fmt.Errorf("%s: %w", opRecover, err)
	}
	var errs []error
	for _, a := range as {
		retry, err := a.Recover(s.clock.Now())
		if err != nil {
			errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
			continue
		}
		if !retry {
			if err := s.actions.Save(ctx, a); err != nil {
				errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
				continue
			}
			rep.Unknown++
			continue
		}
		if s.comp == nil {
			rep.Left++
			continue
		}
		if err := s.compensate(ctx, a); err != nil {
			s.log.WarnContext(ctx, "agent undo retry failed", "action", a.ID().String(), "err", err)
			rep.Left++
			continue
		}
		if a.Status() == action.StatusUndone {
			rep.Undone++
		} else {
			rep.Refused++
		}
	}
	if len(errs) > 0 {
		return rep, fmt.Errorf("%s: %w", opRecover, errors.Join(errs...))
	}
	return rep, nil
}

// update loads an entry, applies change and saves it.
func (s *Service) update(ctx context.Context, id shared.ActionID, op string, change func(*action.Action) error) (Entry, error) {
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := change(a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	return entryOf(a), nil
}
