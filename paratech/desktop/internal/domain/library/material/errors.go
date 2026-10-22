package material

import "errors"

// Errors of the material aggregate (ARCH-18).
var (
	// ErrInvalidMaterial means a state that breaks an invariant (a bad id, digest, size or place).
	ErrInvalidMaterial = errors.New("invalid material")

	// ErrEmpty means a file without content.
	ErrEmpty = errors.New("empty file")

	// ErrTooLarge means a file larger than MaxBytes.
	ErrTooLarge = errors.New("file too large")

	// ErrUnsupportedType means content that is not DOCX, PPTX, PDF or UTF-8 text.
	ErrUnsupportedType = errors.New("unsupported file type")

	// ErrRemoved means a change of a removed material other than Restore.
	ErrRemoved = errors.New("material is removed")

	// ErrInvalidTransition means a status change the table of transitions does not allow.
	ErrInvalidTransition = errors.New("invalid material status transition")

	// ErrTooManyLessons means the material is linked to as many lessons as it may be.
	ErrTooManyLessons = errors.New("too many linked lessons")
)
