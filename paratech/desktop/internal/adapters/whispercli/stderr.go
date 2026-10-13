package whispercli

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// maxLine bounds one stderr line kept for progress parsing; longer lines are skipped.
	maxLine = 1024

	// rawTailMax is the stderr tail kept in memory; redaction runs on it before it is cut to stderrMax.
	rawTailMax = 16 << 10

	// stderrMax bounds the stderr tail attached to errors.
	stderrMax = 2 << 10

	// defaultProgressEvery is the minimum interval between two progress reports (100 is always reported).
	defaultProgressEvery = time.Second
)

// progressLine matches the line printed by whisper_print_progress_callback ("<func>: progress =  5%"). It is
// anchored at a function name, so a path echoed by another log line cannot fake progress.
var progressLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*: progress = *([0-9]{1,3})%\s*$`)

// reporter forwards increasing percentages to the port's callback, at most once per interval.
type reporter struct {
	fn     func(percent int)
	every  time.Duration
	now    func() time.Time
	last   int
	lastAt time.Time
}

func newReporter(fn func(int), every time.Duration, now func() time.Time) *reporter {
	return &reporter{fn: fn, every: every, now: now, last: -1}
}

func (r *reporter) report(p int) {
	if r.fn == nil || p < 0 || p > 100 || p <= r.last {
		return
	}

	t := r.now()
	if p < 100 && r.last >= 0 && t.Sub(r.lastAt) < r.every {
		return
	}

	r.last, r.lastAt = p, t
	r.fn(p)
}

// stderrSink receives the process's stderr: it keeps a bounded tail and reports progress lines. The exec
// package writes to it from its copying goroutine; stop makes later writes inert.
type stderrSink struct {
	mu       sync.Mutex
	rep      *reporter
	line     []byte
	overlong bool
	tail     []byte
	cut      bool // the tail lost its beginning
	stopped  bool
}

func newStderrSink(rep *reporter) *stderrSink {
	return &stderrSink{rep: rep}
}

func (s *stderrSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tail = append(s.tail, p...)
	if len(s.tail) > 2*rawTailMax {
		s.tail = append(s.tail[:0], s.tail[len(s.tail)-rawTailMax:]...)
		s.cut = true
	}

	for _, b := range p {
		if b == '\n' || b == '\r' {
			if !s.overlong && !s.stopped {
				s.parse(s.line)
			}
			s.line, s.overlong = s.line[:0], false
			continue
		}

		if len(s.line) >= maxLine {
			s.overlong = true
			continue
		}
		s.line = append(s.line, b)
	}

	return len(p), nil
}

func (s *stderrSink) parse(line []byte) {
	m := progressLine.FindSubmatch(line)
	if m == nil {
		return
	}

	// 100 is kept for final: the result is not parsed and stored yet.
	if p, err := strconv.Atoi(string(m[1])); err == nil && p <= 100 {
		s.rep.report(min(p, 99))
	}
}

// stop disables progress reports from later writes (a stray writer after Wait).
func (s *stderrSink) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

// final reports 100 percent after a successful run.
func (s *stderrSink) final() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rep.report(100)
}

// tailText returns the last stderrMax bytes of stderr with the secrets replaced and control characters removed.
func (s *stderrSink) tailText(redact []redaction) string {
	s.mu.Lock()
	raw := string(s.tail)
	cut := s.cut || len(s.tail) > rawTailMax
	s.mu.Unlock()

	if len(raw) > rawTailMax {
		raw = raw[len(raw)-rawTailMax:]
	}

	// A cut first line may hold a partial path that redaction cannot recognise.
	if cut {
		if i := strings.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		} else {
			raw = ""
		}
	}

	text := clean(applyRedactions(strings.ToValidUTF8(raw, "?"), redact))
	text = strings.TrimSpace(text)
	if len(text) <= stderrMax {
		return text
	}

	text = text[len(text)-stderrMax:]
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return strings.ToValidUTF8(text, "")
}

// clean drops control characters except newlines and tabs.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// redaction replaces an absolute directory in messages with a placeholder.
type redaction struct {
	dir         string
	placeholder string
}

// applyRedactions replaces every occurrence of each directory, case-insensitively (Windows paths), also in the
// slash-separated form.
func applyRedactions(s string, rs []redaction) string {
	for _, r := range rs {
		if r.dir == "" {
			continue
		}
		s = replaceFold(s, r.dir, r.placeholder)
		if alt := strings.ReplaceAll(r.dir, `\`, "/"); alt != r.dir {
			s = replaceFold(s, alt, r.placeholder)
		}
	}
	return s
}

// replaceFold replaces old in s ignoring letter case. Matching is per byte window, so a case variant whose UTF-8
// length differs is not found; the adapter's own paths are echoed by the CLI unchanged.
func replaceFold(s, old, repl string) string {
	if old == "" || len(old) > len(s) {
		return s
	}

	var b strings.Builder
	i := 0
	for i+len(old) <= len(s) {
		if strings.EqualFold(s[i:i+len(old)], old) {
			b.WriteString(repl)
			i += len(old)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	b.WriteString(s[i:])
	return b.String()
}
