package anthropic_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

func sseBlockStart(index int, block string) string {
	return sseEvent("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, index, block))
}

func sseInput(index int, partial string) string {
	raw, _ := json.Marshal(partial)
	return sseEvent("content_block_delta",
		fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, index, raw))
}

func sseThinking(index int, text string) string {
	raw, _ := json.Marshal(text)
	return sseEvent("content_block_delta",
		fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"thinking_delta","thinking":%s}}`, index, raw))
}

func sseSig(index int, sig string) string {
	return sseEvent("content_block_delta",
		fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":%q}}`, index, sig))
}

const (
	thinkingBlock = `{"type":"thinking","thinking":"","signature":""}`
	textBlock     = `{"type":"text","text":""}`
	startEvent    = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-haiku-5-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":20,"output_tokens":1,"cache_creation_input_tokens":1000,"cache_read_input_tokens":3000}}}`
)

func toolBlock(id, name string) string {
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":{}}`, id, name)
}

// sseToolUse is toolUseJSON as a stream: the same content, stop reason and usage, every part in pieces.
func sseToolUse() string {
	return sseEvent("message_start", startEvent) + sseEvent("ping", ssePing) +
		sseBlockStart(0, thinkingBlock) + sseThinking(0, "..") + sseThinking(0, ".") + sseSig(0, "x") + sseBlockStop(0) +
		sseBlockStart(1, textBlock) + sseText(1, "igno") + sseText(1, "red") + sseBlockStop(1) +
		sseBlockStart(2, toolBlock("toolu_01A", "find_lessons")) + sseInput(2, "") + sseInput(2, `{"subject":"subj-1",`) +
		sseInput(2, `"kind":"lecture","n":1.50}`) + sseBlockStop(2) +
		sseBlockStart(3, toolBlock("toolu_01B", "reply")) + sseInput(3, `{"te`) + sseInput(3, `xt":"a <b>`) +
		sseInput(3, ` & c"}`) + sseBlockStop(3) +
		sseMessageDelta(`"tool_use"`, 7) + sseEvent("message_stop", sseStop)
}

func decode(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return v
}

// TestStepStreamReadsTheAnswerLikeStep: the streamed answer has the calls, usage, stop reason and kept turn of the
// unary answer with the same content; the request asks for a stream.
func TestStepStreamReadsTheAnswerLikeStep(t *testing.T) {
	var rec recorder
	unary, err := newClient(t, rec.serve(t, http.StatusOK, toolUseJSON).URL, time.Minute).Step(t.Context(), agentCall(t))
	if err != nil {
		t.Fatal(err)
	}

	var body []byte
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseToolUse())
	}))
	t.Cleanup(srv.Close)
	got, onDelta := collect()
	streamed, err := newClient(t, srv.URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil || streamed.Failure() != nil {
		t.Fatalf("answer %v, %v", streamed, err)
	}
	if !reflect.DeepEqual(streamed.Calls(), unary.Calls()) || streamed.Usage() != unary.Usage() || streamed.Stop() != unary.Stop() {
		t.Fatalf("streamed %v %v %v, unary %v %v %v", streamed.Calls(), streamed.Usage(), streamed.Stop(), unary.Calls(), unary.Usage(), unary.Stop())
	}
	if !reflect.DeepEqual(decode(t, streamed.Assistant().Raw()), decode(t, unary.Assistant().Raw())) {
		t.Fatalf("kept turn\n%s\nwant\n%s", streamed.Assistant().Raw(), unary.Assistant().Raw())
	}
	// Only the reply text: never the preamble text block, the other call's arguments or the thinking.
	if strings.Join(*got, "") != "a <b> & c" {
		t.Fatalf("deltas %q", *got)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil || req["stream"] != true || req["tool_choice"] == nil || accept != "text/event-stream" {
		t.Fatalf("request %s, accept %q", body, accept)
	}
	// The unary request is the same without the stream field.
	delete(req, "stream")
	if !reflect.DeepEqual(req, rec.last(t)) {
		t.Fatalf("streamed request differs from the unary one")
	}
}

// TestStepStreamDraftIsOnlyTheReply: a text argument of another tool and the thinking never reach onDelta; the reply
// text comes in the pieces it was written in.
func TestStepStreamDraftIsOnlyTheReply(t *testing.T) {
	stream := sseEvent("message_start", startEvent) +
		sseBlockStart(0, thinkingBlock) + sseThinking(0, "secret-thinking") + sseSig(0, "sig") + sseBlockStop(0) +
		sseBlockStart(1, toolBlock("toolu_1", "create_note")) + sseInput(1, `{"text":"secret-arg"}`) + sseBlockStop(1) +
		sseBlockStart(2, toolBlock("toolu_2", "reply")) + sseInput(2, `{"text":"Пер`) + sseInput(2, `вая\n`) +
		sseInput(2, `часть","card_ids":["secret-id"]}`) + sseBlockStop(2) +
		sseMessageDelta(`"tool_use"`, 7) + sseEvent("message_stop", sseStop)
	got, onDelta := collect()
	answer, err := newClient(t, serveSSE(t, stream).URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil || answer.Failure() != nil || len(answer.Calls()) != 2 {
		t.Fatalf("answer %v, %v", answer, err)
	}
	if want := []string{"Пер", "вая\n", "часть"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("deltas %q, want %q", *got, want)
	}
	if c := answer.Calls()[1]; c.Arguments() != `{"card_ids":["secret-id"],"text":"Первая\nчасть"}` {
		t.Fatalf("reply arguments %s", c.Arguments())
	}
}

// TestStepStreamTextOnlyAnswer: the text of an answer without calls never streams; it is the answer's plain text.
func TestStepStreamTextOnlyAnswer(t *testing.T) {
	stream := sseEvent("message_start", startEvent) + sseBlockStart(0, thinkingBlock) + sseSig(0, "s") + sseBlockStop(0) +
		sseBlockStart(1, `{"type":"text","text":"Сей"}`) + sseText(1, "час ") + sseText(1, "отвечу.") + sseBlockStop(1) +
		sseMessageDelta(`"end_turn"`, 9) + sseEvent("message_stop", sseStop)
	got, onDelta := collect()
	answer, err := newClient(t, serveSSE(t, stream).URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil {
		t.Fatal(err)
	}
	text, ok := answer.PlainText()
	if !ok || text != "Сейчас отвечу." || len(*got) != 0 || answer.Stop() != values.StopEnd {
		t.Fatalf("text %q %v, deltas %q, stop %v", text, ok, *got, answer.Stop())
	}
}

// TestStepStreamPreambleIsNotStreamed: a preamble text block before the calls never streams; the reply text does, and
// a second reply call of the step starts after a blank line. The kept text block is in the answer's turn.
func TestStepStreamPreambleIsNotStreamed(t *testing.T) {
	stream := sseEvent("message_start", startEvent) +
		sseBlockStart(0, `{"type":"text","text":"Сейчас "}`) + sseText(0, "посмотрю расписание.") + sseBlockStop(0) +
		sseBlockStart(1, toolBlock("toolu_1", "reply")) + sseInput(1, `{"text":"Первый"}`) + sseBlockStop(1) +
		sseBlockStart(2, textBlock) + sseText(2, "между") + sseBlockStop(2) +
		sseBlockStart(3, toolBlock("toolu_2", "reply")) + sseInput(3, `{"text":"Второй"}`) + sseBlockStop(3) +
		sseMessageDelta(`"tool_use"`, 7) + sseEvent("message_stop", sseStop)
	got, onDelta := collect()
	answer, err := newClient(t, serveSSE(t, stream).URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil || answer.Failure() != nil || len(answer.Calls()) != 2 {
		t.Fatalf("answer %v, %v", answer, err)
	}
	if want := []string{"Первый", "\n\nВторой"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("deltas %q, want %q", *got, want)
	}
	if raw := answer.Assistant().Raw(); !strings.Contains(raw, "Сейчас посмотрю расписание.") {
		t.Fatalf("kept turn %s", raw)
	}
}

// TestStepStreamMalformedInputIsABilledAnswer: an input that is not JSON makes a malformed answer with its usage, as
// in a unary answer.
func TestStepStreamMalformedInputIsABilledAnswer(t *testing.T) {
	stream := sseEvent("message_start", startEvent) + sseBlockStart(0, toolBlock("toolu_1", "reply")) +
		sseInput(0, `{"text":"a"`) + sseBlockStop(0) + sseMessageDelta(`"tool_use"`, 7) + sseEvent("message_stop", sseStop)
	got, onDelta := collect()
	answer, err := newClient(t, serveSSE(t, stream).URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil || answer.Failure() == nil || answer.Usage().Output() != 7 {
		t.Fatalf("answer %v, %v", answer, err)
	}
	if strings.Join(*got, "") != "a" {
		t.Fatalf("deltas %q", *got)
	}
}

// TestStepStreamFailures: an end before message_stop is "answer interrupted", an error event is classified, broken
// events are permanent; every error is a ProviderError of anthropic.Stream that leaks no text.
func TestStepStreamFailures(t *testing.T) {
	head := sseEvent("message_start", startEvent) + sseBlockStart(0, toolBlock("toolu_1", "reply")) + sseInput(0, `{"text":"secret`)
	cases := map[string]struct {
		stream string
		kind   values.ErrorKind
		reason string
	}{
		"truncated":   {head, values.KindTransient, "answer interrupted"},
		"overloaded":  {head + sseEvent("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), values.KindTransient, "overloaded"},
		"bad index":   {sseEvent("message_start", startEvent) + sseBlockStart(1, textBlock), values.KindPermanent, "malformed response"},
		"no start":    {sseBlockStart(0, textBlock), values.KindPermanent, "malformed response"},
		"bad delta":   {head + sseInput(3, `x`), values.KindPermanent, "malformed response"},
		"no usage":    {strings.Replace(head, `"usage"`, `"x"`, 1) + sseBlockStop(0) + sseEvent("message_stop", sseStop), values.KindPermanent, "malformed usage"},
		"block !json": {sseEvent("message_start", startEvent) + sseBlockStart(0, `"text"`), values.KindPermanent, "malformed response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newClient(t, serveSSE(t, tc.stream).URL, time.Minute).StepStream(t.Context(), agentCall(t), nil)
			pe := streamError(t, err)
			if pe.Kind != tc.kind || pe.Reason != tc.reason {
				t.Fatalf("error %+v, want %v %q", pe, tc.kind, tc.reason)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error %q leaks text", err)
			}
		})
	}
}

// TestStepStreamReadsAJSONAnswer: a gateway that answers the stream with JSON gets the unary answer and no chunk; one
// paid call.
func TestStepStreamReadsAJSONAnswer(t *testing.T) {
	srv, hits, _ := jsonServer(t, toolUseJSON)
	got, onDelta := collect()
	answer, err := newClient(t, srv.URL, time.Minute).StepStream(t.Context(), agentCall(t), onDelta)
	if err != nil || answer.Failure() != nil || len(answer.Calls()) != 2 || len(*got) != 0 || hits.Load() != 1 {
		t.Fatalf("answer %v, %v, deltas %q, hits %d", answer, err, *got, hits.Load())
	}
}

// TestStepStreamKeepsTheThinkingTurn: the turn assembled from a stream goes back unchanged, so the API with thinking
// on accepts the continuation (testenv checks the thinking text and its signature).
func TestStepStreamKeepsTheThinkingTurn(t *testing.T) {
	api := testenv.NewFakeMessagesAPI(t,
		[]testenv.FakeToolUse{{ID: "toolu_01", Name: "find_lessons", Input: `{"subject":"subj-1"}`}},
		[]testenv.FakeToolUse{{ID: "toolu_02", Name: "reply", Input: `{"text":"Нашёл два урока <по матану> & всё"}`}},
	)
	client := newClient(t, api.URL, time.Minute)
	first, err := client.StepStream(t.Context(), agentCall(t), nil)
	if err != nil || first.Failure() != nil {
		t.Fatalf("first %v, %v", first, err)
	}
	if raw := first.Assistant().Raw(); !strings.Contains(raw, testenv.FakeSignatureMarker) || !strings.Contains(raw, testenv.FakeThinkingMarker) {
		t.Fatalf("kept turn %s", raw)
	}
	call, err := continuation(t, first).WithAssistant(first.Assistant())
	if err != nil {
		t.Fatal(err)
	}
	got, onDelta := collect()
	second, err := client.StepStream(t.Context(), call, onDelta)
	if err != nil || second.Failure() != nil || api.Rejected() != 0 {
		t.Fatalf("second %v, %v, rejected %d", second, err, api.Rejected())
	}
	// The fake's "Calling tools." text block is a preamble: only the reply text streams.
	if strings.Join(*got, "") != "Нашёл два урока <по матану> & всё" {
		t.Fatalf("deltas %q", *got)
	}
	for _, d := range *got {
		if strings.Contains(d, testenv.FakeThinkingMarker) || strings.Contains(d, "subj-1") {
			t.Fatalf("delta %q leaks", d)
		}
	}
}

// TestStepStreamRejectsAnUnbuiltCall: nothing is sent for a call without its parts.
func TestStepStreamRejectsAnUnbuiltCall(t *testing.T) {
	_, err := newClient(t, "http://127.0.0.1:1", time.Minute).StepStream(t.Context(), values.AgentCall{}, nil)
	var pe *values.ProviderError
	if !errors.As(err, &pe) || pe.Kind != values.KindPermanent {
		t.Fatalf("err %v", err)
	}
}
