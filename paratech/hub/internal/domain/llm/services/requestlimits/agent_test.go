package requestlimits

import (
	"errors"
	"testing"

	"paratech/hub/internal/domain/llm/values"
)

func TestAgentReservation(t *testing.T) {
	text, err := values.StudentTurn("Что задали?")
	if err != nil {
		t.Fatal(err)
	}
	base, err := values.NewAgentBase(nil, "2026-10-22", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	req, err := values.NewAgentRequest("k", "th", values.ProfilePanel, 1, base, values.AgentScope{}, []values.Turn{text}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := values.NewToolSpec("reply", "Card.", `{"type":"object"}`)
	if err != nil {
		t.Fatal(err)
	}
	call, err := values.NewAgentCall("m", "System.", []values.ToolSpec{tool}, req, 400)
	if err != nil {
		t.Fatal(err)
	}
	got, err := AgentReservation(call)
	if err != nil || got != call.InputBytes()+FramingTokens+400 {
		t.Fatalf("AgentReservation = %d, %v; want input bytes + %d + 400", got, err, FramingTokens)
	}
	if _, err := AgentReservation(values.AgentCall{}); !errors.Is(err, ErrInvalidMaxOutput) {
		t.Fatalf("zero call: err = %v", err)
	}
}
