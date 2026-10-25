package anthropic_test

import (
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/adapters/anthropic"
	"paratech/hub/internal/domain/llm/values"
)

// Probe environment (TASK-144). The probe runs only when URL and key are both set, so CI skips it.
const (
	probeURLEnv     = "PARATECH_PROBE_ANTHROPIC_URL"   // base URL without path, e.g. the owner's gateway
	probeKeyEnv     = "PARATECH_PROBE_ANTHROPIC_KEY"   // never logged
	probeModelEnv   = "PARATECH_PROBE_ANTHROPIC_MODEL" // optional
	probeModel      = "claude-haiku-5-5"
	probeTimeout    = 2 * time.Minute
	probeMinDeltas  = 2
	probeMinSpread  = 100 * time.Millisecond
	probeMaxNameLen = 32
)

// TestProbeGatewayStreaming makes one short streamed call and checks that the deltas arrive spread over time (a
// gateway that buffers the stream delivers them all at once at the end). It logs only event names, counts and
// timings: never the key, the prompt or the model text.
func TestProbeGatewayStreaming(t *testing.T) {
	base, key := os.Getenv(probeURLEnv), os.Getenv(probeKeyEnv)
	if base == "" || key == "" {
		t.Skipf("set %s and %s to run the streaming probe", probeURLEnv, probeKeyEnv)
	}
	model := os.Getenv(probeModelEnv)
	if model == "" {
		model = probeModel
	}

	start := time.Now()
	events := &eventLog{t: t, start: start}
	c, err := anthropic.New(anthropic.Options{APIKey: key, BaseURL: base, Timeout: probeTimeout,
		Transport: &eventTap{next: http.DefaultTransport, log: events}})
	if err != nil {
		t.Fatalf("client options rejected: %v", err)
	}
	req, err := values.NewRequest(model, "Write the numbers from 1 to 60 in words, separated by commas. Nothing else.",
		"Go.", 400)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	var first, last time.Duration
	got, err := c.Stream(t.Context(), req, func(string) {
		now := time.Since(start)
		if count == 0 {
			first = now
		}
		last = now
		count++
	})
	end := time.Since(start)
	if err != nil {
		t.Fatalf("stream failed after %s: %v", end.Round(time.Millisecond), err)
	}

	t.Logf("deltas=%d first=%s last=%s end=%s stop=%s usage=%d/%d", count, first.Round(time.Millisecond),
		last.Round(time.Millisecond), end.Round(time.Millisecond), got.Stop(), got.Usage().Input(), got.Usage().Output())
	if count < probeMinDeltas {
		t.Fatalf("got %d deltas, want at least %d", count, probeMinDeltas)
	}
	if got.Usage().Input() <= 0 || got.Usage().Output() <= 0 {
		t.Fatalf("usage %d/%d is missing", got.Usage().Input(), got.Usage().Output())
	}
	if spread := end - first; spread < probeMinSpread {
		t.Fatalf("the first delta came %s before the end (want >= %s): the gateway seems to buffer the stream",
			spread.Round(time.Millisecond), probeMinSpread)
	}
}

// eventTap logs the status and content type of the answer and wraps its body to log event names.
type eventTap struct {
	next http.RoundTripper
	log  *eventLog
}

func (e *eventTap) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := e.next.RoundTrip(r)
	if err != nil {
		e.log.logf("request failed")
		return nil, err
	}
	e.log.logf("status=%d content-type=%q", resp.StatusCode, safeName(resp.Header.Get("Content-Type"), 64))
	resp.Body = &eventReader{ReadCloser: resp.Body, log: e.log}
	return resp, nil
}

// eventReader passes the body through and logs the name of every "event:" line with its arrival time. It keeps only
// the first bytes of a line, enough for the field name and the event name.
type eventReader struct {
	io.ReadCloser
	log  *eventLog
	line []byte
}

func (e *eventReader) Read(p []byte) (int, error) {
	n, err := e.ReadCloser.Read(p)
	for _, b := range p[:n] {
		switch {
		case b == '\n':
			if len(e.line) > len("event:") && string(e.line[:len("event:")]) == "event:" {
				e.log.logf("event %s", safeName(string(e.line[len("event:"):]), probeMaxNameLen))
			}
			e.line = e.line[:0]
		case len(e.line) < len("event:")+probeMaxNameLen+1:
			e.line = append(e.line, b)
		}
	}
	return n, err
}

// safeName keeps only [a-z0-9_/;=.-] and drops the rest, so nothing but identifiers reaches the log.
func safeName(s string, maxLen int) string {
	out := make([]byte, 0, maxLen)
	for i := 0; i < len(s) && len(out) < maxLen; i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '/' || c == ';' || c == '=' || c == '.' || c == '-' {
			out = append(out, c)
		}
	}
	return string(out)
}

type eventLog struct {
	mu    sync.Mutex
	t     *testing.T
	start time.Time
}

func (l *eventLog) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.t.Logf("+%s "+format, append([]any{time.Since(l.start).Round(time.Millisecond)}, args...)...)
}
