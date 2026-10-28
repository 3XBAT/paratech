package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"paratech/hub/internal/domain/llm/values"
)

// Block and delta types a planner stream assembles (docs/refs/anthropic.md section 8).
const (
	blockThinking  = "thinking"
	deltaInputJSON = "input_json_delta"
	deltaThinking  = "thinking_delta"
	deltaSignature = "signature_delta"
	eventBlockStop = "content_block_stop"
	draftSeparator = "\n\n"
)

// StepStream is Step with "stream": true: the same request, deadline and error rules, and the same answer, its kept
// raw turn assembled from the events (thinking with its signature, text, tool_use with its parsed input). onDelta
// (may be nil) gets the visible reply as the model writes it, in order, from the calling goroutine, before StepStream
// returns: the top-level text argument of every reply call and the text blocks, a later part after a blank line. No
// other tool argument and no thinking is ever passed to it. The chunks are untrusted draft text and may differ from
// the answer's calls. A stream that ends or breaks before message_stop is the transient "answer interrupted" (probably
// billed), unless ctx ended it. A 200 JSON answer is read as Step reads it, with no chunk (TASK-190 fallback).
func (c *Client) StepStream(ctx context.Context, call values.AgentCall, onDelta func(chunk string)) (values.AgentAnswer, error) {
	if call.Model() == "" || call.System() == "" || call.MaxOutput() <= 0 || len(call.Tools()) == 0 || len(call.Turns()) == 0 {
		return values.AgentAnswer{}, failStream(permanent("invalid request"))
	}
	wire := newAgentWireRequest(call)
	wire.Stream = true
	body, err := json.Marshal(wire)
	if err != nil {
		return values.AgentAnswer{}, failStream(permanent("request encoding failed"))
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}

	var answer values.AgentAnswer
	err = c.post(ctx, body, contentTypeSSE, func(resp *http.Response) error {
		var rerr error
		if isJSON(resp) {
			c.noSSE.Do(func() { c.log.Warn("provider answered without SSE") })
			answer, rerr = readAgentAnswer(resp)
			return rerr
		}
		st := &agentStream{onDelta: onDelta}
		if rerr = readSSE(resp, st.dispatch); rerr != nil {
			return rerr
		}
		answer, rerr = st.answer()
		return rerr
	})
	if err != nil {
		return values.AgentAnswer{}, failStream(err)
	}
	return answer, nil
}

// agentStreamEvent holds the fields of the documented events that StepStream reads.
type agentStreamEvent struct {
	Index   *int `json:"index"`
	Message *struct {
		Type  string     `json:"type"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock json.RawMessage `json:"content_block"`
	Delta        *struct {
		Type        string  `json:"type"`
		Text        string  `json:"text"`
		PartialJSON string  `json:"partial_json"`
		Thinking    string  `json:"thinking"`
		Signature   string  `json:"signature"`
		StopReason  *string `json:"stop_reason"`
	} `json:"delta"`
	Usage *wireUsage `json:"usage"`
}

// streamBlock is one content block being assembled: the fields of its content_block_start, with the deltas of its
// type appended.
type streamBlock struct {
	fields    map[string]json.RawMessage
	typ       string
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     strings.Builder
	hasInput  bool
	reply     *replyText // set for a reply tool_use
	shown     bool       // a chunk of this block went to onDelta
}

// agentStream builds a planner answer from the events.
type agentStream struct {
	onDelta func(string)
	started bool
	shown   bool
	usage   wireUsage
	stop    *string
	blocks  []*streamBlock
}

// dispatch applies one event and reports whether it was message_stop; ping and unknown events are ignored.
func (st *agentStream) dispatch(event string, data []byte) (bool, error) {
	switch event {
	case eventError:
		return false, streamError(data)
	case eventMessageStart, eventBlockStart, eventBlockDelta, eventBlockStop, eventMessageDelta, eventMessageStop:
	default:
		return false, nil
	}
	var e agentStreamEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return false, permanent("malformed response")
	}
	if event == eventMessageStart {
		if st.started || e.Message == nil || e.Message.Type != responseTypeMessage {
			return false, permanent("malformed response")
		}
		st.started = true
		mergeUsage(&st.usage, e.Message.Usage)
		return false, nil
	}
	if !st.started {
		return false, permanent("malformed response")
	}

	switch event {
	case eventBlockStart:
		return false, st.start(e)
	case eventBlockDelta:
		return false, st.delta(e)
	case eventMessageDelta:
		if e.Delta != nil && e.Delta.StopReason != nil {
			st.stop = e.Delta.StopReason
		}
		mergeUsage(&st.usage, e.Usage)
	case eventMessageStop:
		return true, nil
	}
	return false, nil // content_block_stop: the block is complete, nothing to do
}

// start opens the block at the next index.
func (st *agentStream) start(e agentStreamEvent) error {
	if e.Index == nil || *e.Index != len(st.blocks) {
		return permanent("malformed response")
	}
	b := &streamBlock{}
	if json.Unmarshal(e.ContentBlock, &b.fields) != nil || b.fields == nil {
		return permanent("malformed response")
	}
	b.typ = stringField(b.fields, "type")
	st.blocks = append(st.blocks, b)
	switch b.typ {
	case blockText:
		text := stringField(b.fields, "text")
		b.text.WriteString(text)
		st.show(b, draftText(text))
	case blockThinking:
		b.thinking.WriteString(stringField(b.fields, "thinking"))
		b.signature.WriteString(stringField(b.fields, "signature"))
	case blockToolUse:
		if stringField(b.fields, "name") == values.ReplyTool {
			b.reply = &replyText{}
		}
	}
	return nil
}

// delta appends a delta to its block; a delta of another type than the block's is ignored.
func (st *agentStream) delta(e agentStreamEvent) error {
	if e.Index == nil || *e.Index < 0 || *e.Index >= len(st.blocks) || e.Delta == nil {
		return permanent("malformed response")
	}
	b := st.blocks[*e.Index]
	switch {
	case e.Delta.Type == deltaText && b.typ == blockText:
		b.text.WriteString(e.Delta.Text)
		st.show(b, draftText(e.Delta.Text))
	case e.Delta.Type == deltaThinking && b.typ == blockThinking:
		b.thinking.WriteString(e.Delta.Thinking)
	case e.Delta.Type == deltaSignature && b.typ == blockThinking:
		b.signature.WriteString(e.Delta.Signature)
	case e.Delta.Type == deltaInputJSON && b.typ == blockToolUse:
		b.input.WriteString(e.Delta.PartialJSON)
		b.hasInput = true
		if b.reply != nil {
			st.show(b, b.reply.Feed(e.Delta.PartialJSON))
		}
	}
	return nil
}

// show passes a non-empty chunk of b on; the first chunk of a later block starts with a blank line.
func (st *agentStream) show(b *streamBlock, chunk string) {
	if chunk == "" {
		return
	}
	if !b.shown && st.shown {
		chunk = draftSeparator + chunk
	}
	b.shown, st.shown = true, true
	st.onDelta(chunk)
}

// answer is the answer of the complete message, read like a unary answer from the assembled content.
func (st *agentStream) answer() (values.AgentAnswer, error) {
	content, err := st.content()
	if err != nil {
		return values.AgentAnswer{}, err
	}
	return agentAnswerOf(content, st.stop, &st.usage)
}

// content is the content array as a unary answer carries it. An input that is not JSON becomes null, so the call is
// malformed (billed) as in a unary answer; fields of other block types stay as they started.
func (st *agentStream) content() (json.RawMessage, error) {
	var out bytes.Buffer
	out.WriteByte('[')
	for i, b := range st.blocks {
		switch b.typ {
		case blockText:
			b.fields["text"] = jsonString(b.text.String())
		case blockThinking:
			b.fields["thinking"] = jsonString(b.thinking.String())
			b.fields["signature"] = jsonString(b.signature.String())
		case blockToolUse:
			if b.hasInput {
				in := strings.TrimSpace(b.input.String())
				switch {
				case in == "":
					in = "{}"
				case !json.Valid([]byte(in)):
					in = "null"
				}
				b.fields["input"] = json.RawMessage(in)
			}
		}
		raw, err := marshalNoEscape(b.fields)
		if err != nil {
			return nil, permanent("malformed response")
		}
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(raw)
	}
	out.WriteByte(']')
	return out.Bytes(), nil
}

// stringField is the string value of key in fields, "" when it is missing or not a string.
func stringField(fields map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := fields[key]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func jsonString(s string) json.RawMessage {
	raw, _ := marshalNoEscape(s) // a Go string always encodes
	return raw
}

// marshalNoEscape is json.Marshal without HTML escaping, so texts keep their bytes.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}
