package values

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strconv"
)

// Format limits of a completion spec (ADR-0005(b), gateway.proto CompletionSpec). They mirror the desktop core.
const (
	// MaxPayloadBytes bounds len(instructions) + len(untrusted data).
	MaxPayloadBytes = 1 << 20

	maxKeyLen                     = 128
	maxAliasLen                   = 64
	firstPrintable, lastPrintable = 0x21, 0x7e // printable ASCII without the space
)

// Errors of a completion spec (ARCH-18). None of them quotes client input.
var (
	// ErrInvalidSpec means a completion spec breaks a format rule.
	ErrInvalidSpec = errors.New("invalid completion spec")

	// ErrPayloadTooLarge means instructions and untrusted data together exceed MaxPayloadBytes. It matches ErrInvalidSpec.
	ErrPayloadTooLarge = fmt.Errorf("%w: instructions and untrusted data exceed %d bytes", ErrInvalidSpec, MaxPayloadBytes)

	// ErrClientInstructions means a client sent instructions for an operation whose prompt is the server's. It matches
	// ErrInvalidSpec.
	ErrClientInstructions = fmt.Errorf("%w: the operation takes no client instructions", ErrInvalidSpec)

	// ErrAliasMismatch means a client named another alias than the server's for an operation with a server prompt. It
	// matches ErrInvalidSpec.
	ErrAliasMismatch = fmt.Errorf("%w: the operation uses the server's model alias", ErrInvalidSpec)
)

// Operation is the LLM operation of a completion; the set mirrors the contract's OperationType.
type Operation string

// Operations. OperationAgentStep names the ledger entries of AgentStep and is never valid in a spec (ADR-0023).
const (
	OperationSummary          Operation = "summary"
	OperationExtractHomework  Operation = "extract_homework_deadlines"
	OperationHomeworkDraft    Operation = "homework_draft"
	OperationLectureText      Operation = "lecture_text"
	OperationAgentStep        Operation = "agent_step"
	OperationInspectMaterial  Operation = "inspect_material"
	OperationAnswer           Operation = "answer"
	OperationTriageAssignment Operation = "triage_assignment"
)

// Spec is a validated completion request of a client (gateway.proto CompletionSpec). Build it with NewSpec.
type Spec struct {
	key          string
	operation    Operation
	alias        string
	instructions string
	data         string
	maxOutput    int64
	// promptVersion is set by WithServerPrompt: the instructions are the server prompt of that version.
	promptVersion string
}

// ServerPrompt reports whether the Hub writes the prompt of op itself and picks its alias (ADR-0023): the client sends
// no instructions, and an alias only if it is the server's.
func (op Operation) ServerPrompt() bool {
	return op == OperationInspectMaterial || op == OperationAnswer || op == OperationTriageAssignment
}

// NewSpec checks every rule of a CompletionSpec: key 1-128 printable ASCII without spaces, a known operation, alias
// 1-64 characters of [a-z0-9_-] starting with a letter or digit, non-empty instructions, both texts valid UTF-8
// without NUL and together at most MaxPayloadBytes, maxOutput not negative (0 means the alias cap). Whether the alias
// is allowed is the allowlist's decision. For an operation with a server prompt the instructions must be empty and the
// alias may be empty; the spec is usable only after WithServerPrompt.
func NewSpec(key, operation, alias, instructions, data string, maxOutput int64) (Spec, error) {
	if Operation(operation).ServerPrompt() {
		return newServerSpec(key, Operation(operation), alias, instructions, data, maxOutput)
	}
	switch {
	case !validKey(key):
		return Spec{}, fmt.Errorf("%w: idempotency key must be 1-%d printable ASCII characters without spaces", ErrInvalidSpec, maxKeyLen)
	case !validOperation(Operation(operation)):
		return Spec{}, fmt.Errorf("%w: unknown operation type", ErrInvalidSpec)
	case !validAlias(alias):
		return Spec{}, fmt.Errorf("%w: model alias must be 1-%d characters of a-z, 0-9, '_' and '-' starting with a letter or digit",
			ErrInvalidSpec, maxAliasLen)
	case instructions == "":
		return Spec{}, fmt.Errorf("%w: instructions are empty", ErrInvalidSpec)
	case len(instructions) > MaxPayloadBytes-len(data):
		return Spec{}, ErrPayloadTooLarge
	case !cleanText(instructions):
		return Spec{}, fmt.Errorf("%w: instructions are not valid UTF-8 without NUL", ErrInvalidSpec)
	case !cleanText(data):
		return Spec{}, fmt.Errorf("%w: untrusted data is not valid UTF-8 without NUL", ErrInvalidSpec)
	case maxOutput < 0:
		return Spec{}, fmt.Errorf("%w: max output tokens must not be negative", ErrInvalidSpec)
	}
	return Spec{key: key, operation: Operation(operation), alias: alias, instructions: instructions, data: data, maxOutput: maxOutput}, nil
}

// newServerSpec checks a spec of an operation with a server prompt: no instructions, the alias empty or well formed.
func newServerSpec(key string, op Operation, alias, instructions, data string, maxOutput int64) (Spec, error) {
	switch {
	case !validKey(key):
		return Spec{}, fmt.Errorf("%w: idempotency key must be 1-%d printable ASCII characters without spaces", ErrInvalidSpec, maxKeyLen)
	case alias != "" && !validAlias(alias):
		return Spec{}, fmt.Errorf("%w: model alias must be 1-%d characters of a-z, 0-9, '_' and '-' starting with a letter or digit",
			ErrInvalidSpec, maxAliasLen)
	case instructions != "":
		return Spec{}, ErrClientInstructions
	case len(data) > MaxPayloadBytes:
		return Spec{}, ErrPayloadTooLarge
	case !cleanText(data):
		return Spec{}, fmt.Errorf("%w: untrusted data is not valid UTF-8 without NUL", ErrInvalidSpec)
	case maxOutput < 0:
		return Spec{}, fmt.Errorf("%w: max output tokens must not be negative", ErrInvalidSpec)
	}
	return Spec{key: key, operation: op, alias: alias, data: data, maxOutput: maxOutput}, nil
}

// WithServerPrompt returns the spec of an operation with a server prompt completed by the Hub: alias is the server's
// alias of the operation, version and text its prompt. A client alias other than the server's is ErrAliasMismatch.
func (s Spec) WithServerPrompt(alias, version, text string) (Spec, error) {
	switch {
	case !s.operation.ServerPrompt() || s.promptVersion != "":
		return Spec{}, fmt.Errorf("%w: the operation has no server prompt", ErrInvalidSpec)
	case !validAlias(alias) || version == "" || !cleanText(version) || text == "" || !cleanText(text):
		return Spec{}, fmt.Errorf("%w: invalid server prompt", ErrInvalidSpec)
	case s.alias != "" && s.alias != alias:
		return Spec{}, ErrAliasMismatch
	case len(text) > MaxPayloadBytes-len(s.data):
		return Spec{}, ErrPayloadTooLarge
	}
	s.alias, s.instructions, s.promptVersion = alias, text, version
	return s, nil
}

// PromptVersion is the server prompt version of the spec, empty when the client wrote the instructions.
func (s Spec) PromptVersion() string { return s.promptVersion }

// Key is the idempotency key.
func (s Spec) Key() string { return s.key }

// Operation is the operation type.
func (s Spec) Operation() Operation { return s.operation }

// Alias is the requested model alias.
func (s Spec) Alias() string { return s.alias }

// Instructions is the trusted app text.
func (s Spec) Instructions() string { return s.instructions }

// Data is the untrusted user data.
func (s Spec) Data() string { return s.data }

// MaxOutput is the requested output cap as the client sent it (0: the alias cap).
func (s Spec) MaxOutput() int64 { return s.maxOutput }

// PayloadBytes is len(instructions) + len(data).
func (s Spec) PayloadBytes() int64 { return int64(len(s.instructions)) + int64(len(s.data)) }

// fingerprintDomain separates this encoding from any other use of SHA-256; the version changes with the encoding.
const fingerprintDomain = "paratech.hub.v1.CompletionSpec/1"

// Fingerprint is the hex SHA-256 of an unambiguous encoding of the spec without its key: operation, alias,
// instructions, data and the requested max output tokens in decimal, each preceded by its length as 8 bytes
// big-endian. The key is
// left out because it is what the fingerprint is stored under. Two specs have the same fingerprint only if every
// field but the key is equal; the texts themselves are never stored (ADR-0005(d)). With a server prompt its version
// stands in for the instructions (ADR-0010 as amended by ADR-0023), so a new prompt version is a new request.
func (s Spec) Fingerprint() string {
	h := sha256.New()
	writeField(h, fingerprintDomain)
	instructions := s.instructions
	if s.promptVersion != "" {
		instructions = "server-prompt/" + s.promptVersion
	}
	for _, f := range []string{string(s.operation), s.alias, instructions, s.data, strconv.FormatInt(s.maxOutput, 10)} {
		writeField(h, f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(h hash.Hash, f string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(f)))
	h.Write(n[:])
	h.Write([]byte(f))
}

// String prints the spec without its texts and key (only sizes), so a logged spec carries no prompt content.
func (s Spec) String() string {
	return fmt.Sprintf("{Operation:%s Alias:%s Instructions:(%d bytes) Data:(%d bytes) MaxOutput:%d}",
		s.operation, s.alias, len(s.instructions), len(s.data), s.maxOutput)
}

// GoString is String for %#v.
func (s Spec) GoString() string { return "values.Spec" + s.String() }

func validOperation(op Operation) bool {
	return op == OperationSummary || op == OperationExtractHomework || op == OperationHomeworkDraft ||
		op == OperationLectureText
}

func validKey(s string) bool {
	if len(s) == 0 || len(s) > maxKeyLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < firstPrintable || s[i] > lastPrintable {
			return false
		}
	}
	return true
}

func validAlias(s string) bool {
	if len(s) == 0 || len(s) > maxAliasLen || !isAlnum(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isAlnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') }
