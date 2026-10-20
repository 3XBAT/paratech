package transcript

import (
	"fmt"
	"time"
)

// Rules of the live parts (TASK-079): what the engine recognised so far, stored in batches while it runs, so the
// viewer shows it and a restart does not lose it. The document stays the result; the parts are a preview and are
// removed once the transcript is stored.
const (
	// LiveBatchParts is how many parts are stored at once.
	LiveBatchParts = 20

	// LiveFlushEvery bounds how long a recognised part waits before it is stored.
	LiveFlushEvery = 5 * time.Second

	// maxLivePending bounds the parts kept for a retry after failed stores; older ones are dropped.
	maxLivePending = 1000
)

// Part is one segment recognised while the engine runs, numbered in the order of the run from 0. A rerun of the
// engine (whisper-cli cannot resume) numbers from 0 again, so storing by number replaces the parts of the earlier run
// one by one. The text could later be edited by the user without changing the number (manual correction).
type Part struct {
	seq     int
	segment Segment
}

// NewPart validates a part: 0 <= seq < MaxSegments and the segment rules of NewDocument (without the order, which
// LiveBatch keeps within one run; stored parts of two runs may interleave).
func NewPart(seq int, s Segment) (Part, error) {
	if seq < 0 || seq >= MaxSegments {
		return Part{}, fmt.Errorf("%w: part number %d", ErrInvalidDocument, seq)
	}
	if err := checkSegment(s, 0); err != nil {
		return Part{}, fmt.Errorf("%w: part %d: %w", ErrInvalidDocument, seq, err)
	}
	return Part{seq: seq, segment: s}, nil
}

// Seq returns the number of the part in its run.
func (p Part) Seq() int { return p.seq }

// Segment returns the recognised segment.
func (p Part) Segment() Segment { return p.segment }

// LiveBatch numbers the segments of one engine run and decides when the pending parts are stored: every
// LiveBatchParts parts or once the oldest waited LiveFlushEvery. It is not safe for concurrent use.
type LiveBatch struct {
	next      int
	prevStart int64
	pending   []Part
	since     time.Time // when the oldest pending part arrived
	notBefore time.Time // after a failed store: no retry before this time
}

// NewLiveBatch returns the batch of a new run, numbering from 0.
func NewLiveBatch() *LiveBatch { return &LiveBatch{} }

// Add numbers s as the next part and reports whether the pending parts are due. A segment that breaks the rules or
// starts before the previous one is refused with ErrInvalidDocument and takes no number. A segment without speech
// (Segment.Speech, TASK-110) is left out without an error; the marks are removed from one with speech.
func (b *LiveBatch) Add(s Segment, now time.Time) (due bool, err error) {
	s, ok := s.Speech()
	if !ok {
		return b.Due(now), nil
	}
	if s.StartMs < b.prevStart {
		return b.Due(now), fmt.Errorf("%w: part %d starts before the previous one", ErrInvalidDocument, b.next)
	}
	p, err := NewPart(b.next, s)
	if err != nil {
		return b.Due(now), err
	}

	b.next++
	b.prevStart = s.StartMs
	if len(b.pending) == 0 {
		b.since = now
	}
	b.pending = append(b.pending, p)
	return b.Due(now), nil
}

// Due reports whether pending parts should be stored now.
func (b *LiveBatch) Due(now time.Time) bool {
	if len(b.pending) == 0 || now.Before(b.notBefore) {
		return false
	}
	return len(b.pending) >= LiveBatchParts || now.Sub(b.since) >= LiveFlushEvery
}

// Take returns the pending parts in order and clears them.
func (b *LiveBatch) Take() []Part {
	out := b.pending
	b.pending = nil
	return out
}

// Retain puts parts whose store failed back before the pending ones; they are not due again before LiveFlushEvery
// has passed. Beyond maxLivePending the oldest are dropped: the document is the result, the parts only a preview.
func (b *LiveBatch) Retain(parts []Part, now time.Time) {
	all := append(append([]Part(nil), parts...), b.pending...)
	if len(all) > maxLivePending {
		all = all[len(all)-maxLivePending:]
	}
	b.pending = all
	b.since = now
	b.notBefore = now.Add(LiveFlushEvery)
}

// Pending returns how many parts wait to be stored.
func (b *LiveBatch) Pending() int { return len(b.pending) }
