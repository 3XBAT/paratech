package homeworkdraft

import "errors"

// Errors of the homework draft aggregate (ARCH-18). Messages name fields, never their content.
var (
	// ErrInvalidDraft is returned by New and Restore for an inconsistent draft history.
	ErrInvalidDraft = errors.New("invalid homework draft")

	// ErrInvalidContent is returned by AddGenerated and Edit for a title, body or generation the draft cannot hold.
	ErrInvalidContent = errors.New("invalid homework draft content")

	// ErrStaleBase means the version a generation or an edit started from is no longer the latest one: the draft
	// changed meanwhile. Nothing is added; the caller reloads and decides again.
	ErrStaleBase = errors.New("homework draft changed: the base version is not the latest")

	// ErrFull means the draft already has MaxRevisions versions.
	ErrFull = errors.New("homework draft has the maximum number of versions")
)
