package work

import (
	"fmt"
	"slices"
	"time"
)

// Problem is why the due reader call of a work did not go through (TASK-226). It is a code only, never a text of the
// reader or of the model, so the page shows a fixed text for it.
type Problem string

// Problems of a reader call.
const (
	ProblemOffline    Problem = "offline"     // no connection with the Hub: the same call is asked again for free
	ProblemQuota      Problem = "quota"       // the quota is used up
	ProblemRefused    Problem = "refused"     // the answer was refused (Refuse)
	ProblemNoText     Problem = "no_text"     // the source has no text the reader can read
	ProblemSourceGone Problem = "source_gone" // the assignment or the file was removed
)

// Problems returns every problem, for the schema checks.
func Problems() []Problem {
	return []Problem{ProblemOffline, ProblemQuota, ProblemRefused, ProblemNoText, ProblemSourceGone}
}

// Valid reports whether p is one of the problems.
func (p Problem) Valid() bool { return slices.Contains(Problems(), p) }

// keepsProblem reports whether a work in status s may keep the problem of its last reader call: only while the call is
// still due, or after its last attempt failed. Any other status follows a recorded answer, which clears it.
func keepsProblem(s Status) bool { return s == StatusTriage || s == StatusFailed }

// NoteProblem keeps why the due reader call did not go through; the call stays due, the attempt is not used (a refused
// answer is Refuse). Errors: ErrInvalidTransition when no call is due, ErrInvalidWork for an unknown problem.
func (w *Work) NoteProblem(p Problem, now time.Time) error {
	if w.status != StatusTriage {
		return fmt.Errorf("%w: no reader call is due in %s", ErrInvalidTransition, w.status)
	}
	if !p.Valid() {
		return fmt.Errorf("%w: problem %q", ErrInvalidWork, p)
	}
	next := w.clone()
	next.problem = p
	return w.move(next, StatusTriage, now)
}

// LastProblem returns why the last reader call did not go through; "" before any call and after a recorded answer.
func (w *Work) LastProblem() Problem { return w.problem }

// RecordSent keeps when the work was last sent to the teacher: at is when the delivery of a send of it was SENT, zero
// when none was. A time not after the kept one changes nothing (changed is false), so reading the same delivery again
// is harmless. The time is a fact about the send, not a change of the work: UpdatedAt stays. Error: ErrNoAssignment for
// a work that cannot be sent (Sendable).
func (w *Work) RecordSent(at time.Time) (changed bool, err error) {
	if err := w.Sendable(); err != nil {
		return false, err
	}
	if !at.After(w.sentAt) {
		return false, nil
	}
	w.sentAt = at
	return true, nil
}

// SentAt returns when the work was last sent to the teacher; zero if never.
func (w *Work) SentAt() time.Time { return w.sentAt }
