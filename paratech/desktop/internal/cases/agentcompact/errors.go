package agentcompact

import "errors"

// Errors of the use case: every failed Compact matches exactly one of them and stores nothing. The cause is wrapped
// too (shared.ErrNotFound of a missing chat still matches), so desktop.log tells them apart; no error text holds a
// summary or a student's text.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("agentcompact: invalid configuration")

	// ErrInvalidInput means the chat ID is malformed.
	ErrInvalidInput = errors.New("agentcompact: invalid input")

	// ErrBusy means the chat's request is running or has a step open: no compaction during a step.
	ErrBusy = errors.New("agentcompact: the chat's request is running")

	// ErrChanged means the chat changed while the summary was made and can no longer take it, or kept changing.
	ErrChanged = errors.New("agentcompact: the chat changed")

	// ErrQuota means the tester's Hub quota is used up.
	ErrQuota = errors.New("agentcompact: quota exceeded")

	// ErrUnavailable means the summary could not be made now: no network, the Hub not set up, unavailable, busy, rate
	// limited or out of time, or the caller went away. The next request tries again.
	ErrUnavailable = errors.New("agentcompact: hub unavailable")

	// ErrRefused means the Hub refused the request or the model's answer for good, or the answer is not a summary the
	// chat may keep. A refused answer is charged and replayed for the same key, so the key is not asked again.
	ErrRefused = errors.New("agentcompact: summary refused")

	// ErrStore means the local store failed: the chat or its history could not be read (or is missing), or the chat
	// could not be saved for another reason than a concurrent change.
	ErrStore = errors.New("agentcompact: the local store failed")
)
