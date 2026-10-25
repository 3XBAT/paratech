package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

// Fixtures in the shape of docs/refs/anthropic.md section 8 (spaces after colons as in the docs' examples).
const (
	sseStart = `{"type": "message_start", "message": {"id": "msg_1", "type": "message", "role": "assistant", "content": [],
		"model": "claude-sonnet-5-5", "stop_reason": null, "stop_sequence": null,
		"usage": {"input_tokens": 25, "output_tokens": 1, "cache_read_input_tokens": 5}}}`
	sseThinkingStart = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`
	sseThinkingDelta = `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hidden reasoning"}}`
	sseSignature     = `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`
	sseTextStart     = `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`
	ssePing          = `{"type": "ping"}`
	sseStop          = `{"type":"message_stop"}`
)

func sseEvent(name, data string) string {
	return "event: " + name + "\ndata: " + strings.ReplaceAll(data, "\n", " ") + "\n\n"
}

func sseText(index int, text string) string {
	raw, _ := json.Marshal(text)
	return sseEvent("content_block_delta",
		fmt.Sprintf(`{"type": "content_block_delta","index": %d,"delta": {"type": "text_delta", "text": %s}}`, index, raw))
}

func sseBlockStop(index int) string {
	return sseEvent("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
}

func sseMessageDelta(stop string, output int) string {
	return sseEvent("message_delta", fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%s,"stop_sequence":null},"usage":{"output_tokens":%d}}`, stop, output))
}

// sseHead is message_start, a thinking block and the start of a text block.
func sseHead() string {
	return sseEvent("message_start", sseStart) + sseEvent("content_block_start", sseThinkingStart) +
		sseEvent("content_block_delta", sseThinkingDelta) + sseEvent("content_block_delta", sseSignature) +
		sseBlockStop(0) + sseEvent("content_block_start", sseTextStart)
}

// sseNormal is a whole stream with the text deltas "Hel", "lo" and " world".
func sseNormal() string {
	return sseHead() + sseText(1, "Hel") + sseText(1, "lo") + sseText(1, " world") + sseBlockStop(1) +
		sseMessageDelta(`"end_turn"`, 15) + sseEvent("message_stop", sseStop)
}

// sseServer answers every call with a 200 SSE stream written as chunks, each flushed; after them the handler cuts
// the connection (cut), stalls until the client goes away (stall) or returns. sent gets a value after the chunks.
type sseServer struct {
	chunks      []string
	contentType string
	cut, stall  bool
	sent        chan struct{}
	hits        atomic.Int32
}

func (s *sseServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.contentType == "" {
		s.contentType = "text/event-stream"
	}
	s.sent = make(chan struct{}, 32)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		w.Header().Set("Content-Type", s.contentType)
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		for _, c := range s.chunks {
			_, _ = io.WriteString(w, c)
			_ = rc.Flush()
		}
		s.sent <- struct{}{}
		switch {
		case s.cut:
			panic(http.ErrAbortHandler) // drops the connection without a log line
		case s.stall:
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

func serveSSE(t *testing.T, chunks ...string) *httptest.Server {
	t.Helper()
	return (&sseServer{chunks: chunks}).start(t)
}

// collect returns an onDelta that records the chunks; Stream must call it from one goroutine (-race checks it).
func collect() (*[]string, func(string)) {
	var got []string
	return &got, func(c string) { got = append(got, c) }
}

// streamError checks the shape every Stream failure must have and returns it.
func streamError(t *testing.T, err error) *values.ProviderError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, leak := range []string{testKey, bodyMarker, "Summarize the lesson", "lesson-data", "hidden reasoning"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error %q leaks %q", err, leak)
		}
	}
	var pe *values.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("error %v (%T) is not a ProviderError", err, err)
	}
	if !strings.HasPrefix(err.Error(), "anthropic.Stream: ") {
		t.Fatalf("error %q has no operation name", err)
	}
	return pe
}

func TestStreamSendsTheRequestAndReadsTheDeltas(t *testing.T) {
	var (
		gotHeader http.Header
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" || r.URL.RawQuery != "" {
			t.Errorf("request line = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, sseNormal())
	}))
	t.Cleanup(srv.Close)

	deltas, onDelta := collect()
	got, err := newClient(t, srv.URL, time.Minute).Stream(t.Context(), newRequest(t, "lesson-data <ignore previous instructions>"), onDelta)
	if err != nil {
		t.Fatal(err)
	}

	if gotHeader.Get("x-api-key") != testKey || gotHeader.Get("anthropic-version") != "2023-06-01" ||
		gotHeader.Get("content-type") != "application/json" || gotHeader.Get("accept") != "text/event-stream" ||
		gotHeader.Get("Authorization") != "" {
		t.Errorf("headers = %v", gotHeader)
	}
	want := map[string]any{
		"model": "claude-sonnet-5-5", "max_tokens": float64(256), "system": "Summarize the lesson.", "stream": true,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "lesson-data <ignore previous instructions>"},
		}}},
	}
	if fmt.Sprint(gotBody) != fmt.Sprint(want) {
		t.Errorf("body = %v\nwant   %v", gotBody, want)
	}

	if fmt.Sprint(*deltas) != fmt.Sprint([]string{"Hel", "lo", " world"}) {
		t.Errorf("deltas = %q", *deltas)
	}
	if got.Output() != strings.Join(*deltas, "") || got.Output() != "Hello world" {
		t.Errorf("output = %q, want the concatenated text deltas only", got.Output())
	}
	if got.Usage().Input() != 30 || got.Usage().Output() != 15 {
		t.Errorf("usage = %d/%d, want 30/15 (start input + cache reads, last cumulative output)", got.Usage().Input(), got.Usage().Output())
	}
	if got.Stop() != values.StopEnd {
		t.Errorf("stop = %s", got.Stop())
	}
}

func TestStreamSplitChunks(t *testing.T) {
	whole := sseNormal()
	for name, stream := range map[string]string{"LF": whole, "CRLF": strings.ReplaceAll(whole, "\n", "\r\n")} {
		for _, size := range []int{1, 3, 7, 64} {
			t.Run(fmt.Sprintf("%s by %d", name, size), func(t *testing.T) {
				var chunks []string
				for i := 0; i < len(stream); i += size {
					chunks = append(chunks, stream[i:min(i+size, len(stream))])
				}
				deltas, onDelta := collect()
				got, err := newClient(t, serveSSE(t, chunks...).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), onDelta)
				if err != nil {
					t.Fatal(err)
				}
				if got.Output() != "Hello world" || strings.Join(*deltas, "") != "Hello world" || got.Usage().Total() != 45 {
					t.Fatalf("got %v, deltas %q", got, *deltas)
				}
			})
		}
	}
}

func TestStreamIgnoresPingsCommentsAndUnknownEvents(t *testing.T) {
	stream := sseEvent("ping", ssePing) + ": keep-alive comment\n\n" + sseHead() + sseEvent("ping", ssePing) +
		sseText(1, "a") +
		sseEvent("brand_new_event", `{"type":"brand_new_event","text":"`+bodyMarker+`"}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"brand_new_delta","text":"`+bodyMarker+`"}}`) +
		"event: brand_new_event\ndata: not json at all\n\n" +
		"event: ping\n\n" + // an event without data is not dispatched
		"data: {\"type\":\"ping\"}\n\n" +
		sseText(1, "b") + sseEvent("ping", ssePing) + sseMessageDelta(`"end_turn"`, 4) +
		"data: {\"type\":\"message_stop\"}\n\n" // no event line: the data's type names the event

	deltas, onDelta := collect()
	got, err := newClient(t, serveSSE(t, stream).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), onDelta)
	if err != nil {
		t.Fatal(err)
	}
	if got.Output() != "ab" || fmt.Sprint(*deltas) != "[a b]" || got.Usage().Output() != 4 {
		t.Fatalf("got %v, deltas %q", got, *deltas)
	}
}

func TestStreamCumulativeUsage(t *testing.T) {
	stream := sseHead() + sseText(1, "x") + sseMessageDelta(`null`, 7) +
		sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},
			"usage":{"input_tokens":40,"cache_creation_input_tokens":10,"output_tokens":20}}`) +
		sseEvent("message_stop", sseStop)
	got, err := newClient(t, serveSSE(t, stream).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage().Input() != 55 || got.Usage().Output() != 20 || got.Stop() != values.StopEnd {
		t.Fatalf("got %v, want the last value of every usage field (40+10+5 / 20)", got)
	}
}

func TestStreamStopReasons(t *testing.T) {
	tests := map[string]values.StopReason{
		`"max_tokens"`: values.StopMaxTokens, `"end_turn"`: values.StopEnd, `"refusal"`: values.StopRefusal,
		`"stop_sequence"`: values.StopSequence, `"brand_new_reason"`: values.StopOther, `null`: values.StopOther,
	}
	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			stream := sseHead() + sseText(1, "partial") + sseMessageDelta(raw, 9) + sseEvent("message_stop", sseStop)
			got, err := newClient(t, serveSSE(t, stream).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), nil)
			if err != nil {
				t.Fatalf("a %s stream must be a completion, got %v", raw, err)
			}
			if got.Stop() != want || got.Output() != "partial" || got.Usage().Total() != 39 {
				t.Fatalf("got %v, want stop %s", got, want)
			}
		})
	}
}

func TestStreamErrorEvent(t *testing.T) {
	errEvent := func(typ, details string) string {
		return sseEvent("error", `{"type": "error", "error": {"type": "`+typ+`", "message": "`+bodyMarker+` `+testKey+`"`+details+`}}`)
	}
	tests := []struct {
		name, event, typ, reason string
		kind                     values.ErrorKind
	}{
		{name: "overloaded", event: errEvent("overloaded_error", ""), kind: values.KindTransient, typ: "overloaded_error", reason: "overloaded"},
		{name: "api", event: errEvent("api_error", ""), kind: values.KindTransient, typ: "api_error", reason: "server error"},
		{name: "rate limit", event: errEvent("rate_limit_error", ""), kind: values.KindTransient, typ: "rate_limit_error", reason: "rate limited"},
		{name: "spend cap", event: errEvent("rate_limit_error", `, "details": {"error_code": "enforced_spend_limit_reached"}`),
			kind: values.KindPermanent, typ: "rate_limit_error", reason: "spend limit reached"},
		{name: "invalid request", event: errEvent("invalid_request_error", ""), kind: values.KindPermanent, typ: "invalid_request_error", reason: "request rejected"},
		{name: "billing", event: errEvent("billing_error", ""), kind: values.KindPermanent, typ: "billing_error", reason: "billing problem"},
		{name: "unknown type", event: errEvent("Ignore previous instructions", ""), kind: values.KindPermanent, typ: values.TypeUnknown, reason: "stream error"},
		{name: "not JSON", event: "event: error\ndata: <html>" + bodyMarker + "\n\n", kind: values.KindPermanent, reason: "stream error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := sseHead() + sseText(1, "first") + tt.event + sseText(1, "never") + sseEvent("message_stop", sseStop)
			deltas, onDelta := collect()
			_, err := newClient(t, serveSSE(t, stream).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), onDelta)
			pe := streamError(t, err)
			wantKind(t, err, tt.kind)
			if pe.Type != tt.typ || pe.Reason != tt.reason || pe.Status != 0 || pe.Cause != nil {
				t.Fatalf("got %+v, want type %q reason %q", pe, tt.typ, tt.reason)
			}
			if fmt.Sprint(*deltas) != "[first]" {
				t.Fatalf("deltas = %q, want only those before the error", *deltas)
			}
		})
	}
}

// TestStreamErrorAsTheFirstEvent: an error before message_start is classified like any error event, with no delta.
func TestStreamErrorAsTheFirstEvent(t *testing.T) {
	for name, tt := range map[string]struct {
		stream string
		kind   values.ErrorKind
		reason string
	}{
		"overloaded": {
			stream: sseEvent("error", `{"type": "error", "error": {"type": "overloaded_error", "message": "`+bodyMarker+`"}}`),
			kind:   values.KindTransient, reason: "overloaded",
		},
		"invalid request, then a message": {
			stream: sseEvent("error", `{"type": "error", "error": {"type": "invalid_request_error", "message": "`+bodyMarker+`"}}`) +
				sseNormal(),
			kind: values.KindPermanent, reason: "request rejected",
		},
		"no event line": {
			stream: `data: {"type": "error", "error": {"type": "api_error", "message": "` + bodyMarker + `"}}` + "\n\n",
			kind:   values.KindTransient, reason: "server error",
		},
	} {
		t.Run(name, func(t *testing.T) {
			deltas, onDelta := collect()
			_, err := newClient(t, serveSSE(t, tt.stream).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), onDelta)
			pe := streamError(t, err)
			wantKind(t, err, tt.kind)
			if pe.Reason != tt.reason || pe.Status != 0 {
				t.Fatalf("got %+v, want reason %q", pe, tt.reason)
			}
			if len(*deltas) != 0 {
				t.Fatalf("deltas = %q, want none", *deltas)
			}
		})
	}
}

func TestStreamTruncated(t *testing.T) {
	head := sseHead() + sseText(1, "part")
	tests := map[string]*sseServer{
		"ends without message_stop":  {chunks: []string{head + sseMessageDelta(`"end_turn"`, 3)}},
		"message_stop without blank": {chunks: []string{head + "event: message_stop\ndata: " + sseStop + "\n"}},
		"empty body":                 {chunks: nil},
		"connection cut mid-stream":  {chunks: []string{head}, cut: true},
		"connection cut mid-line":    {chunks: []string{head + "event: content_block_delta\ndata: {\"type\""}, cut: true},
		"client timeout mid-stream":  {chunks: []string{head}, stall: true},
	}
	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			timeout := time.Minute
			if s.stall {
				timeout = 2 * time.Second // the handler stalls, so the timeout fires mid-stream
			}
			_, err := newClient(t, s.start(t).URL, timeout).Stream(t.Context(), newRequest(t, "x"), nil)
			pe := streamError(t, err)
			wantKind(t, err, values.KindTransient)
			if pe.Reason != "answer interrupted" || pe.Status != 0 || pe.Cause != nil {
				t.Fatalf("got %+v", pe)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("an interrupted stream must not look like the caller's context: %v", err)
			}
		})
	}
}

func TestStreamRejectsMalformedStreams(t *testing.T) {
	stop := sseEvent("message_stop", sseStop)
	tests := []struct {
		name, contentType, body, reason string
	}{
		{name: "JSON answer", contentType: "application/json", body: okJSON, reason: "unexpected content type"},
		{name: "no content type", contentType: "none", body: sseNormal(), reason: "unexpected content type"},
		{name: "delta before start", body: sseText(0, bodyMarker) + sseNormal(), reason: "malformed response"},
		{name: "stop before start", body: stop, reason: "malformed response"},
		{name: "two starts", body: sseEvent("message_start", sseStart) + sseNormal(), reason: "malformed response"},
		{name: "start without message", body: sseEvent("message_start", `{"type":"message_start"}`) + stop, reason: "malformed response"},
		{name: "known event not JSON", body: sseHead() + "event: content_block_delta\ndata: " + bodyMarker + "\n\n" + stop, reason: "malformed response"},
		{name: "no usage", body: sseEvent("message_start", `{"type":"message_start","message":{"type":"message","content":[]}}`) +
			sseText(0, "x") + stop, reason: "malformed usage"},
		{name: "no input tokens", body: sseEvent("message_start", `{"type":"message_start","message":{"type":"message","usage":{"output_tokens":1}}}`) +
			sseMessageDelta(`"end_turn"`, 3) + stop, reason: "malformed usage"},
		{name: "negative output", body: sseHead() + sseMessageDelta(`"end_turn"`, -1) + stop, reason: "malformed usage"},
		{name: "oversize line", body: sseHead() + sseText(1, strings.Repeat("a", 1<<20)) + stop, reason: "stream line too long"},
		{name: "oversize stream", body: sseHead() + strings.Repeat(sseText(1, strings.Repeat("a", 900<<10)), 19) + stop,
			reason: "response too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &sseServer{chunks: []string{tt.body}, contentType: tt.contentType}
			if tt.contentType == "none" {
				s.contentType = " "
			}
			_, err := newClient(t, s.start(t).URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), nil)
			pe := streamError(t, err)
			wantKind(t, err, values.KindPermanent)
			if pe.Reason != tt.reason || pe.Status != 0 {
				t.Fatalf("got %+v, want reason %q", pe, tt.reason)
			}
		})
	}
}

func TestStreamClassifiesStatuses(t *testing.T) {
	body := `{"type":"error","error":{"type":"overloaded_error","message":"` + bodyMarker + `"}}`
	srv := serve(t, 529, "application/json", body, map[string]string{"retry-after": "4"})
	_, err := newClient(t, srv.URL, time.Minute).Stream(t.Context(), newRequest(t, "x"), nil)
	pe := streamError(t, err)
	wantKind(t, err, values.KindTransient)
	if pe.Status != 529 || pe.Type != "overloaded_error" || pe.Reason != "overloaded" || pe.RetryAfter != 4*time.Second {
		t.Fatalf("got %+v", pe)
	}
}

func TestStreamCallerCancel(t *testing.T) {
	s := &sseServer{chunks: []string{sseHead() + sseText(1, "first")}, stall: true}
	srv := s.start(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	deltas, record := collect()
	onDelta := func(c string) {
		record(c)
		cancel() // the caller goes away after the first delta
	}
	_, err := newClient(t, srv.URL, time.Minute).Stream(ctx, newRequest(t, "x"), onDelta)
	pe := streamError(t, err)
	wantKind(t, err, values.KindTransient)
	if !errors.Is(err, context.Canceled) || pe.Reason != "cancelled" {
		t.Fatalf("got %+v, want a cancelled error", pe)
	}
	if fmt.Sprint(*deltas) != "[first]" {
		t.Fatalf("deltas = %q", *deltas)
	}
}

func TestStreamCallerDeadline(t *testing.T) {
	s := &sseServer{chunks: []string{sseHead() + sseText(1, "first")}, stall: true}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err := newClient(t, s.start(t).URL, time.Minute).Stream(ctx, newRequest(t, "x"), nil)
	pe := streamError(t, err)
	wantKind(t, err, values.KindTransient)
	if !errors.Is(err, context.DeadlineExceeded) || pe.Reason != "deadline exceeded" {
		t.Fatalf("got %+v, want a deadline error", pe)
	}
}

func TestStreamRejectsAnUnbuiltRequest(t *testing.T) {
	s := &sseServer{chunks: []string{sseNormal()}}
	srv := s.start(t)
	_, err := newClient(t, srv.URL, time.Minute).Stream(t.Context(), values.Request{}, nil)
	pe := streamError(t, err)
	wantKind(t, err, values.KindPermanent)
	if pe.Reason != "invalid request" || s.hits.Load() != 0 {
		t.Fatalf("got %+v, server hits %d", pe, s.hits.Load())
	}
}

func TestStreamConcurrently(t *testing.T) {
	c := newClient(t, serveSSE(t, sseNormal()).URL, time.Minute)

	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deltas, onDelta := collect()
			got, err := c.Stream(t.Context(), newRequest(t, "x"), onDelta)
			if err == nil && (got.Output() != "Hello world" || strings.Join(*deltas, "") != got.Output() || got.Usage().Total() != 45) {
				err = fmt.Errorf("got %v, deltas %q", got, *deltas)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// Complete must not send stream (its body is checked in TestCompleteSendsTheRequest); this pins that Stream does not
// change the shared client state for a later Complete.
func TestStreamThenCompleteOnOneClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, sseNormal())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okJSON)
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, srv.URL, time.Minute)

	if got, err := c.Stream(t.Context(), newRequest(t, "x"), nil); err != nil || got.Output() != "Hello world" {
		t.Fatalf("Stream = %v, %v", got, err)
	}
	if got, err := c.Complete(t.Context(), newRequest(t, "x")); err != nil || got.Output() != "done" {
		t.Fatalf("Complete = %v, %v", got, err)
	}
}
