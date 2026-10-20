package knowledgebase

import "errors"

// Errors of the knowledge base (ARCH-18). Fixed texts without paths or lesson text; causes go to the log.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("knowledgebase: invalid configuration")

	// ErrInvalidRequest means an invalid recording id or path.
	ErrInvalidRequest = errors.New("knowledgebase: invalid request")

	// ErrInvalidQuery means a search query that is too short or too long.
	ErrInvalidQuery = errors.New("knowledgebase: invalid search query")

	// ErrInvalidLessonInfo means a subject that is too long or an unknown lesson kind.
	ErrInvalidLessonInfo = errors.New("knowledgebase: invalid subject or kind")

	// ErrRecordingNotFound means the recording is not (any more) a lesson of the knowledge base.
	ErrRecordingNotFound = errors.New("knowledgebase: recording not found")

	// ErrSubjectNotFound means the subject has no lesson (any more).
	ErrSubjectNotFound = errors.New("knowledgebase: subject not found")

	// ErrNotFinished means the recording is still being recorded or finalized.
	ErrNotFinished = errors.New("knowledgebase: the recording is not finished")

	// ErrBusy means the recording kept changing while the correction was saved; trying again helps.
	ErrBusy = errors.New("knowledgebase: the recording is busy")

	// ErrUnavailable means local storage could not be read or the file could not be written.
	ErrUnavailable = errors.New("knowledgebase: local storage failed")
)
