package values

import (
	"fmt"
	"math"
)

// StopReason says why the model stopped. The set is closed: provider values the Hub does not know (providers add
// values) map to StopOther. The zero value is StopOther.
type StopReason uint8

// Stop reasons.
const (
	// StopOther is a reason the Hub does not model (also tool use or a paused turn, which the Hub never asks for).
	StopOther StopReason = iota
	// StopEnd means the model finished its answer.
	StopEnd
	// StopMaxTokens means the output reached the output cap; the output is truncated.
	StopMaxTokens
	// StopSequence means the output reached a stop sequence.
	StopSequence
	// StopRefusal means the provider's safety classifiers stopped the answer; the output is not usable.
	StopRefusal
	// StopContextWindow means the input and output reached the model's context window.
	StopContextWindow
)

var stopNames = [...]string{"other", "end", "max_tokens", "stop_sequence", "refusal", "context_window"}

// String names the reason for logs.
func (s StopReason) String() string {
	if int(s) < len(stopNames) {
		return stopNames[s]
	}
	return fmt.Sprintf("StopReason(%d)", uint8(s))
}

// valid reports whether s is one of the constants.
func (s StopReason) valid() bool { return int(s) < len(stopNames) }

// Usage is the provider-reported token count of one call, the basis of settlement. Build it with NewUsage.
type Usage struct {
	input, output int64
}

// NewUsage checks the counts: input is all input tokens (including cached ones), output all output tokens (including
// thinking). Both must be non-negative and their sum must fit in int64.
func NewUsage(input, output int64) (Usage, error) {
	if input < 0 || output < 0 {
		return Usage{}, fmt.Errorf("%w: negative token count", ErrInvalidCompletion)
	}
	if input > math.MaxInt64-output {
		return Usage{}, fmt.Errorf("%w: token count overflows", ErrInvalidCompletion)
	}
	return Usage{input: input, output: output}, nil
}

// Input is the number of input tokens.
func (u Usage) Input() int64 { return u.input }

// Output is the number of output tokens.
func (u Usage) Output() int64 { return u.output }

// Total is Input plus Output, the quota amount in tokens (ADR-0005(a)).
func (u Usage) Total() int64 { return u.input + u.output }

// Completion is a successful provider answer. A truncated or refused answer is still a Completion (see Stop); what to
// do with it is the gateway's decision. Build it with NewCompletion.
type Completion struct {
	output string
	usage  Usage
	stop   StopReason
}

// NewCompletion checks the stop reason. output is the model text, UNTRUSTED.
func NewCompletion(output string, usage Usage, stop StopReason) (Completion, error) {
	if !stop.valid() {
		return Completion{}, fmt.Errorf("%w: unknown stop reason", ErrInvalidCompletion)
	}
	return Completion{output: output, usage: usage, stop: stop}, nil
}

// Output is the model text. UNTRUSTED: it may carry prompt injection from the user data. It is never interpreted,
// logged or used to choose a side effect by the Hub; the client validates it (CLAUDE.md hard rule 3).
func (c Completion) Output() string { return c.output }

// Usage is the provider-reported token count.
func (c Completion) Usage() Usage { return c.usage }

// Stop says why the model stopped.
func (c Completion) Stop() StopReason { return c.stop }

// String prints the completion without the output text (only its size).
func (c Completion) String() string {
	return fmt.Sprintf("{Output:(%d bytes) InputTokens:%d OutputTokens:%d Stop:%s}", len(c.output), c.usage.input, c.usage.output, c.stop)
}

// GoString is String for %#v.
func (c Completion) GoString() string { return "values.Completion" + c.String() }
