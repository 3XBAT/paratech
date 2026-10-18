package transcript

import "errors"

// Errors of the transcript package (ARCH-18).
var (
	// ErrInvalidTranscript is returned by New and Restore for a transcript that breaks a rule.
	ErrInvalidTranscript = errors.New("invalid transcript")

	// ErrInvalidDocument is returned by NewDocument and ParseDocument for a document that is malformed or breaks a limit.
	ErrInvalidDocument = errors.New("invalid transcript document")

	// ErrInvalidLanguage is returned for a language code that is not 2 to 8 lowercase ASCII letters.
	ErrInvalidLanguage = errors.New("invalid language code")

	// ErrUnknownFormat is returned for an export format other than txt and md.
	ErrUnknownFormat = errors.New("unknown transcript export format")

	// ErrExportTooLarge is returned by Render when the result would exceed MaxExportBytes.
	ErrExportTooLarge = errors.New("transcript export too large")
)
