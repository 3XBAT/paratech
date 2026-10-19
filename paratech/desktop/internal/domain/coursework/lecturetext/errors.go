package lecturetext

import "errors"

// Errors of the lecture text aggregate (ARCH-18). Messages name fields and positions, never their content.
var (
	// ErrInvalidLectureText is returned by New, Restore, AddPart and Publish for an inconsistent lecture text.
	ErrInvalidLectureText = errors.New("invalid lecture text")

	// ErrInvalidBlock is returned by NewBlock for a block the lecture text cannot hold.
	ErrInvalidBlock = errors.New("invalid lecture text block")

	// ErrPartOutOfOrder is returned by AddPart for a part that is not the next one of its run.
	ErrPartOutOfOrder = errors.New("lecture text part out of order")

	// ErrRunIncomplete is returned by Publish when the run does not have all its parts.
	ErrRunIncomplete = errors.New("lecture text run is not complete")
)
