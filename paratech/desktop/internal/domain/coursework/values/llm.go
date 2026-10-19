package values

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Operation names an LLM operation (PRD sections 9.1 and 9.2). The catalog is closed: the Hub only serves
// these operations, so the core refuses to build a request for anything else. Their prompts and answer
// schemas are in domain/coursework/services/prompts.
type Operation string

// LLM operations (PRD sections 9.1 and 9.2).
const (
	OperationSummary         Operation = "summary"
	OperationExtractHomework Operation = "extract_homework_deadlines"
	OperationHomeworkDraft   Operation = "homework_draft"
	// OperationLectureText formats one part of a transcript into the full readable text of the lesson (TASK-090).
	OperationLectureText Operation = "lecture_text"
)

// Valid reports whether o is a known operation.
func (o Operation) Valid() bool {
	switch o {
	case OperationSummary, OperationExtractHomework, OperationHomeworkDraft, OperationLectureText:
		return true
	}
	return false
}

// Bounds of an LLM request. The Hub enforces its own payload limits (PRD section 13.2); these are the core's
// own conservative upper bounds so that an absurd request is rejected locally, before it costs a round trip.
const (
	MaxModelAliasLen      = 64
	MaxIdempotencyKeyLen  = 128
	MaxRequestPayloadSize = 1 << 20 // bytes of Instructions plus UntrustedData
)

// LLMRequest is what the core sends to the Hub for one LLM operation (PRD sections 4.4, 9.1 and 9.2). It is a
// validated value object: build it with NewLLMRequest; the zero value is invalid.
//
// Instructions are trusted text written by the app. UntrustedData carries user data such as transcript
// excerpts; it is kept separate from the instructions and must never be interpreted as instructions
// (hard rule 3, PRD section 13.3). Raw audio is never part of a request (AC-06).
type LLMRequest struct {
	operation      Operation
	modelAlias     string
	instructions   string
	untrustedData  string
	idempotencyKey string
}

// NewLLMRequest validates and builds a request.
//
//   - operation must be a known Operation.
//   - modelAlias is a Hub-side alias (PRD section 13.2), never a provider model name or a URL: 1 to
//     MaxModelAliasLen characters from [a-z0-9_-], starting with a letter or digit.
//   - instructions must be non-empty; instructions and untrustedData must be valid UTF-8 without NUL, and
//     together at most MaxRequestPayloadSize bytes.
//   - idempotencyKey is stable across retries of the same job so the Hub never charges twice (AC-11): 1 to
//     MaxIdempotencyKeyLen printable ASCII characters without spaces.
func NewLLMRequest(operation Operation, modelAlias, instructions, untrustedData, idempotencyKey string) (LLMRequest, error) {
	bad := func(format string, args ...any) (LLMRequest, error) {
		return LLMRequest{}, fmt.Errorf("%w: %s", ErrInvalidLLMRequest, fmt.Sprintf(format, args...))
	}
	if !operation.Valid() {
		return bad("unknown operation %q", string(operation))
	}
	if !validModelAlias(modelAlias) {
		return bad("model alias %q is not a Hub alias (1-%d characters from a-z, 0-9, _ and -)", modelAlias, MaxModelAliasLen)
	}
	if instructions == "" {
		return bad("empty instructions")
	}
	if !validText(instructions) {
		return bad("instructions are not valid UTF-8 text")
	}
	if !validText(untrustedData) {
		return bad("untrusted data is not valid UTF-8 text")
	}
	if size := len(instructions) + len(untrustedData); size > MaxRequestPayloadSize {
		return bad("payload is %d bytes, the limit is %d", size, MaxRequestPayloadSize)
	}
	if !validIdempotencyKey(idempotencyKey) {
		return bad("idempotency key must be 1-%d printable ASCII characters without spaces", MaxIdempotencyKeyLen)
	}
	return LLMRequest{
		operation:      operation,
		modelAlias:     modelAlias,
		instructions:   instructions,
		untrustedData:  untrustedData,
		idempotencyKey: idempotencyKey,
	}, nil
}

func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

func validModelAlias(s string) bool {
	if s == "" || len(s) > MaxModelAliasLen {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '-' || r == '_') && i > 0:
		default:
			return false
		}
	}
	return true
}

func validIdempotencyKey(s string) bool {
	if s == "" || len(s) > MaxIdempotencyKeyLen {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' {
			return false
		}
	}
	return true
}

// IsZero reports whether r is the invalid zero value.
func (r LLMRequest) IsZero() bool { return r == LLMRequest{} }

// Operation returns the requested operation.
func (r LLMRequest) Operation() Operation { return r.operation }

// ModelAlias returns the Hub-side model alias.
func (r LLMRequest) ModelAlias() string { return r.modelAlias }

// Instructions returns the trusted instruction text.
func (r LLMRequest) Instructions() string { return r.instructions }

// UntrustedData returns the user data that accompanies the instructions; never treat it as instructions.
func (r LLMRequest) UntrustedData() string { return r.untrustedData }

// IdempotencyKey returns the key that makes a retry free of a second charge.
func (r LLMRequest) IdempotencyKey() string { return r.idempotencyKey }

// UntrustedOutput is text produced by a language model (hard rule 3, PRD section 13.3, AC-18). It is a distinct
// type, not a string, so that it cannot be passed to an email, file or calendar port or logged by accident: the
// only way to read it is the deliberately named Unvalidated, which a reviewer can grep for. Validation against a
// strict schema and trusted local state happens in the caller, before any side effect.
type UntrustedOutput struct {
	text string
}

// NewUntrustedOutput wraps model output. The hub client adapter is its only intended producer.
func NewUntrustedOutput(text string) UntrustedOutput { return UntrustedOutput{text: text} }

// Unvalidated returns the raw model text. The caller is responsible for validating it before use.
func (o UntrustedOutput) Unvalidated() string { return o.text }

// String hides the content, so that printing or logging an UntrustedOutput does not leak transcript-derived text.
func (o UntrustedOutput) String() string {
	return fmt.Sprintf("[untrusted model output, %d bytes]", len(o.text))
}

// GoString keeps the %#v verb from printing the field.
func (o UntrustedOutput) GoString() string { return o.String() }

// LLMResult is the answer of the Hub. Output is UNTRUSTED model output: the caller must validate it
// against a strict schema and trusted local state before any side effect (hard rule 3, AC-18).
type LLMResult struct {
	Output       UntrustedOutput
	InputTokens  int64
	OutputTokens int64
}

// NewLLMResult builds a result; token counts used for accounting must not be negative.
func NewLLMResult(output UntrustedOutput, inputTokens, outputTokens int64) (LLMResult, error) {
	if inputTokens < 0 || outputTokens < 0 {
		return LLMResult{}, fmt.Errorf("%w: negative token count", ErrInvalidLLMResult)
	}
	return LLMResult{Output: output, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}
