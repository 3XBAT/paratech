package icsschedule

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// ErrInvalidOptions is returned by New without a clock.
var ErrInvalidOptions = errors.New("icsschedule: invalid options")

// Typed errors of a file that cannot be read (ARCH-32). Each *Error also matches importplan.ErrUnreadableFile.
var (
	// ErrTooLarge means the file is larger than MaxFileBytes.
	ErrTooLarge = errors.New("file is too large")

	// ErrEncoding means the file is not valid UTF-8.
	ErrEncoding = errors.New("file is not UTF-8 text")

	// ErrNotCalendar means the file is not one complete VCALENDAR (missing BEGIN or END, for example a cut download).
	ErrNotCalendar = errors.New("not an iCalendar file")

	// ErrMalformed means the iCalendar syntax is broken, so the events cannot be trusted.
	ErrMalformed = errors.New("malformed iCalendar")

	// ErrTooMany means more than MaxEvents events or more than importplan.MaxRows lessons in the import bounds.
	ErrTooMany = errors.New("too many events")

	// ErrTooComplex means the recurrence rules need more than MaxExpansionSteps steps to expand.
	ErrTooComplex = errors.New("recurrences are too long to expand")
)

// Error is a file-level failure. Detail never quotes values of the file.
type Error struct {
	Kind   error
	Line   int // 0 when not tied to a line
	Detail string
}

// Error implements error.
func (e *Error) Error() string {
	msg := "icsschedule: " + e.Kind.Error()
	if e.Line > 0 {
		msg += fmt.Sprintf(" at line %d", e.Line)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap lets errors.Is match both the kind and importplan.ErrUnreadableFile.
func (e *Error) Unwrap() []error { return []error{e.Kind, importplan.ErrUnreadableFile} }
