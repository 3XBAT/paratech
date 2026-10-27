package note

import "errors"

// Errors of the note aggregate (ARCH-18).
var (
	// ErrInvalidNote means a state that breaks an invariant (a bad id, subject, status or time).
	ErrInvalidNote = errors.New("invalid note")

	// ErrInvalidAnchor means an anchor that breaks its rules: an unknown kind, a missing or extra id or date.
	ErrInvalidAnchor = errors.New("invalid note anchor")

	// ErrInvalidDate means a text that is not a real day in YYYY-MM-DD.
	ErrInvalidDate = errors.New("invalid note date")

	// ErrEmptyTitle means a title with nothing left after cleaning.
	ErrEmptyTitle = errors.New("empty note title")

	// ErrNoParts means a note, or a change of it, without any part.
	ErrNoParts = errors.New("note without parts")

	// ErrTooManyParts means more than MaxParts parts.
	ErrTooManyParts = errors.New("too many note parts")

	// ErrDuplicatePart means a material that is already a part of the note.
	ErrDuplicatePart = errors.New("material is already a part of the note")

	// ErrPartNotFound means a material that is not a part of the note.
	ErrPartNotFound = errors.New("material is not a part of the note")

	// ErrInvalidOrder means an order that is not exactly the parts of the note.
	ErrInvalidOrder = errors.New("order is not the parts of the note")

	// ErrRemoved means a change of a removed note other than Restore.
	ErrRemoved = errors.New("note is removed")

	// ErrInvalidTransition means a status change the table of transitions does not allow.
	ErrInvalidTransition = errors.New("invalid note status transition")
)
