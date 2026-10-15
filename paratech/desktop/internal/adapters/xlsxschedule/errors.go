package xlsxschedule

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Typed errors of a file that cannot be read (ARCH-32). Each *Error also matches importplan.ErrUnreadableFile.
var (
	// ErrTooLarge means the file, its unpacked size or its number of zip entries is over the limits.
	ErrTooLarge = errors.New("file is too large")

	// ErrNotWorkbook means the file is not an .xlsx workbook (for example an encrypted workbook or an old .xls).
	ErrNotWorkbook = errors.New("not an .xlsx workbook")

	// ErrMalformed means the workbook is damaged (bad zip entry, broken XML), so its rows cannot be trusted.
	ErrMalformed = errors.New("malformed workbook")

	// ErrHeader means the header row is missing, lacks a required column or repeats one.
	ErrHeader = errors.New("bad header")

	// ErrTooMany means the sheet has more than importplan.MaxRows lessons.
	ErrTooMany = errors.New("too many rows")
)

// Error is a file-level failure. Detail never quotes cell values.
type Error struct {
	Kind   error
	Line   int // sheet row; 0 when not tied to a row
	Detail string
}

// Error implements error.
func (e *Error) Error() string {
	msg := "xlsxschedule: " + e.Kind.Error()
	if e.Line > 0 {
		msg += fmt.Sprintf(" at row %d", e.Line)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap lets errors.Is match both the kind and importplan.ErrUnreadableFile.
func (e *Error) Unwrap() []error { return []error{e.Kind, importplan.ErrUnreadableFile} }
