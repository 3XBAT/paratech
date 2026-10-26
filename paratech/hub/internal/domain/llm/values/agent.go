package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

// Limits of one agent step (ADR-0023, gateway.proto AgentStepRequest). Sizes are bytes of UTF-8.
const (
	MaxAgentCalls        = 4
	MaxStudentTextBytes  = 4 << 10
	MaxAgentReplyBytes   = 2 << 10
	MaxResultJSONBytes   = 16 << 10
	MaxArgumentsBytes    = 8 << 10
	MaxAgentRequestBytes = 256 << 10
	MaxAgentTurns        = 64
	MaxAgentSubjects     = 100
	// MaxLabelRunes bounds a subject name (ADR-0022) and a label argument (tool catalog).
	MaxLabelRunes = 80

	maxIDLen       = 128
	maxCallIDLen   = 128
	maxToolNameLen = 64
	maxTimeZoneLen = 64
)

// Errors of an agent step (ARCH-18). None of them quotes client input.
var (
	// ErrInvalidAgentRequest means an agent step request breaks a rule of ADR-0023.
	ErrInvalidAgentRequest = errors.New("invalid agent step request")

	// ErrAgentRequestTooLarge means the texts of the request exceed MaxAgentRequestBytes. It matches
	// ErrInvalidAgentRequest.
	ErrAgentRequestTooLarge = fmt.Errorf("%w: the request exceeds %d bytes", ErrInvalidAgentRequest, MaxAgentRequestBytes)
)

// Profile selects the server prompt, the tool catalog and the model alias of an agent step. Closed set.
type Profile string

// Profiles.
const (
	ProfilePanel Profile = "panel"
)

// ParseProfile accepts a known profile.
func ParseProfile(s string) (Profile, error) {
	if Profile(s) != ProfilePanel {
		return "", fmt.Errorf("%w: unknown profile", ErrInvalidAgentRequest)
	}
	return ProfilePanel, nil
}

// ToolCall is one tool call proposed by the model, or its echo by the client. UNTRUSTED: the Hub checks its format
// and, for calls it returns, the catalog schema; the desktop checks it again before acting. Build it with NewToolCall.
type ToolCall struct {
	callID    string
	tool      string
	arguments string
}

// NewToolCall checks the format of a call: call id 1-128 characters of [A-Za-z0-9_-], tool name snake_case of at most
// 64 characters, arguments a JSON object of at most MaxArgumentsBytes. Whether the tool is in a catalog is the
// catalog's decision.
func NewToolCall(callID, tool, arguments string) (ToolCall, error) {
	switch {
	case !validCallID(callID):
		return ToolCall{}, fmt.Errorf("%w: call id must be 1-%d characters of A-Z, a-z, 0-9, '_' and '-'", ErrInvalidAgentRequest, maxCallIDLen)
	case !ValidToolName(tool):
		return ToolCall{}, fmt.Errorf("%w: tool name must be snake_case of at most %d characters", ErrInvalidAgentRequest, maxToolNameLen)
	case len(arguments) > MaxArgumentsBytes:
		return ToolCall{}, fmt.Errorf("%w: arguments exceed %d bytes", ErrInvalidAgentRequest, MaxArgumentsBytes)
	case !jsonObject(arguments):
		return ToolCall{}, fmt.Errorf("%w: arguments must be a JSON object", ErrInvalidAgentRequest)
	}
	return ToolCall{callID: callID, tool: tool, arguments: arguments}, nil
}

// CallID is the provider's id of the call; tool results refer to it.
func (c ToolCall) CallID() string { return c.callID }

// Tool is the tool name.
func (c ToolCall) Tool() string { return c.tool }

// Arguments is the JSON object of arguments. UNTRUSTED.
func (c ToolCall) Arguments() string { return c.arguments }

// String prints the call without its arguments (only their size).
func (c ToolCall) String() string {
	return fmt.Sprintf("{CallID:%s Tool:%s Arguments:(%d bytes)}", c.callID, c.tool, len(c.arguments))
}

// GoString is String for %#v.
func (c ToolCall) GoString() string { return "values.ToolCall" + c.String() }

// ToolResult is what the desktop reports for one echoed call: size-limited data for the model, never instructions.
type ToolResult struct {
	callID  string
	json    string
	isError bool
}

// NewToolResult checks a result: a call id as in NewToolCall and valid JSON of at most MaxResultJSONBytes.
func NewToolResult(callID, resultJSON string, isError bool) (ToolResult, error) {
	switch {
	case !validCallID(callID):
		return ToolResult{}, fmt.Errorf("%w: result call id must be 1-%d characters of A-Z, a-z, 0-9, '_' and '-'", ErrInvalidAgentRequest, maxCallIDLen)
	case len(resultJSON) > MaxResultJSONBytes:
		return ToolResult{}, fmt.Errorf("%w: a tool result exceeds %d bytes", ErrInvalidAgentRequest, MaxResultJSONBytes)
	case !json.Valid([]byte(resultJSON)):
		return ToolResult{}, fmt.Errorf("%w: a tool result must be valid JSON", ErrInvalidAgentRequest)
	}
	return ToolResult{callID: callID, json: resultJSON, isError: isError}, nil
}

// CallID is the id of the call this result answers.
func (r ToolResult) CallID() string { return r.callID }

// JSON is the result as JSON data.
func (r ToolResult) JSON() string { return r.json }

// IsError reports whether the desktop failed or refused the call.
func (r ToolResult) IsError() bool { return r.isError }

// String prints the result without its JSON (only its size).
func (r ToolResult) String() string {
	return fmt.Sprintf("{CallID:%s JSON:(%d bytes) IsError:%t}", r.callID, len(r.json), r.isError)
}

// GoString is String for %#v.
func (r ToolResult) GoString() string { return "values.ToolResult" + r.String() }

// TurnKind is the kind of one history turn.
type TurnKind uint8

// Turn kinds. The zero value is not a kind.
const (
	TurnStudentText TurnKind = iota + 1
	TurnCalls
	TurnResults
	TurnAgentReply
)

// Turn is one history turn of a thread: what the student typed, an echo of calls the Hub returned (with their seal),
// the results of those calls, or what the app showed the student in answer to an earlier request. Build it with
// StudentTurn, CallsTurn, ResultsTurn or AgentReplyTurn. An echo inside an AgentCall may also carry the kept assistant
// turn of its calls (AgentCall.WithAssistant).
type Turn struct {
	kind      TurnKind
	text      string
	calls     []ToolCall
	seal      []byte
	results   []ToolResult
	assistant AssistantTurn
}

// StudentTurn checks the student's text: non-blank, valid UTF-8 without NUL, at most MaxStudentTextBytes.
func StudentTurn(text string) (Turn, error) {
	switch {
	case len(text) > MaxStudentTextBytes:
		return Turn{}, fmt.Errorf("%w: student text exceeds %d bytes", ErrInvalidAgentRequest, MaxStudentTextBytes)
	case !cleanText(text):
		return Turn{}, fmt.Errorf("%w: student text is not valid UTF-8 without NUL", ErrInvalidAgentRequest)
	case isBlank(text):
		return Turn{}, fmt.Errorf("%w: student text is empty", ErrInvalidAgentRequest)
	}
	return Turn{kind: TurnStudentText, text: text}, nil
}

// AgentReplyTurn checks the app's answer to an earlier request of the thread, built by the desktop: non-blank, valid
// UTF-8 without NUL, at most MaxAgentReplyBytes. It is bounded client data like a student text, so it has no seal
// (ADR-0023 "History pairs").
func AgentReplyTurn(text string) (Turn, error) {
	switch {
	case len(text) > MaxAgentReplyBytes:
		return Turn{}, fmt.Errorf("%w: agent reply exceeds %d bytes", ErrInvalidAgentRequest, MaxAgentReplyBytes)
	case !cleanText(text):
		return Turn{}, fmt.Errorf("%w: agent reply is not valid UTF-8 without NUL", ErrInvalidAgentRequest)
	case isBlank(text):
		return Turn{}, fmt.Errorf("%w: agent reply is empty", ErrInvalidAgentRequest)
	}
	return Turn{kind: TurnAgentReply, text: text}, nil
}

// CallsTurn checks an echo: 1..MaxAgentCalls calls with distinct ids and a non-empty seal. Whether the seal matches is
// the seal service's decision.
func CallsTurn(calls []ToolCall, seal []byte) (Turn, error) {
	if len(calls) == 0 || len(calls) > MaxAgentCalls {
		return Turn{}, fmt.Errorf("%w: an echo holds 1-%d calls", ErrInvalidAgentRequest, MaxAgentCalls)
	}
	if !distinctCallIDs(calls) {
		return Turn{}, fmt.Errorf("%w: call ids of an echo repeat", ErrInvalidAgentRequest)
	}
	if len(seal) == 0 {
		return Turn{}, fmt.Errorf("%w: an echo has no seal", ErrInvalidAgentRequest)
	}
	return Turn{kind: TurnCalls, calls: append([]ToolCall(nil), calls...), seal: bytes.Clone(seal)}, nil
}

// ResultsTurn checks a results turn: 1..MaxAgentCalls results with distinct call ids.
func ResultsTurn(results []ToolResult) (Turn, error) {
	if len(results) == 0 || len(results) > MaxAgentCalls {
		return Turn{}, fmt.Errorf("%w: a results turn holds 1-%d results", ErrInvalidAgentRequest, MaxAgentCalls)
	}
	seen := make(map[string]bool, len(results))
	for _, r := range results {
		if seen[r.callID] {
			return Turn{}, fmt.Errorf("%w: call ids of a results turn repeat", ErrInvalidAgentRequest)
		}
		seen[r.callID] = true
	}
	return Turn{kind: TurnResults, results: append([]ToolResult(nil), results...)}, nil
}

// Kind is the kind of the turn.
func (t Turn) Kind() TurnKind { return t.kind }

// Text is the student's text of a TurnStudentText or the app's answer of a TurnAgentReply.
func (t Turn) Text() string { return t.text }

// Calls are the echoed calls of a TurnCalls (a copy).
func (t Turn) Calls() []ToolCall { return append([]ToolCall(nil), t.calls...) }

// Seal is the seal of a TurnCalls (a copy).
func (t Turn) Seal() []byte { return bytes.Clone(t.seal) }

// Results are the results of a TurnResults (a copy).
func (t Turn) Results() []ToolResult { return append([]ToolResult(nil), t.results...) }

// Assistant is the kept assistant turn of an echo, zero when the call carries none.
func (t Turn) Assistant() AssistantTurn { return t.assistant }

// bytes is the size of the texts of the turn.
func (t Turn) bytes() int64 {
	n := int64(len(t.text)) + int64(len(t.seal)) + int64(len(t.assistant.raw))
	for _, c := range t.calls {
		n += int64(len(c.callID) + len(c.tool) + len(c.arguments))
	}
	for _, r := range t.results {
		n += int64(len(r.callID) + len(r.json))
	}
	return n
}

// String prints the turn without its texts.
func (t Turn) String() string {
	return fmt.Sprintf("{Kind:%d Calls:%d Results:%d Bytes:%d}", t.kind, len(t.calls), len(t.results), t.bytes())
}

// GoString is String for %#v.
func (t Turn) GoString() string { return "values.Turn" + t.String() }

// ValidToolName reports whether s is snake_case of at most 64 characters starting with a letter.
func ValidToolName(s string) bool {
	if len(s) == 0 || len(s) > maxToolNameLen || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// ValidID reports whether s is an opaque desktop ID: 1-128 bytes of valid UTF-8 without spaces or control characters
// (the desktop's own rule, domain/shared).
func ValidID(s string) bool {
	if len(s) == 0 || len(s) > maxIDLen || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validCallID(s string) bool {
	if len(s) == 0 || len(s) > maxCallIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isAlnum(c) && (c < 'A' || c > 'Z') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func distinctCallIDs(calls []ToolCall) bool {
	seen := make(map[string]bool, len(calls))
	for _, c := range calls {
		if seen[c.callID] {
			return false
		}
		seen[c.callID] = true
	}
	return true
}

// jsonObject reports whether s is valid JSON whose top value is an object.
func jsonObject(s string) bool {
	trimmed := bytes.TrimLeft([]byte(s), " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}

func isBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
