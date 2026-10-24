package ticketset

import "errors"

// Errors of the aggregate (ARCH-18). None of them quotes the list or a ticket title.
var (
	// ErrNoTickets means the list holds no numbered ticket («Билет 1», «1.», «1)»).
	ErrNoTickets = errors.New("ticketset: no tickets found")

	// ErrTooManyTickets means the list holds more than MaxTickets tickets.
	ErrTooManyTickets = errors.New("ticketset: too many tickets")

	// ErrListTooLong means the list text is larger than MaxListBytes.
	ErrListTooLong = errors.New("ticketset: the list text is too long")

	// ErrTitleTooLong means a ticket title is longer than MaxTitleRunes.
	ErrTitleTooLong = errors.New("ticketset: a ticket is too long")

	// ErrCostNotAccepted means the estimate of the run is above the cap and the student did not accept it.
	ErrCostNotAccepted = errors.New("ticketset: the cost of the run is not accepted")

	// ErrInvalidTransition means the move is not allowed in the current status.
	ErrInvalidTransition = errors.New("ticketset: invalid transition")

	// ErrStaleStep means a step's result is for a step that is not due any more; it is dropped.
	ErrStaleStep = errors.New("ticketset: the step is not due any more")

	// ErrInvalidSet means stored or given state breaks an invariant of the aggregate.
	ErrInvalidSet = errors.New("ticketset: invalid ticket set")
)
