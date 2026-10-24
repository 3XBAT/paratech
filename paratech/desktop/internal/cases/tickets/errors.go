package tickets

import "errors"

// Errors of the use case. Each wraps its cause; none quotes the list or an answer.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("tickets: invalid configuration")

	// ErrNotFound means the set does not exist (also shared.ErrNotFound).
	ErrNotFound = errors.New("tickets: set not found")

	// ErrInvalidRequest means the move does not fit the set (its status) or the request is malformed.
	ErrInvalidRequest = errors.New("tickets: invalid request")

	// ErrSubjectNotFound means the subject of a pasted list does not exist.
	ErrSubjectNotFound = errors.New("tickets: subject not found")

	// ErrNoSubject means the material of the list is in «Входящие»: it has no subject to answer from.
	ErrNoSubject = errors.New("tickets: the material has no subject")

	// ErrSourceNotFound means the material of the list is gone or removed.
	ErrSourceNotFound = errors.New("tickets: the material is gone")

	// ErrNoText means the material has no text that can be read (a PDF, an unreadable or empty file).
	ErrNoText = errors.New("tickets: the material has no readable text")

	// ErrNoTickets, ErrTooManyTickets, ErrListTooLong and ErrTicketTooLong are the refusals of the list
	// (ticketset.ParseList).
	ErrNoTickets      = errors.New("tickets: no tickets found")
	ErrTooManyTickets = errors.New("tickets: too many tickets")
	ErrListTooLong    = errors.New("tickets: the list is too long")
	ErrTicketTooLong  = errors.New("tickets: a ticket is too long")

	// ErrCostNotAccepted means the estimate of the run is above the cap and the student did not accept it.
	ErrCostNotAccepted = errors.New("tickets: the cost of the run is not accepted")

	// ErrAlreadyDone means the answers are ready: the file is in «Билеты».
	ErrAlreadyDone = errors.New("tickets: the answers are ready")

	// ErrRetries means the run was started again too often after failed or cancelled jobs.
	ErrRetries = errors.New("tickets: the run was retried too often")

	// ErrChanged means the filed file was moved or removed by the student: the Undo leaves it alone.
	ErrChanged = errors.New("tickets: the file changed")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("tickets: local storage failed")
)

// The fixed errors a ticket_answers job ends with: the job keeps only the text of its last error (never a cause, a
// title or an answer).
var (
	ErrJobPayload = errors.New("tickets job: invalid job payload")
	ErrJobGone    = errors.New("tickets job: the set is gone")
	ErrJobAnswer  = errors.New("tickets job: the answer call failed")
	ErrJobFile    = errors.New("tickets job: the file could not be built")
	ErrJobStorage = errors.New("tickets job: local storage failed")
	ErrJobStopped = errors.New("tickets job: stopped")
)

// jobError is a fixed error of a job with its cause, for errors.Is (the runner reads the network and quota causes);
// its text is the fixed one only.
type jobError struct {
	msg   error
	cause error
}

func (e *jobError) Error() string   { return e.msg.Error() }
func (e *jobError) Unwrap() []error { return []error{e.msg, e.cause} }

func fail(msg, cause error) error {
	if cause == nil {
		return msg
	}
	return &jobError{msg: msg, cause: cause}
}
