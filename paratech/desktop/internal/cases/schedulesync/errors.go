package schedulesync

import "errors"

// Errors of the schedule use case (ARCH-18). Domain and port errors are returned wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid schedule configuration")

	// ErrUnsupportedSource means no parser is configured for the import source.
	ErrUnsupportedSource = errors.New("unsupported schedule import source")

	// ErrNotConfirmed means a commit was requested without the confirm token of a preview.
	ErrNotConfirmed = errors.New("schedule import is not confirmed")

	// ErrInvalidImport means the file has row errors; nothing was written.
	ErrInvalidImport = errors.New("schedule import has errors")

	// ErrStalePreview means the plan differs from the confirmed preview (the file or the schedule changed meanwhile);
	// nothing was written and the user previews again.
	ErrStalePreview = errors.New("schedule import preview is out of date")
)
