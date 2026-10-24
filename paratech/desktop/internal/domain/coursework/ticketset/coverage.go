package ticketset

import "paratech/desktop/internal/domain/coursework/answer"

// Coverage tells how far the student's lessons answer a ticket.
type Coverage string

// Coverages.
const (
	CoverageCovered    Coverage = "covered"     // every claim has a citation that passed the check, nothing is missing
	CoveragePartial    Coverage = "partial"     // some claims are supported, others or parts of the ticket are not
	CoverageNotCovered Coverage = "not_covered" // «нет в ваших лекциях»: no supported claim
	CoverageFailed     Coverage = "failed"      // the reader's answer was refused: no answer to show
)

// Valid reports whether c is one of the coverages.
func (c Coverage) Valid() bool {
	switch c {
	case CoverageCovered, CoveragePartial, CoverageNotCovered, CoverageFailed:
		return true
	}
	return false
}

// Classify is the coverage of an answer: nil (no passage of the lessons matched the ticket) or an answer without a
// supported claim is not covered; one whose claims are all supported and that misses nothing is covered; anything
// between is partial. An unsupported claim never counts as «из ваших лекций».
func Classify(a *answer.Answer) Coverage {
	if a == nil {
		return CoverageNotCovered
	}
	c := a.Counts()
	switch {
	case c.Supported == 0:
		return CoverageNotCovered
	case c.Unsupported == 0 && c.Missing == 0:
		return CoverageCovered
	}
	return CoveragePartial
}

// Report counts the tickets of a set by coverage; tickets without a result yet are Pending.
type Report struct {
	Covered    int
	Partial    int
	NotCovered int
	Failed     int
	Pending    int
}
