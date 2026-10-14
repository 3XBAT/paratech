package summary

import "errors"

// Errors of the summary aggregate (ARCH-18). Messages name fields, never their content.
var (
	// ErrInvalidSummary is returned by New, Restore and Replace for an inconsistent summary.
	ErrInvalidSummary = errors.New("invalid summary")

	// ErrInvalidContent is returned by NewContent for a text or key points the summary cannot hold.
	ErrInvalidContent = errors.New("invalid summary content")
)
