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

	// ErrAttachmentChanged means the attachment is not the content the user confirmed (its SHA-256 or size
	// differs). Nothing was sent; the delivery fails, the user confirms the new content in a new proposal.
	ErrAttachmentChanged = errors.New("attachment differs from the confirmed content")

	// ErrAttemptsExhausted means the job of the delivery used up its attempts without handing the message over;
	// the delivery fails (FailUnsent), nothing was sent.
	ErrAttemptsExhausted = errors.New("attempts exhausted")
)

// Errors of the aggregate.
var (
	// ErrInvalidDelivery is returned by New and Restore for data that breaks an invariant.
	ErrInvalidDelivery = errors.New("invalid delivery")

	// ErrInvalidTransition is returned for a status change the transition table does not allow.
	ErrInvalidTransition = errors.New("invalid delivery transition")

	// ErrInvalidPayload is returned for a delivery job payload that is not exactly {"delivery_id": "<id>"}.
	ErrInvalidPayload = errors.New("invalid delivery job payload")
)
