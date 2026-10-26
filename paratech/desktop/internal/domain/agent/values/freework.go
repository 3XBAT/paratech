package values

import (
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// FreeWorkCard is «Такого задания нет — могу сделать как свободную работу» (TASK-188): a triage_assignment call on a
// topic waits for the student instead of running. It names the call only (thread, request, call ID); the topic stays
// in the stored call, so the page never sends a text back. «Сделать» on it is the student's consent to the paid triage.
type FreeWorkCard struct {
	Thread shared.ThreadID
	Turn   int
	CallID string
}

// Kind returns CardFreeWork.
func (FreeWorkCard) Kind() CardKind { return CardFreeWork }

// Validate checks the card.
func (c FreeWorkCard) Validate() error {
	if err := c.Thread.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCard, err)
	}
	if c.Turn < 1 {
		return fmt.Errorf("%w: a free work card names a request", ErrInvalidCard)
	}
	if err := checkCallID(c.CallID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCard, err)
	}
	return nil
}

// freeWorkResultJSON is the whole result the planner gets for a call that waits on the free-work card (allowlist:
// one fixed field, never the topic).
const freeWorkResultJSON = `{"status":"awaiting_student"}`

// FreeWorkResult is the result of a triage_assignment call on a topic that waits for the student's answer on the
// free-work card: nothing ran.
func FreeWorkResult(callID string) (ToolResult, error) {
	r := ToolResult{CallID: callID, ResultJSON: freeWorkResultJSON}
	if err := r.Validate(); err != nil {
		return ToolResult{}, err
	}
	return r, nil
}

// IsFreeWorkResult reports whether r is the result FreeWorkResult made.
func IsFreeWorkResult(r ToolResult) bool {
	return !r.IsError && r.ResultJSON == freeWorkResultJSON && checkCallID(r.CallID) == nil
}
