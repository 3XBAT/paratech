package assignment

import "time"

// MarkDone is «Выполнено» (ADR-0033 §9): the assignment is done from now on. A deleted assignment is not marked
// (ErrDeleted); marking a done one again changes nothing (changed false, the first time is kept). changed tells
// whether the assignment must be saved.
func (a *Assignment) MarkDone(now time.Time) (changed bool, err error) {
	if err := a.NotDeleted(); err != nil {
		return false, err
	}
	if a.Done() {
		return false, nil
	}
	// A clock set back must not store a mark that Restore(State) then refuses.
	at := now
	if at.Before(a.createdAt) {
		at = a.createdAt
	}
	a.doneAt, a.updatedAt = at, now
	return true, nil
}

// MarkUndone is «Вернуть в работу», the undo of MarkDone. A deleted assignment is not changed (ErrDeleted); one that
// is not done changes nothing (changed false).
func (a *Assignment) MarkUndone(now time.Time) (changed bool, err error) {
	if err := a.NotDeleted(); err != nil {
		return false, err
	}
	if !a.Done() {
		return false, nil
	}
	a.doneAt, a.updatedAt = time.Time{}, now
	return true, nil
}

// Done reports whether the student marked the assignment done.
func (a *Assignment) Done() bool { return !a.doneAt.IsZero() }

// DoneAt returns when the assignment was marked done, zero if it is not.
func (a *Assignment) DoneAt() time.Time { return a.doneAt }
