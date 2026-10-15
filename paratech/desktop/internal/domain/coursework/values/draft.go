package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// HomeworkDraftJobType is the job type of the generation of a homework draft (PRD section 10 steps 2 and 5).
const HomeworkDraftJobType = "homework_draft"

// MaxInstructionBytes bounds the student's additional instructions of a generation (PRD section 10 step 4). It is the
// limit of the student request in the selected LLM context.
const MaxInstructionBytes = 4 << 10

// CleanInstruction validates and trims the student's additional instructions: valid UTF-8, no control characters
// other than line breaks and tabs, at most MaxInstructionBytes. Empty is valid: a plain generation.
func CleanInstruction(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(s):
		return "", fmt.Errorf("%w: instruction is not valid UTF-8", ErrInvalidDraftRequest)
	case strings.ContainsFunc(s, badControl):
		return "", fmt.Errorf("%w: instruction contains control characters", ErrInvalidDraftRequest)
	case len(s) > MaxInstructionBytes:
		return "", fmt.Errorf("%w: instruction is larger than %d bytes", ErrInvalidDraftRequest, MaxInstructionBytes)
	}
	return s, nil
}

func badControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }

// DraftRequest is the payload of a homework_draft job: the assignment, the draft version the generation starts from
// (0: no draft yet) and the student's instructions. The instructions are copied into the job because they are its
// input: nothing else stores them until the generated version does. The assignment and the draft are read when the
// job runs.
type DraftRequest struct {
	assignment  shared.AssignmentID
	base        int
	instruction string
}

// NewDraftRequest validates the request; the instruction is cleaned with CleanInstruction.
func NewDraftRequest(a shared.AssignmentID, base int, instruction string) (DraftRequest, error) {
	if err := a.Validate(); err != nil {
		return DraftRequest{}, fmt.Errorf("%w: %w", ErrInvalidDraftRequest, err)
	}
	if base < 0 {
		return DraftRequest{}, fmt.Errorf("%w: negative base version %d", ErrInvalidDraftRequest, base)
	}
	instruction, err := CleanInstruction(instruction)
	if err != nil {
		return DraftRequest{}, err
	}
	return DraftRequest{assignment: a, base: base, instruction: instruction}, nil
}

// Assignment returns the assignment the draft is for.
func (r DraftRequest) Assignment() shared.AssignmentID { return r.assignment }

// Base returns the draft version the generation starts from; 0 when there was no draft.
func (r DraftRequest) Base() int { return r.base }

// Instruction returns the student's instructions; may be empty.
func (r DraftRequest) Instruction() string { return r.instruction }

type wireDraftRequest struct {
	AssignmentID *string `json:"assignment_id"`
	Base         *int    `json:"base"`
	Instruction  *string `json:"instruction"`
}

// Payload renders the job payload, for example {"assignment_id":"a1","base":2,"instruction":"shorter"}.
func (r DraftRequest) Payload() string {
	id, base, instr := string(r.assignment), r.base, r.instruction
	b, _ := json.Marshal(wireDraftRequest{AssignmentID: &id, Base: &base, Instruction: &instr}) // strings and an int always encode
	return string(b)
}

// ParseDraftPayload reads a job payload strictly: exactly one object with assignment_id, base and instruction.
func ParseDraftPayload(s string) (DraftRequest, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()

	var w wireDraftRequest
	if err := dec.Decode(&w); err != nil {
		return DraftRequest{}, fmt.Errorf("%w: not a payload object", ErrInvalidDraftRequest)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return DraftRequest{}, fmt.Errorf("%w: data after the payload", ErrInvalidDraftRequest)
	}
	if w.AssignmentID == nil || w.Base == nil || w.Instruction == nil {
		return DraftRequest{}, fmt.Errorf("%w: assignment_id, base and instruction are required", ErrInvalidDraftRequest)
	}
	return NewDraftRequest(shared.AssignmentID(*w.AssignmentID), *w.Base, *w.Instruction)
}
