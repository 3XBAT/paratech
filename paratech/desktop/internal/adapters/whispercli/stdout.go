package whispercli

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"paratech/desktop/internal/domain/transcription/transcript"
)

const (
	// maxStdoutLine bounds one stdout line: the timestamps and a segment text of maxSegmentText bytes. Longer lines
	// are dropped whole.
	maxStdoutLine = maxSegmentText + 64

	// maxQueuedSegments and maxQueuedBytes bound the segments waiting for a slow callback; beyond them new
	// segments are dropped (the result file still has them).
	maxQueuedSegments = 4096
	maxQueuedBytes    = 8 << 20
)

// segmentLine matches a line of whisper_print_segment_callback (examples/cli/cli.cpp v1.9.5), printed for every new
// segment: "[00:00:00.000 --> 00:00:05.000]  text". Hours have at least two digits; three bound the parse.
var segmentLine = regexp.MustCompile(
	`^\[([0-9]{2,3}):([0-5][0-9]):([0-5][0-9])\.([0-9]{3}) --> ([0-9]{2,3}):([0-5][0-9]):([0-5][0-9])\.([0-9]{3})\](.*)$`)

// stdoutSink receives the process's stdout, which repeats each recognised segment as soon as it is decoded. It keeps
// no text beyond the current line: parsed segments go to the pump, everything else is dropped. Nothing of it is ever
// logged or put into an error, since it is the transcript itself.
type stdoutSink struct {
	mu        sync.Mutex
	send      func(transcript.Segment)
	line      []byte
	overlong  bool
	prevStart int64
	stopped   bool
}

func newStdoutSink(send func(transcript.Segment)) *stdoutSink {
	return &stdoutSink{send: send, line: make([]byte, 0, maxStdoutLine)}
}

// Write splits p into lines; a line may arrive in several writes, so a multi-byte character can be split too.
func (s *stdoutSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range p {
		if b == '\n' || b == '\r' {
			if !s.overlong && !s.stopped && len(s.line) > 0 {
				s.parse(s.line)
			}
			s.line, s.overlong = s.line[:0], false
			continue
		}

		if len(s.line) >= maxStdoutLine {
			s.overlong = true
			continue
		}
		s.line = append(s.line, b)
	}

	return len(p), nil
}

// parse delivers a valid segment line. Times are clamped like in the result (convertSegment): the start not before
// the previous start, the end not before the start. Invalid UTF-8 (a token boundary) becomes U+FFFD, as in the
// result file; empty texts are skipped.
func (s *stdoutSink) parse(line []byte) {
	m := segmentLine.FindSubmatch(line)
	if m == nil {
		return
	}

	from, to := stampMs(m[1:5]), stampMs(m[5:9])
	if from > maxOffsetMs || to > maxOffsetMs {
		return
	}
	from = max(from, s.prevStart)
	to = max(to, from)

	text := normaliseText(strings.ToValidUTF8(string(m[9]), "�"))
	if text == "" || len(text) > maxSegmentText {
		return
	}

	s.prevStart = from
	s.send(transcript.Segment{StartMs: from, EndMs: to, Text: text})
}

// stampMs converts the matched hours, minutes, seconds and milliseconds (all digits) to milliseconds.
func stampMs(parts [][]byte) int64 {
	var n [4]int64
	for i, p := range parts {
		v, _ := strconv.ParseInt(string(p), 10, 64) // the pattern allows only up to three digits
		n[i] = v
	}
	return ((n[0]*60+n[1])*60+n[2])*1000 + n[3]
}

// stop makes later writes inert (a stray writer after Wait).
func (s *stdoutSink) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.line = nil
}

// segmentPump calls the port's callback on its own goroutine with every segment in order, so a slow callback never
// blocks the stdout pipe (and with it the process). The queue is bounded; calls never overlap.
type segmentPump struct {
	fn      func(transcript.Segment)
	mu      sync.Mutex
	queue   []transcript.Segment
	bytes   int
	dropped int
	wake    chan struct{}
	quit    chan struct{}
	done    chan struct{}
}

func startSegmentPump(fn func(transcript.Segment)) *segmentPump {
	p := &segmentPump{
		fn:   fn,
		wake: make(chan struct{}, 1),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	go p.loop()
	return p
}

// send queues seg for the pump goroutine; it never blocks on the callback.
func (p *segmentPump) send(seg transcript.Segment) {
	p.mu.Lock()
	if len(p.queue) >= maxQueuedSegments || p.bytes+len(seg.Text) > maxQueuedBytes {
		p.dropped++
		p.mu.Unlock()
		return
	}
	p.queue = append(p.queue, seg)
	p.bytes += len(seg.Text)
	p.mu.Unlock()

	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *segmentPump) take() []transcript.Segment {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.queue
	p.queue, p.bytes = nil, 0
	return out
}

func (p *segmentPump) deliver() {
	for _, seg := range p.take() {
		p.fn(seg)
	}
}

func (p *segmentPump) loop() {
	defer close(p.done)
	for {
		select {
		case <-p.wake:
			p.deliver()
		case <-p.quit:
			p.deliver()
			return
		}
	}
}

// stop delivers the queued segments and waits for the goroutine (and a running callback) to end.
func (p *segmentPump) stop() {
	close(p.quit)
	<-p.done
}

// droppedCount returns how many segments a full queue refused.
func (p *segmentPump) droppedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped
}
