package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNewAssistantTurn(t *testing.T) {
	raw := `[{"type":"thinking","thinking":"secret-thinking","signature":"sig"}]`
	turn, err := NewAssistantTurn(raw)
	if err != nil || turn.Raw() != raw || turn.IsZero() {
		t.Fatalf("turn = %v, %v", turn, err)
	}
	if s := fmt.Sprintf("%v %+v %#v", turn, turn, turn); strings.Contains(s, "secret") {
		t.Fatalf("printed turn leaks its content: %s", s)
	}
	if !(AssistantTurn{}).IsZero() {
		t.Fatal("the zero turn is not zero")
	}
	for name, raw := range map[string]string{
		"empty":     "",
		"not JSON":  "[{",
		"too large": `"` + strings.Repeat("a", MaxAssistantTurnBytes) + `"`,
	} {
		if _, err := NewAssistantTurn(raw); !errors.Is(err, ErrInvalidAssistantTurn) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCallSetKey(t *testing.T) {
	calls := []ToolCall{mustCall(t, "toolu_01", "find_lessons", `{"subject":"s"}`), mustCall(t, "toolu_02", "reply", `{"text":"x"}`)}
	key := CallSetKey("th-1", ProfilePanel, 1, calls)
	if len(key) != 64 || key != CallSetKey("th-1", ProfilePanel, 1, calls) {
		t.Fatalf("key = %q", key)
	}
	others := map[string]string{
		"thread":    CallSetKey("th-2", ProfilePanel, 1, calls),
		"version":   CallSetKey("th-1", ProfilePanel, 2, calls),
		"order":     CallSetKey("th-1", ProfilePanel, 1, []ToolCall{calls[1], calls[0]}),
		"one call":  CallSetKey("th-1", ProfilePanel, 1, calls[:1]),
		"arguments": CallSetKey("th-1", ProfilePanel, 1, []ToolCall{mustCall(t, "toolu_01", "find_lessons", `{"subject":"t"}`), calls[1]}),
		// Length-prefixed fields: moving bytes between fields changes the key.
		"shifted": CallSetKey("th-1t", ProfilePanel, 1, []ToolCall{mustCall(t, "oolu_01", "find_lessons", `{"subject":"s"}`), calls[1]}),
	}
	for name, other := range others {
		if other == key {
			t.Errorf("%s: same key", name)
		}
	}
}

func TestAgentCallWithAssistant(t *testing.T) {
	req, err := testRequest(t, history(t))
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := NewToolSpec("reply", "Card.", `{"type":"object"}`)
	call, err := NewAgentCall("m", "system", []ToolSpec{tool}, req, 100)
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := NewAssistantTurn(`[{"type":"thinking"}]`)
	with, err := call.WithAssistant(turn)
	if err != nil {
		t.Fatal(err)
	}
	turns := with.Turns()
	if LastCalls(turns) != 1 || turns[1].Assistant() != turn || !turns[0].Assistant().IsZero() || !turns[3].Assistant().IsZero() {
		t.Fatalf("turns = %v", turns)
	}
	if !call.Turns()[1].Assistant().IsZero() {
		t.Fatal("WithAssistant changed the original call")
	}
	if with.InputBytes() != call.InputBytes()+int64(len(turn.Raw())) {
		t.Fatalf("input bytes %d -> %d", call.InputBytes(), with.InputBytes())
	}
	if _, err := call.WithAssistant(AssistantTurn{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero turn: err = %v", err)
	}

	text, _ := StudentTurn("hi")
	noEcho, _ := testRequest(t, []Turn{text})
	plain, _ := NewAgentCall("m", "system", []ToolSpec{tool}, noEcho, 100)
	if _, err := plain.WithAssistant(turn); !errors.Is(err, ErrInvalidRequest) || LastCalls(plain.Turns()) != -1 {
		t.Fatalf("no echo: err = %v", err)
	}
}

func TestAgentAnswerWithAssistant(t *testing.T) {
	u, _ := NewUsage(1, 1)
	a := NewAgentAnswer([]ToolCall{mustCall(t, "toolu_01", "reply", "{}")}, u, true)
	if !a.Assistant().IsZero() {
		t.Fatal("a new answer has a turn")
	}
	turn, _ := NewAssistantTurn(`[]`)
	if got := a.WithAssistant(turn).Assistant(); got != turn {
		t.Fatalf("turn = %v", got)
	}
}
