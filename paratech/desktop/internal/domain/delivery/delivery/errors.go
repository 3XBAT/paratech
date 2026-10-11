package delivery

import "errors"

// Outcome errors of sending an email. The EmailAdapter returns them (wrapped) so that the use case can
// choose the next step with errors.Is without knowing the mail provider (ARCH-18, ARCH-32).
var (
	// ErrOutcomeUnknown means the provider may or may not have accepted the message (for example a timeout
	// after the message was handed over). The delivery becomes UNKNOWN and is reconciled, never blindly
	// retried (PRD sections 10 and 14, AC-15, hard rule 7).
	ErrOutcomeUnknown = errors.New("delivery outcome unknown")

	// ErrRejected means the provider definitely refused the message; nothing was sent and retrying
	// the same request will not help (PRD section 10).
	ErrRejected = errors.New("delivery rejected")

	// ErrTransient means the message was definitely not accepted and a later retry may succeed,
	// for example a connection refused before any data was sent (PRD sections 7.3 and 10).
	ErrTransient = errors.New("delivery failed transiently")
)
