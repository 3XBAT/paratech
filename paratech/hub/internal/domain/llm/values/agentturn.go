package values

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// MaxAssistantTurnBytes bounds the raw assistant content kept for one step. The planner's output cap keeps a real
// turn (thinking with its signature, text, tool_use) far below it.
const MaxAssistantTurnBytes = 1 << 20

// ErrInvalidAssistantTurn means raw assistant content is empty, larger than MaxAssistantTurnBytes or not JSON.
var ErrInvalidAssistantTurn = errors.New("invalid assistant turn")

// AssistantTurn is the provider's raw assistant content of one step (thinking blocks with their signatures, text and
// tool_use blocks), kept on the Hub so that the next step of the tool loop sends it back unchanged (ADR-0023). It is
// model output: never logged and never sent to the client. The zero value is no turn. Build it with NewAssistantTurn.
type AssistantTurn struct {
	raw string
}

// NewAssistantTurn checks raw: non-empty JSON of at most MaxAssistantTurnBytes.
func NewAssistantTurn(raw string) (AssistantTurn, error) {
	if raw == "" || len(raw) > MaxAssistantTurnBytes || !json.Valid([]byte(raw)) {
		return AssistantTurn{}, fmt.Errorf("%w: it must be JSON of 1-%d bytes", ErrInvalidAssistantTurn, MaxAssistantTurnBytes)
	}
	return AssistantTurn{raw: raw}, nil
}

// Raw is the provider's content as it was received.
func (a AssistantTurn) Raw() string { return a.raw }

// IsZero reports whether there is no turn.
func (a AssistantTurn) IsZero() bool { return a.raw == "" }

// String prints only the size.
func (a AssistantTurn) String() string { return fmt.Sprintf("{Bytes:%d}", len(a.raw)) }

// GoString is String for %#v.
func (a AssistantTurn) GoString() string { return "values.AssistantTurn" + a.String() }

// callSetKeyDomain separates CallSetKey from every other use of SHA-256.
const callSetKeyDomain = "paratech.hub.v1.AgentCallSet/1"

// CallSetKey names one returned call set: the hex SHA-256 of the thread, profile, catalog version and calls in order,
// the values the seal binds besides the owner (the store keeps the owner as its own column). An echo that passed the
// seal check names the same set as the answer that returned it.
func CallSetKey(threadID string, profile Profile, catalogVersion uint32, calls []ToolCall) string {
	h := sha256.New()
	writeField(h, callSetKeyDomain)
	fields := []string{threadID, string(profile), strconv.FormatUint(uint64(catalogVersion), 10), strconv.Itoa(len(calls))}
	for _, c := range calls {
		fields = append(fields, c.callID, c.tool, c.arguments)
	}
	for _, f := range fields {
		writeField(h, f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MissingReplyText stands on the assistant side for the answer to an earlier request when the client sent no agent
// reply at all (a desktop before TASK-183). Without it consecutive student texts merge into one user message and the
// model answers all of them (TASK-140).
const MissingReplyText = "(earlier request; its answer is not available)"

// ReplyMarkers reports for each turn whether MissingReplyText follows it: only when the turns hold no agent reply and
// the turn is a student text directly followed by another one. A history with agent replies is sent as it is.
func ReplyMarkers(turns []Turn) []bool {
	marks := make([]bool, len(turns))
	for _, t := range turns {
		if t.kind == TurnAgentReply {
			return marks
		}
	}
	for i := 0; i+1 < len(turns); i++ {
		marks[i] = turns[i].kind == TurnStudentText && turns[i+1].kind == TurnStudentText
	}
	return marks
}

// LastCalls is the index of the last echo of calls in the turns, or -1. That echo is the assistant turn the provider
// sees last, the one whose kept content must be sent back (ADR-0023).
func LastCalls(turns []Turn) int {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].kind == TurnCalls {
			return i
		}
	}
	return -1
}
