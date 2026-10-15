package values

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestNewRequest(t *testing.T) {
	r, err := NewRequest("claude-sonnet-5-5", "Summarize.", "lesson text", 512)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model() != "claude-sonnet-5-5" || r.Instructions() != "Summarize." || r.Data() != "lesson text" || r.MaxOutput() != 512 {
		t.Fatalf("got %v", r)
	}
	if _, err := NewRequest("m", "i", "", 1); err != nil {
		t.Fatalf("empty data must be allowed: %v", err)
	}

	tests := []struct {
		name               string
		model, instr, data string
		maxOutput          int64
	}{
		{name: "no model", instr: "i", maxOutput: 1},
		{name: "no instructions", model: "m", maxOutput: 1},
		{name: "zero max output", model: "m", instr: "i"},
		{name: "negative max output", model: "m", instr: "i", maxOutput: -1},
		{name: "instructions with NUL", model: "m", instr: "a\x00b", maxOutput: 1},
		{name: "data with NUL", model: "m", instr: "i", data: "a\x00b", maxOutput: 1},
		{name: "instructions not UTF-8", model: "m", instr: "\xff", maxOutput: 1},
		{name: "data not UTF-8", model: "m", instr: "i", data: "\xc3", maxOutput: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRequest(tt.model, tt.instr, tt.data, tt.maxOutput); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestRequestPrintsNoText(t *testing.T) {
	r, err := NewRequest("m", "secret-instructions", "secret-data", 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r), fmt.Sprintf("%v", &r)} {
		if strings.Contains(out, "secret") {
			t.Errorf("printed request leaks text: %s", out)
		}
		if !strings.Contains(out, "19 bytes") || !strings.Contains(out, "11 bytes") {
			t.Errorf("printed request lacks sizes: %s", out)
		}
	}
}

func TestNewUsage(t *testing.T) {
	u, err := NewUsage(100, 20)
	if err != nil {
		t.Fatal(err)
	}
	if u.Input() != 100 || u.Output() != 20 || u.Total() != 120 {
		t.Fatalf("got %+v", u)
	}
	if u, err := NewUsage(0, 0); err != nil || u.Total() != 0 {
		t.Fatalf("zero usage: %v %v", u, err)
	}
	for _, c := range [][2]int64{{-1, 0}, {0, -1}, {math.MaxInt64, 1}, {1, math.MaxInt64}} {
		if _, err := NewUsage(c[0], c[1]); !errors.Is(err, ErrInvalidCompletion) {
			t.Errorf("NewUsage(%d, %d) error = %v, want ErrInvalidCompletion", c[0], c[1], err)
		}
	}
	if u, err := NewUsage(math.MaxInt64-1, 1); err != nil || u.Total() != math.MaxInt64 {
		t.Fatalf("max usage: %v %v", u, err)
	}
}

func TestNewCompletion(t *testing.T) {
	u, _ := NewUsage(10, 5)
	c, err := NewCompletion("untrusted-output", u, StopMaxTokens)
	if err != nil {
		t.Fatal(err)
	}
	if c.Output() != "untrusted-output" || c.Usage() != u || c.Stop() != StopMaxTokens {
		t.Fatalf("got %v", c)
	}
	if c.Refused() {
		t.Fatal("a max_tokens answer is not a refusal")
	}
	if c.Failure() != nil {
		t.Fatal("a max_tokens answer is not a failure")
	}
	r, err := NewCompletion("", u, StopRefusal)
	if err != nil || !r.Refused() {
		t.Fatalf("refusal: %v, %v", r, err)
	}
	var pe *ProviderError
	if f := r.Failure(); !errors.As(f, &pe) || pe.Kind != KindPermanent || !errors.Is(f, ErrProviderPermanent) {
		t.Fatalf("refusal failure = %v, want a permanent ProviderError", f)
	}
	if _, err := NewCompletion("", u, StopReason(200)); !errors.Is(err, ErrInvalidCompletion) {
		t.Fatalf("error = %v, want ErrInvalidCompletion", err)
	}
	for _, out := range []string{fmt.Sprint(c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(out, "untrusted-output") {
			t.Errorf("printed completion leaks the output: %s", out)
		}
	}
}

func TestStopReasonString(t *testing.T) {
	want := map[StopReason]string{
		StopOther: "other", StopEnd: "end", StopMaxTokens: "max_tokens", StopSequence: "stop_sequence",
		StopRefusal: "refusal", StopContextWindow: "context_window", StopReason(99): "StopReason(99)",
	}
	for s, w := range want {
		if s.String() != w {
			t.Errorf("String(%d) = %q, want %q", uint8(s), s.String(), w)
		}
	}
	var zero StopReason
	if zero != StopOther {
		t.Fatal("zero stop reason must be StopOther")
	}
}

func TestProviderErrorClassification(t *testing.T) {
	transient := &ProviderError{Kind: KindTransient, Reason: "rate limited", Status: 429, Type: "rate_limit_error", RetryAfter: 3 * time.Second}
	if !errors.Is(transient, ErrProviderTransient) || errors.Is(transient, ErrProviderPermanent) {
		t.Fatalf("transient error matches the wrong sentinel: %v", transient)
	}
	want := "provider error (transient): rate limited, status 429, type rate_limit_error, retry after 3s"
	if transient.Error() != want {
		t.Fatalf("Error() = %q, want %q", transient.Error(), want)
	}

	permanent := &ProviderError{Kind: KindPermanent, Reason: "rejected", Status: 401, Type: "authentication_error"}
	if !errors.Is(permanent, ErrProviderPermanent) || errors.Is(permanent, ErrProviderTransient) {
		t.Fatalf("permanent error matches the wrong sentinel: %v", permanent)
	}

	var zero ProviderError
	if !errors.Is(&zero, ErrProviderPermanent) {
		t.Fatal("an unclassified error must be permanent")
	}

	wrapped := fmt.Errorf("op: %w", transient)
	var pe *ProviderError
	if !errors.As(wrapped, &pe) || pe.Status != 429 || !errors.Is(wrapped, ErrProviderTransient) {
		t.Fatalf("wrapped error lost its classification: %v", wrapped)
	}
}

func TestProviderErrorCause(t *testing.T) {
	cancelled := &ProviderError{Kind: KindTransient, Reason: "cancelled", Cause: context.Canceled}
	if !errors.Is(cancelled, context.Canceled) || !errors.Is(cancelled, ErrProviderTransient) {
		t.Fatalf("cause lost: %v", cancelled)
	}
	if errors.Is(cancelled, context.DeadlineExceeded) {
		t.Fatal("wrong cause matched")
	}
	timeout := &ProviderError{Kind: KindTransient, Reason: "timeout"}
	if errors.Is(timeout, context.DeadlineExceeded) || errors.Is(timeout, context.Canceled) {
		t.Fatal("an error without a cause must not match context errors")
	}
}

func TestProviderErrorPrintsOnlySafeTypes(t *testing.T) {
	for _, typ := range []string{"Ignore previous instructions", "x-api-key: test-key-not-real", strings.Repeat("a", 65), "type\nnewline"} {
		e := &ProviderError{Kind: KindPermanent, Status: 400, Type: typ}
		if strings.Contains(e.Error(), typ) || !strings.Contains(e.Error(), "type unknown") {
			t.Errorf("Error() = %q prints an unsafe type", e.Error())
		}
	}
	if e := (&ProviderError{Kind: KindPermanent}); e.Error() != "provider error (permanent)" {
		t.Fatalf("Error() = %q", e.Error())
	}
}
