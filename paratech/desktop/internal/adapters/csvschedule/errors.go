package csvschedule

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Typed errors of a file that cannot be read (ARCH-32). Each *Error also matches importplan.ErrUnreadableFile.
var (
	// ErrTooLarge means the file is larger than MaxFileBytes.
	ErrTooLarge = errors.New("file is too large")

	// ErrEncoding means the file is not valid UTF-8.
	ErrEncoding = errors.New("file is not UTF-8 text")

	// ErrHeader means the header line is missing, lacks a required column or repeats one.
	ErrHeader = errors.New("bad header")

	// ErrMalformed means the CSV syntax is broken (for example an unbalanced quote), so later rows cannot be trusted.
	ErrMalformed = errors.New("malformed CSV")

	// ErrTooMany means the file has more than importplan.MaxRows rows.
	ErrTooMany = errors.New("too many rows")
)

// Error is a file-level failure. Detail never quotes cell values.
type Error struct {
	Kind   error
	Line   int // 0 when not tied to a line
	Detail string
}

// Error implements error.
func (e *Error) Error() string {
	msg := "csvschedule: " + e.Kind.Error()
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
