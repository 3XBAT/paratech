// Package values holds the value objects of the llm context that cross the Provider port (ARCH-26).
package values

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Request is one provider call after the allowlist resolved the alias. Build it with NewRequest.
type Request struct {
	model        string
	instructions string
	data         string
	maxOutput    int64
}

// NewRequest checks the parts of a call: model is the provider model id resolved by the allowlist (never a client
// value), instructions is the trusted app text, data is untrusted user data and may be empty, maxOutput is the
// effective output cap. Texts must be valid UTF-8 without NUL (ADR-0005(b)); the size limits are the gateway's.
func NewRequest(model, instructions, data string, maxOutput int64) (Request, error) {
	switch {
	case model == "":
		return Request{}, fmt.Errorf("%w: model is empty", ErrInvalidRequest)
	case instructions == "":
		return Request{}, fmt.Errorf("%w: instructions are empty", ErrInvalidRequest)
	case !cleanText(instructions):
		return Request{}, fmt.Errorf("%w: instructions are not valid UTF-8 without NUL", ErrInvalidRequest)
	case !cleanText(data):
		return Request{}, fmt.Errorf("%w: data is not valid UTF-8 without NUL", ErrInvalidRequest)
	case maxOutput <= 0:
		return Request{}, fmt.Errorf("%w: max output tokens must be above 0", ErrInvalidRequest)
	}
	return Request{model: model, instructions: instructions, data: data, maxOutput: maxOutput}, nil
}

// Model is the provider model id.
func (r Request) Model() string { return r.model }

// Instructions is the trusted app text.
func (r Request) Instructions() string { return r.instructions }

// Data is the untrusted user data; a provider passes it as data, never as instructions.
func (r Request) Data() string { return r.data }

// MaxOutput is the effective output cap of the call.
func (r Request) MaxOutput() int64 { return r.maxOutput }

// String prints the request without its texts (only their sizes), so a logged request carries no prompt content.
func (r Request) String() string {
	return fmt.Sprintf("{Model:%s Instructions:(%d bytes) Data:(%d bytes) MaxOutput:%d}",
		r.model, len(r.instructions), len(r.data), r.maxOutput)
}

// GoString is String for %#v.
func (r Request) GoString() string { return "values.Request" + r.String() }

func cleanText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
