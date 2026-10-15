package account

import (
	"fmt"
	"math"
)

// Result is the stored answer of a COMPLETED gateway attempt (gateway.proto, key life cycle): it is kept on the key
// record of the attempt so that a retry with the same key replays it. Output is model text: UNTRUSTED and opaque to
// this context. The zero value (Stored false) means no result. Build it with NewResult.
type Result struct {
	Stored       bool
	Output       string
	InputTokens  int64
	OutputTokens int64
}

// NewResult checks the provider-reported token counts of an answer.
func NewResult(output string, inputTokens, outputTokens int64) (Result, error) {
	r := Result{Stored: true, Output: output, InputTokens: inputTokens, OutputTokens: outputTokens}
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	return r, nil
}

// Total is the settled usage of the result, input and output tokens together.
func (r Result) Total() int64 { return r.InputTokens + r.OutputTokens }

// String prints the result without its output (only its size).
func (r Result) String() string {
	return fmt.Sprintf("{Stored:%t Output:(%d bytes) InputTokens:%d OutputTokens:%d}", r.Stored, len(r.Output), r.InputTokens, r.OutputTokens)
}

// GoString is String for %#v.
func (r Result) GoString() string { return "account.Result" + r.String() }

func (r Result) validate() error {
	if !r.Stored {
		if r != (Result{}) {
			return fmt.Errorf("%w: a result that is not stored has no content", ErrInvalidArgument)
		}
		return nil
	}
	if r.InputTokens < 0 || r.OutputTokens < 0 || r.InputTokens > math.MaxInt64-r.OutputTokens {
		return fmt.Errorf("%w: result token counts must be non-negative and fit int64", ErrInvalidArgument)
	}
	return nil
}
