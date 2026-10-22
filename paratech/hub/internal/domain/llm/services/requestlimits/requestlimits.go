// Package requestlimits holds the limits of one gateway call (PRD section 13.2): the most a call may cost, which is the
// amount the gateway reserves (ADR-0005(b)), and the per-tester rate policy.
package requestlimits

import (
	"errors"
	"fmt"
	"math"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

// Errors of the service (ARCH-18).
var (
	// ErrInvalidRate means a rate policy value is out of range.
	ErrInvalidRate = errors.New("invalid rate limit")

	// ErrInvalidMaxOutput means an effective output cap is not positive.
	ErrInvalidMaxOutput = errors.New("invalid effective max output tokens")
)

// FramingTokens is the allowance for what the provider adds around the two texts: the system and message wrappers and
// the placeholder sent for empty data. Generous on purpose; it only makes the hold larger.
const FramingTokens = 1024

// Rate policy bounds.
const (
	MaxPerMinute = 6000
	MaxBurst     = 1000
)

// MaxInputTokens is a safe upper bound of the input tokens of spec without a tokenizer: one token per byte of text
// (provider tokens are byte sequences of at least one byte) plus FramingTokens.
func MaxInputTokens(spec values.Spec) int64 {
	return spec.PayloadBytes() + FramingTokens
}

// Reservation is the most a call of spec may cost with the effective output cap maxOutput (the allowlist's clamped
// value): MaxInputTokens plus maxOutput. The gateway reserves it before the provider is called.
func Reservation(spec values.Spec, maxOutput int64) (int64, error) {
	in := MaxInputTokens(spec)
	if maxOutput <= 0 || maxOutput > math.MaxInt64-in {
		return 0, ErrInvalidMaxOutput
	}
	return in + maxOutput, nil
}

// AgentReservation is the most an agent step may cost: one token per byte of everything call sends (prompt, tools,
// context, history) plus FramingTokens (message wrappers and the provider's own tool-use prompt) plus the effective
// output cap. The step reserves it before the provider is called (ADR-0023).
func AgentReservation(call values.AgentCall) (int64, error) {
	in := call.InputBytes() + FramingTokens
	if call.MaxOutput() <= 0 || call.MaxOutput() > math.MaxInt64-in {
		return 0, ErrInvalidMaxOutput
	}
	return in + call.MaxOutput(), nil
}

// Rate is the per-tester rate policy: on average PerMinute calls a minute, with up to Burst calls at once. It is a
// GCRA token bucket whose whole state per tester is one time (the theoretical arrival time, TAT).
type Rate struct {
	interval time.Duration // time that earns one call
	window   time.Duration // interval x burst: how far the TAT may run ahead of now
}

// NewRate validates the policy: 1..MaxPerMinute calls a minute, burst 1..MaxBurst.
func NewRate(perMinute, burst int) (Rate, error) {
	if perMinute < 1 || perMinute > MaxPerMinute {
		return Rate{}, fmt.Errorf("%w: calls per minute must be within 1..%d", ErrInvalidRate, MaxPerMinute)
	}
	if burst < 1 || burst > MaxBurst {
		return Rate{}, fmt.Errorf("%w: burst must be within 1..%d", ErrInvalidRate, MaxBurst)
	}
	interval := time.Minute / time.Duration(perMinute)
	return Rate{interval: interval, window: interval * time.Duration(burst)}, nil
}

// Take decides one call at now for a tester whose state is tat (the zero time for a tester never seen). It returns
// the new state; a refused call keeps the state and says how long to wait. A tat further ahead than the window (the
// clock went back) is treated as now, so a clock step never locks a tester out.
func (r Rate) Take(tat, now time.Time) (next time.Time, retryAfter time.Duration, ok bool) {
	if tat.Before(now) || tat.Sub(now) > r.window {
		tat = now
	}
	next = tat.Add(r.interval)
	if ahead := next.Sub(now); ahead > r.window {
		return tat, ahead - r.window, false
	}
	return next, 0, true
}

// Idle reports whether the state tat is the same as no state at now, so a limiter may forget it.
func (r Rate) Idle(tat, now time.Time) bool { return !tat.After(now) }

// Valid reports whether r was built by NewRate.
func (r Rate) Valid() bool { return r.interval > 0 && r.window >= r.interval }
