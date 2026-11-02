package agentstep

import (
	"strings"
	"time"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/toolcatalog"
)

// Coalescing of the streamed reply: the provider writes a few characters per event, so chunks are joined until
// draftFlushBytes are pending or draftFlushEvery has passed since the last flush. The first chunk goes at once. The
// time is checked only when a chunk arrives (no timer): when the model pauses, up to draftFlushBytes-1 bytes wait for
// the next chunk or the end of the step.
const (
	draftFlushBytes = 64
	draftFlushEvery = 50 * time.Millisecond
)

// draft joins the reply chunks of one StepStream and passes them on, at most toolcatalog.MaxReplyTextRunes characters
// in all (the longest reply the catalog takes). It runs on the provider's goroutine; the time comes from the clock, so
// tests decide when a chunk goes out.
type draft struct {
	clock   Clock
	out     func(string)
	pending strings.Builder
	last    time.Time
	sent    bool
	left    int
	chunks  int
}

func newDraft(clock Clock, out func(string)) *draft {
	return &draft{clock: clock, out: out, left: toolcatalog.MaxReplyTextRunes}
}

// push queues chunk, cut at the bound, and flushes when enough is pending or enough time has passed.
func (d *draft) push(chunk string) {
	if d.left <= 0 || chunk == "" {
		return
	}
	if n := utf8.RuneCountInString(chunk); n > d.left {
		chunk = cutRunes(chunk, d.left)
		d.left = 0
	} else {
		d.left -= n
	}
	d.pending.WriteString(chunk)
	if now := d.clock.Now(); !d.sent || d.pending.Len() >= draftFlushBytes || now.Sub(d.last) >= draftFlushEvery || d.left == 0 {
		d.flushAt(now)
	}
}

// flush passes on what is pending.
func (d *draft) flush() { d.flushAt(d.clock.Now()) }

func (d *draft) flushAt(now time.Time) {
	if d.pending.Len() == 0 {
		return
	}
	chunk := d.pending.String()
	d.pending.Reset()
	d.sent, d.last = true, now
	d.chunks++
	d.out(chunk)
}

// cutRunes is the first n characters of s.
func cutRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
