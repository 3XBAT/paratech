package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"paratech/hub/internal/domain/llm/values"
)

// Stream limits (our choice; none is documented). SSE framing adds roughly 100 bytes per delta, so the total cap is
// twice the unary one; one event line holds one small delta or an empty message, far below the line cap.
const (
	maxStreamBytes     = 2 * maxResponseBytes
	maxStreamLineBytes = 1 << 20
	contentTypeSSE     = "text/event-stream"
)

// SSE event names of docs/refs/anthropic.md section 8.
const (
	eventMessageStart = "message_start"
	eventBlockStart   = "content_block_start"
	eventBlockDelta   = "content_block_delta"
	eventMessageDelta = "message_delta"
	eventMessageStop  = "message_stop"
	eventError        = "error"
	deltaText         = "text_delta"
)

// errStreamTooLarge marks a stream longer than maxStreamBytes.
var errStreamTooLarge = errors.New("stream too large")

// Stream is Complete with "stream": true: the same request, deadline and error rules. onDelta (may be nil) gets every
// non-empty text delta in order, from the calling goroutine, before Stream returns; their concatenation is the
// output. An error event is classified by its error.type; a stream that ends or breaks before message_stop is the
// transient "answer interrupted" (probably billed), unless ctx ended it.
func (c *Client) Stream(ctx context.Context, req values.Request, onDelta func(chunk string)) (values.Completion, error) {
	if req.Model() == "" || req.Instructions() == "" || req.MaxOutput() <= 0 {
		return values.Completion{}, failStream(permanent("invalid request"))
	}
	wire := newWireRequest(req)
	wire.Stream = true
	body, err := json.Marshal(wire)
	if err != nil {
		return values.Completion{}, failStream(permanent("request encoding failed"))
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}

	var completion values.Completion
	err = c.post(ctx, body, contentTypeSSE, func(resp *http.Response) error {
		var rerr error
		completion, rerr = readStream(resp, onDelta)
		return rerr
	})
	if err != nil {
		return values.Completion{}, failStream(err)
	}
	return completion, nil
}

// streamEvent holds the fields of the documented events that Stream reads.
type streamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		Type  string     `json:"type"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta *struct {
		Type       string  `json:"type"`
		Text       string  `json:"text"`
		StopReason *string `json:"stop_reason"`
	} `json:"delta"`
	Usage *wireUsage `json:"usage"`
}

// streamState builds the completion from the events.
type streamState struct {
	onDelta func(string)
	started bool
	text    strings.Builder
	usage   wireUsage
	stop    *string
}

// readStream reads the SSE body line by line (docs/refs/anthropic.md section 8). A read failure or an end before
// message_stop is errRead; a malformed or oversized stream is permanent.
func readStream(resp *http.Response, onDelta func(string)) (values.Completion, error) {
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != contentTypeSSE {
		return values.Completion{}, permanent("unexpected content type")
	}
	sc := bufio.NewScanner(&cappedReader{r: resp.Body, left: maxStreamBytes})
	sc.Buffer(make([]byte, 0, 64<<10), maxStreamLineBytes)

	st := &streamState{onDelta: onDelta}
	var (
		event   string
		data    bytes.Buffer
		hasData bool
	)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 { // a blank line dispatches the event
			if hasData {
				done, err := st.dispatch(event, data.Bytes())
				if err != nil {
					return values.Completion{}, err
				}
				if done {
					return st.result()
				}
			}
			event, hasData = "", false
			data.Reset()
			continue
		}
		field, value := sseField(line)
		switch field {
		case "event":
			event = string(value)
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.Write(value)
			hasData = true
		}
	}

	err := sc.Err()
	switch {
	case errors.Is(err, bufio.ErrTooLong):
		return values.Completion{}, permanent("stream line too long")
	case errors.Is(err, errStreamTooLarge):
		return values.Completion{}, permanent("response too large")
	}
	return values.Completion{}, errRead // broken, or ended without message_stop
}

// sseField splits an SSE line into field and value (one leading space of the value dropped); a comment line (":")
// has an empty field.
func sseField(line []byte) (string, []byte) {
	name, value, found := bytes.Cut(line, []byte(":"))
	if !found {
		return string(line), nil
	}
	return string(name), bytes.TrimPrefix(value, []byte(" "))
}

// dispatch applies one event and reports whether it was message_stop. ping and unknown events are ignored.
func (st *streamState) dispatch(event string, data []byte) (bool, error) {
	switch event {
	case "":
		// No event line: the data's type names the event (the docs always send both).
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &head) != nil || head.Type == "" {
			return false, nil
		}
		return st.dispatch(head.Type, data)
	case eventError:
		return false, streamError(data)
	case eventMessageStart, eventBlockStart, eventBlockDelta, eventMessageDelta, eventMessageStop:
	default:
		return false, nil
	}

	var e streamEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return false, permanent("malformed response")
	}
	if event == eventMessageStart {
		if st.started || e.Message == nil || e.Message.Type != responseTypeMessage {
			return false, permanent("malformed response")
		}
		st.started = true
		st.mergeUsage(e.Message.Usage)
		return false, nil
	}
	if !st.started {
		return false, permanent("malformed response")
	}

	switch event {
	case eventBlockStart:
		if e.ContentBlock != nil && e.ContentBlock.Type == blockText {
			st.emit(e.ContentBlock.Text)
		}
	case eventBlockDelta:
		if e.Delta != nil && e.Delta.Type == deltaText { // thinking, signature and tool deltas are not output
			st.emit(e.Delta.Text)
		}
	case eventMessageDelta:
		if e.Delta != nil && e.Delta.StopReason != nil {
			st.stop = e.Delta.StopReason
		}
		st.mergeUsage(e.Usage)
	case eventMessageStop:
		return true, nil
	}
	return false, nil
}

func (st *streamState) emit(chunk string) {
	if chunk == "" {
		return
	}
	st.text.WriteString(chunk)
	st.onDelta(chunk)
}

// mergeUsage keeps the latest value of every field: message_delta counts are cumulative.
func (st *streamState) mergeUsage(u *wireUsage) {
	if u == nil {
		return
	}
	for _, f := range []struct{ dst, src **int64 }{
		{&st.usage.InputTokens, &u.InputTokens},
		{&st.usage.OutputTokens, &u.OutputTokens},
		{&st.usage.CacheCreationInputTokens, &u.CacheCreationInputTokens},
		{&st.usage.CacheReadInputTokens, &u.CacheReadInputTokens},
	} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
}

func (st *streamState) result() (values.Completion, error) {
	usage, ok := st.usage.toUsage()
	if !ok {
		return values.Completion{}, permanent("malformed usage")
	}
	completion, err := values.NewCompletion(st.text.String(), usage, stopReason(st.stop))
	if err != nil {
		return values.Completion{}, permanent("malformed response")
	}
	return completion, nil
}

// streamError classifies an error event by error.type like the matching HTTP status of statusError; the HTTP status
// was 200, so Status stays 0. Only known types and fixed texts go into the error.
func streamError(data []byte) *values.ProviderError {
	var body wireError
	if json.Unmarshal(data, &body) != nil || body.Error == nil {
		return permanent("stream error")
	}
	e := &values.ProviderError{Kind: values.KindPermanent, Type: values.TypeUnknown}
	if knownErrorTypes[body.Error.Type] {
		e.Type = body.Error.Type
	}
	spendLimit := body.Error.Details != nil && body.Error.Details.ErrorCode == spendLimitCode

	switch e.Type {
	case "rate_limit_error":
		if spendLimit {
			e.Reason = "spend limit reached"
		} else {
			e.Kind, e.Reason = values.KindTransient, "rate limited"
		}
	case "overloaded_error":
		e.Kind, e.Reason = values.KindTransient, "overloaded"
	case "api_error", "timeout_error":
		e.Kind, e.Reason = values.KindTransient, "server error"
	case "authentication_error", "permission_error":
		e.Reason = "credentials rejected"
	case "billing_error":
		e.Reason = "billing problem"
	case "request_too_large":
		e.Reason = "request too large"
	case values.TypeUnknown:
		e.Reason = "stream error"
	default:
		e.Reason = "request rejected"
	}
	return e
}

// cappedReader returns errStreamTooLarge once more than left bytes arrive.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var one [1]byte
		n, err := c.r.Read(one[:])
		if n > 0 {
			return 0, errStreamTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}
