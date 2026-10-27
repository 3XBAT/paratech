package subject

import (
	"errors"
	"fmt"
)

// Errors of the subject aggregate (ARCH-18).
var (
	// ErrInvalidSubject means a state that breaks an invariant (a bad id, a missing system folder, ...).
	ErrInvalidSubject = errors.New("invalid subject")

	// ErrInvalidName means a subject name or alias that is empty or too long.
	ErrInvalidName = errors.New("invalid subject name")

	// ErrInvalidFolderName means a folder name that is empty, too long or not a valid Windows file name.
	ErrInvalidFolderName = errors.New("invalid folder name")

	// ErrFolderExists means another folder of the subject has the same name (case-insensitive, «ё» = «е»).
	ErrFolderExists = errors.New("folder name is taken")

	// ErrFolderNotFound means the subject has no folder with that id.
	ErrFolderNotFound = errors.New("folder not found")

	// ErrSystemFolder means a system folder was to be removed.
	ErrSystemFolder = errors.New("system folder cannot be removed")

	// ErrNotEmpty means a folder or subject that still holds files was to be removed.
	ErrNotEmpty = errors.New("not empty")

	// ErrTooMany means the subject has as many own folders or aliases as it may have.
	ErrTooMany = errors.New("too many folders or aliases")

	// ErrInvalidTeacher means a teacher name or address that breaks its rules; the error never holds the text.
	ErrInvalidTeacher = errors.New("invalid teacher")

	// ErrInvalidTeacherName means a bad teacher name; it wraps ErrInvalidTeacher.
	ErrInvalidTeacherName = fmt.Errorf("%w: name", ErrInvalidTeacher)

	// ErrInvalidTeacherEmail means a bad teacher address; it wraps ErrInvalidTeacher.
	ErrInvalidTeacherEmail = fmt.Errorf("%w: email", ErrInvalidTeacher)
)
