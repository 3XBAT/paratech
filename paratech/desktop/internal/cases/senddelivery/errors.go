package senddelivery

import "errors"

// Errors of the use case (ARCH-18). Outcome errors of a send are the sentinels of the delivery package.
var (
	// ErrInvalidConfig is returned by New for missing ports or unusable settings.
	ErrInvalidConfig = errors.New("invalid delivery configuration")

	// ErrZeroCommand is returned for the zero proposal.EmailCommand, which no confirmed proposal produces.
	ErrZeroCommand = errors.New("zero email command")

	// ErrUnresolvedDelivery means another delivery of the same assignment may still send or may already have sent
	// its message (PENDING, SENDING or UNKNOWN). The user resolves it first ("mark as sent" or "send again"), so an
	// unknown outcome is never bypassed by a new delivery (hard rule 7).
	ErrUnresolvedDelivery = errors.New("another delivery of this assignment is unresolved")

	// ErrAttachmentMissing means the file to attach does not exist (any more). Nothing was sent.
	ErrAttachmentMissing = errors.New("attachment file is missing")

	// ErrResultNotStored means the result of an attempt could not be saved. The delivery stays SENDING, which startup
	// recovery turns into UNKNOWN; the job is not retried, so the message is not sent twice.
	ErrResultNotStored = errors.New("delivery result could not be stored")
)
