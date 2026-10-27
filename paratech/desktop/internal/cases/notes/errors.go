package notes

import "errors"

// Errors of the notes (ARCH-18). Fixed texts without names or paths. Errors of the library (a file that is empty, too
// large or of an unsupported type, a subject that is gone) come through the Materials port as they are.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("notes: invalid configuration")

	// ErrInvalidRequest means a malformed id, anchor, date or request.
	ErrInvalidRequest = errors.New("notes: invalid request")

	// ErrNoteNotFound means there is no such note (any more).
	ErrNoteNotFound = errors.New("notes: note not found")

	// ErrRecordingUnavailable means a recording that is not there, not completed or marked for deletion.
	ErrRecordingUnavailable = errors.New("notes: the recording cannot take notes")

	// ErrLessonNotFound means a lesson that is not in the schedule (any more).
	ErrLessonNotFound = errors.New("notes: lesson not found")

	// ErrOtherSubject means a material that is not filed under the note's subject.
	ErrOtherSubject = errors.New("notes: the file belongs to another subject")

	// ErrMaterialNotFound means a material that is not in the library.
	ErrMaterialNotFound = errors.New("notes: material not found")

	// ErrMaterialRemoved means a removed material was to become a part.
	ErrMaterialRemoved = errors.New("notes: material is removed")

	// ErrRemoved means a change of a removed note other than Restore.
	ErrRemoved = errors.New("notes: note is removed")

	// ErrEmptyTitle means a title with nothing left after cleaning.
	ErrEmptyTitle = errors.New("notes: empty title")

	// ErrNoFiles means a request without any file.
	ErrNoFiles = errors.New("notes: no files")

	// ErrTooManyParts means more parts than note.MaxParts.
	ErrTooManyParts = errors.New("notes: too many parts")

	// ErrAlreadyAdded means a material that is a part of the note already.
	ErrAlreadyAdded = errors.New("notes: already a part of the note")

	// ErrPartNotFound means a material that is not a part of the note.
	ErrPartNotFound = errors.New("notes: not a part of the note")

	// ErrInvalidOrder means an order that is not exactly the parts of the note.
	ErrInvalidOrder = errors.New("notes: order is not the parts of the note")

	// ErrEmptyText means a pasted text with nothing but spaces.
	ErrEmptyText = errors.New("notes: empty text")

	// ErrTextTooLarge means a pasted text larger than MaxPastedBytes.
	ErrTextTooLarge = errors.New("notes: text too large")

	// ErrInvalidText means a pasted text that is not plain text (a NUL character, or the start of a PDF).
	ErrInvalidText = errors.New("notes: not a plain text")

	// ErrChanged means the subject or a file of the note was gone when it was saved: reloading shows the state.
	ErrChanged = errors.New("notes: subject or file changed")

	// ErrBusy means the note kept changing while it was saved; trying again helps.
	ErrBusy = errors.New("notes: busy")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("notes: local storage failed")
)
