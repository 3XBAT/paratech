package library

import (
	"errors"
	"fmt"
)

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

	// ErrInvalidTeacher means a teacher name or address that breaks its rules (subject.NewTeacher); the error never
	// holds the typed text. ErrInvalidTeacherName and ErrInvalidTeacherEmail tell the field and wrap it.
	ErrInvalidTeacher = errors.New("library: invalid teacher")

	// ErrInvalidTeacherName means a bad teacher name; it wraps ErrInvalidTeacher.
	ErrInvalidTeacherName = fmt.Errorf("%w: name", ErrInvalidTeacher)

	// ErrInvalidTeacherEmail means a bad teacher address; it wraps ErrInvalidTeacher.
	ErrInvalidTeacherEmail = fmt.Errorf("%w: email", ErrInvalidTeacher)

	// ErrSubjectChanged means the subject is no longer at the version the student saw: the page reloads it.
	ErrSubjectChanged = errors.New("library: subject changed")

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

	// ErrHasNotes means a subject that has notes of the student (active or removed) was to be removed; it wraps
	// ErrNotEmpty, so a caller that knows only that one still reads it as «not empty».
	ErrHasNotes = fmt.Errorf("%w: the subject has notes", ErrNotEmpty)

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

	// ErrImageTooLarge means a photo over material.MaxImageBytes or material.MaxImagePixels; it wraps ErrFileTooLarge.
	ErrImageTooLarge = fmt.Errorf("%w: photo over the limits", ErrFileTooLarge)

	// ErrUnsupportedImage means an image that is not JPEG or PNG (HEIC, GIF, WebP, BMP, TIFF, SVG); it wraps
	// ErrUnsupportedFile.
	ErrUnsupportedImage = fmt.Errorf("%w: only JPEG and PNG photos", ErrUnsupportedFile)

	// ErrImageUnreadable means a JPEG or PNG that does not decode (truncated, damaged); it wraps ErrUnsupportedFile.
	ErrImageUnreadable = fmt.Errorf("%w: the photo cannot be decoded", ErrUnsupportedFile)

	// ErrBusy means the subject or file kept changing while it was saved; trying again helps.
	ErrBusy = errors.New("library: busy")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("library: local storage failed")

	// ErrNotConfigured means the reading of texts is not wired (no extractor, reader or inspection store).
	ErrNotConfigured = errors.New("library: text reading is not configured")

	// ErrFileDamaged means the stored bytes of a file no longer have its sha256 or are gone; uploading the file again
	// repairs it.
	ErrFileDamaged = errors.New("library: stored file is damaged")

	// ErrNoText means a file whose text is not read: PDF (ADR-0024) or a photo (ADR-0027).
	ErrNoText = errors.New("library: the text of this file type is not read")

	// ErrTextUnreadable means a file whose text could not be read: damaged or beyond the reader's limits.
	ErrTextUnreadable = errors.New("library: the text of the file cannot be read")

	// ErrReaderUnavailable means the Hub's reader could not be reached now (no network, Hub not configured or busy);
	// trying again later helps and is not charged twice.
	ErrReaderUnavailable = errors.New("library: the reader is unavailable")

	// ErrQuota means the tester's quota on the Hub is used up.
	ErrQuota = errors.New("library: the quota is used up")

	// ErrReaderRefused means the reader's answer was refused (outside its schema); after inspection.MaxAttempts
	// refusals the text is not inspected again.
	ErrReaderRefused = errors.New("library: the reader's answer was refused")
)
