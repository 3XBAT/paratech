package homeworkflow

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const (
	opMarkDone    = "homeworkflow: mark assignments done"
	opRestoreDone = "homeworkflow: restore the done marks"
)

// noVersion is the expected version of a mark that checks none (MarkDone).
const noVersion = -1

// MaxMarkDone bounds the assignments of one MarkDone or RestoreDone call (ADR-0033 §9: mark_assignments_done takes
// 1..50 ids).
const MaxMarkDone = 50

// DoneCode is the outcome of «Выполнено» or «Вернуть в работу» for one assignment.
type DoneCode string

// The outcomes of one assignment.
const (
	DoneChanged   DoneCode = "changed"   // marked (or unmarked) and saved
	DoneUnchanged DoneCode = "unchanged" // already in that state: nothing saved
	DoneNotFound  DoneCode = "not_found" // no such assignment (a malformed id too)
	DoneDeleted   DoneCode = "deleted"   // deleted (ADR-0029): not marked
	DoneConflict  DoneCode = "conflict"  // changed meanwhile (another version): nothing saved
	DoneFailed    DoneCode = "failed"    // the storage failed: nothing saved
)

// DoneOutcome is what happened to one assignment. Done and Version are the stored state after the call for
// DoneChanged and DoneUnchanged; Err is the cause of the other codes (errors.Is: shared.ErrNotFound,
// assignment.ErrDeleted, shared.ErrVersionConflict, a storage error), nil for those two.
type DoneOutcome struct {
	ID      shared.AssignmentID
	Code    DoneCode
	Done    bool
	Version int64
	Err     error
}

// DonePrior is the compensation of one changed assignment: Done is the state before the call, which RestoreDone puts
// back, and Version the version the call saved, which RestoreDone expects.
type DonePrior struct {
	ID      shared.AssignmentID
	Done    bool
	Version int64
}

// DoneMarks is the result of MarkDone and RestoreDone: one outcome per distinct id in the order given, and the prior
// state of exactly the changed ones (none: nothing to undo).
type DoneMarks struct {
	Outcomes []DoneOutcome
	Prior    []DonePrior
}

// MarkDone is «Выполнено» (done) or «Вернуть в работу» (not done) of up to MaxMarkDone assignments (ADR-0033 §9), one
// transaction per assignment: a missing or deleted one gets its code and the others go on. An id given twice is
// handled once. Marking one already in that state changes nothing (DoneUnchanged, idempotent), so a repeated call has
// no Prior. Nothing is sent to Google Calendar and the deadline events are not touched (ADR-0033 О-13 is open).
// Reprocessing does not change the mark and neither overwrites nor withdraws a done assignment (assignment.Reextract,
// assignment.Superseded); the mark also survives SetHints, Edit and Correct.
// An empty list or more than MaxMarkDone ids is ErrInvalidRequest and nothing changes.
func (s *Service) MarkDone(ctx context.Context, ids []shared.AssignmentID, done bool) (DoneMarks, error) {
	if len(ids) == 0 || len(ids) > MaxMarkDone {
		return DoneMarks{}, fmt.Errorf("%s: %w: %d assignments, want 1..%d", opMarkDone, ErrInvalidRequest, len(ids), MaxMarkDone)
	}
	out := DoneMarks{Outcomes: []DoneOutcome{}, Prior: []DonePrior{}}
	seen := make(map[shared.AssignmentID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out.add(s.setDone(ctx, id, done, noVersion, opMarkDone))
	}
	return out, nil
}

// RestoreDone is the compensation of MarkDone («Отменить»): every entry of prior gets back its Done state if the
// assignment is still at the Version the mark saved, otherwise it is DoneConflict and stays as it is; a missing or
// deleted one gets its code; the others go on. Its Prior undoes the restore again. An empty prior changes nothing; more
// than MaxMarkDone entries is ErrInvalidRequest. An assignment restored to done gets the time of the restore as its
// done time: the domain has no way to put back an earlier one, and it only shows as «Выполнено <дата>».
func (s *Service) RestoreDone(ctx context.Context, prior []DonePrior) (DoneMarks, error) {
	if len(prior) > MaxMarkDone {
		return DoneMarks{}, fmt.Errorf("%s: %w: %d assignments, want at most %d", opRestoreDone, ErrInvalidRequest, len(prior), MaxMarkDone)
	}
	out := DoneMarks{Outcomes: []DoneOutcome{}, Prior: []DonePrior{}}
	seen := make(map[shared.AssignmentID]bool, len(prior))
	for _, p := range prior {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out.add(s.setDone(ctx, p.ID, p.Done, p.Version, opRestoreDone))
	}
	return out, nil
}

// add records one outcome and, for a changed assignment, its prior state.
func (m *DoneMarks) add(o DoneOutcome) {
	m.Outcomes = append(m.Outcomes, o)
	if o.Code == DoneChanged {
		m.Prior = append(m.Prior, DonePrior{ID: o.ID, Done: !o.Done, Version: o.Version})
	}
}

// setDone marks one assignment done or not in its own transaction, checking expected unless it is noVersion.
func (s *Service) setDone(ctx context.Context, id shared.AssignmentID, done bool, expected int64, op string) DoneOutcome {
	out := DoneOutcome{ID: id}
	if err := id.Validate(); err != nil {
		out.Code, out.Err = DoneNotFound, fmt.Errorf("%s %q: %w: %w", op, id, shared.ErrNotFound, err)
		return out
	}
	changed := false
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		a, err := s.assignments.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := a.NotDeleted(); err != nil {
			return err
		}
		if expected != noVersion && a.Version() != expected {
			return shared.ErrVersionConflict
		}
		now := s.clock.Now()
		if done {
			changed, err = a.MarkDone(now)
		} else {
			changed, err = a.MarkUndone(now)
		}
		if err != nil {
			return err
		}
		if changed {
			if err := s.assignments.Save(ctx, a); err != nil {
				return err
			}
		}
		out.Done, out.Version = a.Done(), a.Version()
		return nil
	})
	if err != nil {
		out.Code, out.Done, out.Version, out.Err = doneCodeOf(err), false, 0, fmt.Errorf("%s %s: %w", op, id, err)
		return out
	}
	out.Code = DoneUnchanged
	if changed {
		out.Code = DoneChanged
	}
	return out
}

// doneCodeOf is the code of a failed mark.
func doneCodeOf(err error) DoneCode {
	switch {
	case errors.Is(err, assignment.ErrDeleted):
		return DoneDeleted
	case errors.Is(err, shared.ErrNotFound):
		return DoneNotFound
	case errors.Is(err, shared.ErrVersionConflict):
		return DoneConflict
	default:
		return DoneFailed
	}
}
