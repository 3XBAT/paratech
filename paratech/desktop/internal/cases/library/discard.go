package library

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opSubjectDiscarded = "library: subject removed with its notes and files"
	opSubjectRestored  = "library: removed subject restored"
	opExpiredPurge     = "library: purge of expired subjects failed for another subject"
)

// DiscardSubject removes a subject softly with its notes and every file filed under it, in any of its folders, while
// it is still at expectedVersion, the version the student saw (ADR-0027 «Removing a subject that has notes»). It is
// the student's click only: the agent's undo uses RemoveSubject, which never takes the notes. Only the subject is
// written (one aggregate, ARCH-23); the readers hide everything under it until RestoreSubject, and after
// subject.RestoreWindow it is purged. Errors: ErrInvalidRequest, ErrSubjectNotFound (also for a removed one),
// ErrSubjectChanged, ErrUnavailable.
func (s *Service) DiscardSubject(ctx context.Context, id shared.SubjectID, expectedVersion int64) (Discarded, error) {
	sub, err := s.subject(ctx, id)
	if err != nil {
		return Discarded{}, err
	}
	if sub.Version() != expectedVersion {
		return Discarded{}, ErrSubjectChanged
	}
	notes, files, err := s.contents(ctx, id)
	if err != nil {
		return Discarded{}, err
	}
	if _, err := sub.Remove(s.clock.Now()); err != nil {
		return Discarded{}, domainError(err)
	}
	if err := s.saveSeen(ctx, sub); err != nil {
		return Discarded{}, err
	}
	s.log.Info(opSubjectDiscarded, slog.String("subject", string(id)), slog.Int("notes", notes), slog.Int("files", files))
	return Discarded{Subject: sub, Notes: notes, Files: files}, nil
}

// RestoreSubject brings a removed subject back with everything under it, as it was, while it is still at
// expectedVersion (the version DiscardSubject gave). An active one is returned unchanged. Errors: ErrInvalidRequest,
// ErrSubjectNotFound (also once purged), ErrSubjectChanged, ErrRestoreExpired, ErrUnavailable.
func (s *Service) RestoreSubject(ctx context.Context, id shared.SubjectID, expectedVersion int64) (*subject.Subject, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	sub, err := s.subjects.GetAny(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrSubjectNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case sub.Version() != expectedVersion:
		return nil, ErrSubjectChanged
	}
	changed, err := sub.Restore(s.clock.Now())
	switch {
	case err != nil:
		return nil, domainError(err)
	case !changed:
		return sub, nil
	}
	if err := s.saveSeen(ctx, sub); err != nil {
		return nil, err
	}
	s.log.Info(opSubjectRestored, slog.String("subject", string(id)))
	return sub, nil
}

// RemovedSubject reads a removed subject for «Вернуть» where only its ID is known (a *RemovedNameError, a skipped
// schedule row): RestoreSubject takes its version. A missing or active one is ErrSubjectNotFound. Errors:
// ErrInvalidRequest, ErrSubjectNotFound, ErrUnavailable.
func (s *Service) RemovedSubject(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	sub, err := s.subjects.GetAny(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrSubjectNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case sub.Status() != subject.StatusRemoved:
		return nil, ErrSubjectNotFound
	}
	return sub, nil
}

// saveSeen saves a subject changed at the version the student saw: a save that lost is ErrSubjectChanged.
func (s *Service) saveSeen(ctx context.Context, sub *subject.Subject) error {
	switch err := s.subjects.Save(ctx, sub); {
	case errors.Is(err, shared.ErrVersionConflict):
		return ErrSubjectChanged
	case err != nil:
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// contents counts what goes with the subject id: its active notes, and its active files that are no part of an active
// note. A part of its own note goes with the note; a part of a note of another subject stays visible there (the
// readers keep it, see MaterialRepository), so neither is a file that goes. A part filed under another subject stays
// where it is and is not counted either.
func (s *Service) contents(ctx context.Context, id shared.SubjectID) (notes, files int, err error) {
	inNotes := map[shared.MaterialID]bool{}
	if s.notes != nil {
		// The notes of every subject: a few dozen subjects, read once per click of the student.
		subs, err := s.subjects.List(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		for _, sub := range subs {
			list, err := s.notes.ListBySubject(ctx, sub.ID())
			if err != nil {
				return 0, 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
			}
			for _, n := range list {
				if !n.Active() {
					continue
				}
				if sub.ID() == id {
					notes++
				}
				for _, p := range n.Parts() {
					inNotes[p] = true
				}
			}
		}
	}
	list, err := s.materials.ListIn(ctx, id)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	for _, m := range list {
		if m.Active() && !inNotes[m.ID()] {
			files++
		}
	}
	return notes, files, nil
}

// RemovedSubjects lists the removed subjects that can still be restored, the most recently removed first, each with
// when it is erased (subject.PurgeAt), for «Недавно удалённые». One past its window is left out: it waits for the
// purge and «Вернуть» refuses it. Error: ErrUnavailable.
func (s *Service) RemovedSubjects(ctx context.Context) ([]RemovedSubject, error) {
	list, err := s.subjects.ListRemoved(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	now := s.clock.Now()
	out := make([]RemovedSubject, 0, len(list))
	for _, sub := range list {
		if sub.PurgeDue(now) {
			continue
		}
		out = append(out, RemovedSubject{Subject: sub, RemovedAt: sub.RemovedAt(), PurgeAt: sub.PurgeAt()})
	}
	slices.SortFunc(out, func(a, b RemovedSubject) int {
		if c := b.RemovedAt.Compare(a.RemovedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Subject.ID(), b.Subject.ID())
	})
	return out, nil
}

// nameIndex is the index of the names and aliases of every subject, removed ones too, with the removed ones by ID: a
// removed subject keeps its name and aliases until it is purged, so that restoring it never makes a string ambiguous.
// expired holds the removed ones whose restore window had ended when the index was built.
type nameIndex struct {
	subjectmatch.Index
	removed map[shared.SubjectID]bool
	expired map[shared.SubjectID]bool
}

// holdsExpired reports whether a removed subject past its window, other than except, has the key of one of spellings.
func (ix nameIndex) holdsExpired(except shared.SubjectID, spellings []string) bool {
	for _, sp := range spellings {
		if !ix.TakenByOther(sp, except) {
			continue
		}
		if m, ok := ix.Resolve(sp); ok && ix.expired[m.ID] {
			return true
		}
	}
	return false
}

// namesFor builds the nameIndex for checking spellings as names or aliases of a subject other than except. When a
// removed subject past its restore window holds one of them, it is erased first (ExpiredSubjects) and the index is
// built again, so the name is free even before the daily purge (ADR-0027: after the purge the name is free). A purge
// that leaves that subject is ErrUnavailable with its error, never «удалён»: «Вернуть» would refuse it. Without the
// port the expired subject keeps holding the name until the daily purge.
func (s *Service) namesFor(ctx context.Context, except shared.SubjectID, spellings ...string) (nameIndex, error) {
	ix, err := s.names(ctx)
	if err != nil || s.expired == nil || !ix.holdsExpired(except, spellings) {
		return ix, err
	}
	_, perr := s.expired.Purge(ctx)
	if ix, err = s.names(ctx); err != nil {
		return nameIndex{}, err
	}
	if ix.holdsExpired(except, spellings) {
		if perr == nil {
			perr = errors.New("the purge left it")
		}
		return nameIndex{}, fmt.Errorf("%w: erase the expired subject holding the name: %w", ErrUnavailable, perr)
	}
	if perr != nil { // about other subjects: the one holding the name is gone
		s.log.Warn(opExpiredPurge, slog.Any("cause", perr))
	}
	return ix, nil
}

// purgeExpired erases sub when it is removed and past its restore window, through ExpiredSubjects; gone is true when
// it is no longer stored. Without the port nothing is erased.
func (s *Service) purgeExpired(ctx context.Context, sub *subject.Subject) (gone bool, err error) {
	if s.expired == nil || !sub.PurgeDue(s.clock.Now()) {
		return false, nil
	}
	_, perr := s.expired.Purge(ctx)
	switch _, err := s.subjects.GetAny(ctx, sub.ID()); {
	case errors.Is(err, shared.ErrNotFound):
		if perr != nil {
			s.log.Warn(opExpiredPurge, slog.Any("cause", perr))
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if perr == nil {
		perr = errors.New("the purge left it")
	}
	return false, fmt.Errorf("%w: erase the expired subject: %w", ErrUnavailable, perr)
}

// taken refuses name when a subject other than except has its key: a *RemovedNameError when that subject is removed,
// else ErrNameTaken.
func (ix nameIndex) taken(name string, except shared.SubjectID) error {
	if !ix.TakenByOther(name, except) {
		return nil
	}
	if m, ok := ix.Resolve(name); ok && ix.removed[m.ID] {
		return &RemovedNameError{Subject: m.ID}
	}
	return ErrNameTaken
}

// names builds the nameIndex of the library, for the checks of a new name or alias.
func (s *Service) names(ctx context.Context) (ix nameIndex, err error) {
	active, err := s.subjects.List(ctx)
	if err != nil {
		return nameIndex{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	gone, err := s.subjects.ListRemoved(ctx)
	if err != nil {
		return nameIndex{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	now := s.clock.Now()
	ix.removed = make(map[shared.SubjectID]bool, len(gone))
	ix.expired = map[shared.SubjectID]bool{}
	for _, g := range gone {
		ix.removed[g.ID()] = true
		if g.PurgeDue(now) {
			ix.expired[g.ID()] = true
		}
	}
	ix.Index = indexOf(slices.Concat(active, gone))
	return ix, nil
}
