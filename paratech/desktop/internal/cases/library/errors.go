package library

import "errors"

// Errors of the library (ARCH-18). Fixed texts without names or paths; causes go to the log.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("library: invalid configuration")

	// ErrInvalidRequest means a malformed id or semester.
	ErrInvalidRequest = errors.New("library: invalid request")

	// ErrInvalidName means a subject name or alias that is empty or too long.
	ErrInvalidName = errors.New("library: invalid subject name")

	// ErrInvalidFolderName means a folder name that is empty, too long or not a valid Windows file name.
	ErrInvalidFolderName = errors.New("library: invalid folder name")

	// ErrNameTaken means another subject already has the name or alias (case-insensitive, «ё» = «е»).
	ErrNameTaken = errors.New("library: subject name is taken")

	// ErrFolderTaken means the subject already has a folder with that name.
	ErrFolderTaken = errors.New("library: folder name is taken")

	// ErrSubjectNotFound means there is no such subject (any more).
	ErrSubjectNotFound = errors.New("library: subject not found")

	// ErrFolderNotFound means the subject has no such folder (any more).
	ErrFolderNotFound = errors.New("library: folder not found")

	// ErrMaterialNotFound means there is no such file (any more).
	ErrMaterialNotFound = errors.New("library: material not found")

	// ErrLessonNotFound means there is no such lesson in the schedule (any more).
	ErrLessonNotFound = errors.New("library: lesson not found")

	// ErrNotEmpty means a subject or folder that still holds files was to be removed.
	ErrNotEmpty = errors.New("library: not empty")

	// ErrSystemFolder means a system folder was to be removed.
	ErrSystemFolder = errors.New("library: system folder")

	// ErrTooMany means a limit of folders, aliases or lesson links is reached.
	ErrTooMany = errors.New("library: limit reached")

	// ErrMaterialRemoved means a change of a removed file other than restoring it.
	ErrMaterialRemoved = errors.New("library: material is removed")

	// ErrEmptyFile means an upload without content.
	ErrEmptyFile = errors.New("library: empty file")

	// ErrFileTooLarge means an upload larger than material.MaxBytes.
	ErrFileTooLarge = errors.New("library: file too large")

	// ErrUnsupportedFile means an upload that is not DOCX, PPTX, PDF or UTF-8 text.
	ErrUnsupportedFile = errors.New("library: unsupported file type")

	// ErrBusy means the subject or file kept changing while it was saved; trying again helps.
	ErrBusy = errors.New("library: busy")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("library: local storage failed")
)
