package lesson

import "errors"

// Errors of the lesson aggregate (ARCH-18).
var (
	// ErrInvalidLesson is returned by New, Restore, ChangeContent and Content.Validate for input that breaks an
	// invariant: an empty or too long title, an end not after the start, a malformed time zone name or source uid.
	ErrInvalidLesson = errors.New("invalid lesson")

	// ErrDeleted means a change was requested on a soft-deleted lesson.
	ErrDeleted = errors.New("lesson is deleted")

	// ErrInvalidLead is returned for a start announcement lead time outside 0..MaxStartLead.
	ErrInvalidLead = errors.New("invalid start announcement lead time")

	// ErrAlreadyAnnounced means the "lesson is starting" prompt of this start was already raised.
	ErrAlreadyAnnounced = errors.New("lesson start is already announced")

	// ErrNotDue means the start announcement was requested outside its window [start-lead, start+StartLateGrace).
	ErrNotDue = errors.New("lesson start announcement is not due")
)
