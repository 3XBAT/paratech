package proposal

import "errors"

// Errors of the proposal package (ARCH-18). Messages never quote model text, so they are safe to log.
var (
	// ErrMalformed means the model output is not a valid proposal: not strict JSON, an unknown kind or field, a field
	// the kind does not allow, a missing required field or text out of bounds. Nothing is created.
	ErrMalformed = errors.New("malformed action proposal")

	// ErrInvalidID is returned for a malformed proposal identifier.
	ErrInvalidID = errors.New("invalid proposal id")

	// ErrInvalidRecipient is returned by NewRecipient for an address the core will not send to.
	ErrInvalidRecipient = errors.New("invalid recipient")

	// ErrInvalidDigest is returned by NewDigest for a malformed content digest.
	ErrInvalidDigest = errors.New("invalid content digest")

	// ErrStale means the trusted state a CONFIRMED proposal was validated against changed (the assignment is gone, the
	// recipient or the export file differs), so it is not executed. Recheck moves the proposal to STALE; a mailer
	// returns it for an attachment that no longer matches the pinned digest.
	ErrStale = errors.New("proposal is stale: trusted state changed after confirmation")

	// ErrInvalidProposal is returned by New and Restore for inconsistent input (no time, an inconsistent state).
	ErrInvalidProposal = errors.New("invalid proposal")

	// ErrInvalidTransition means the status table does not allow the requested change.
	ErrInvalidTransition = errors.New("invalid proposal status transition")

	// ErrNotConfirmed means a command was requested from a proposal the user has not confirmed.
	ErrNotConfirmed = errors.New("proposal is not confirmed")

	// ErrWrongKind means a command of another kind was requested than the proposal has.
	ErrWrongKind = errors.New("proposal has another kind")

	// ErrChooseTeacher means a send whose recipient the student has to choose (ReasonChooseTeacher) was confirmed
	// without a valid role: nothing changes, the card asks «Кому отправить?».
	ErrChooseTeacher = errors.New("choose the teacher to send to")
)
