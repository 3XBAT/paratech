package values

import (
	"errors"
	"testing"
	"time"
)

func tokens(t *testing.T, v int64) QuotaAmount {
	t.Helper()
	a, err := NewQuotaAmount(QuotaUnitTokens, v)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewQuotaAmount(t *testing.T) {
	tests := []struct {
		name  string
		unit  QuotaUnit
		value int64
		ok    bool
	}{
		{"tokens", QuotaUnitTokens, 1000, true},
		{"zero", QuotaUnitTokens, 0, true},
		{"negative", QuotaUnitTokens, -1, false},
		{"unknown unit", QuotaUnit("credits"), 1, false},
		{"no unit", "", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewQuotaAmount(tt.unit, tt.value)
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidQuota)) {
				t.Fatalf("NewQuotaAmount = %v, want ok %v", err, tt.ok)
			}
			if tt.ok && (a.Unit() != tt.unit || a.Value() != tt.value) {
				t.Fatalf("amount = %v %d", a.Unit(), a.Value())
			}
		})
	}
}

func TestNewQuotaSnapshot(t *testing.T) {
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name                       string
		remaining, reserved, limit QuotaAmount
		ok                         bool
	}{
		{"consistent", tokens(t, 600), tokens(t, 400), tokens(t, 1000), true},
		{"all used", tokens(t, 0), tokens(t, 0), tokens(t, 1000), true},
		{"limit lowered under a hold", tokens(t, 0), tokens(t, 1500), tokens(t, 1000), true},
		{"remaining above the limit", tokens(t, 1001), tokens(t, 0), tokens(t, 1000), false},
		{"missing remaining", QuotaAmount{}, tokens(t, 0), tokens(t, 1000), false},
		{"missing reserved", tokens(t, 1), QuotaAmount{}, tokens(t, 1000), false},
		{"missing limit", tokens(t, 0), tokens(t, 0), QuotaAmount{}, false},
		{"mixed units", tokens(t, 1), QuotaAmount{unit: "credits"}, tokens(t, 1000), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewQuotaSnapshot(tt.remaining, tt.reserved, tt.limit, end)
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidQuota)) {
				t.Fatalf("NewQuotaSnapshot = %v, want ok %v", err, tt.ok)
			}
			if !tt.ok {
				return
			}
			if s.Remaining() != tt.remaining || s.Reserved() != tt.reserved || s.Limit() != tt.limit || s.IsZero() {
				t.Fatalf("snapshot %+v", s)
			}
			if got, ok := s.PeriodEnd(); !ok || !got.Equal(end) {
				t.Fatalf("period end %v %v", got, ok)
			}
		})
	}

	s, err := NewQuotaSnapshot(tokens(t, 1), tokens(t, 0), tokens(t, 1), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.PeriodEnd(); ok {
		t.Fatal("a quota without a period end renews")
	}
	if !(QuotaSnapshot{}).IsZero() {
		t.Fatal("the zero snapshot is not zero")
	}
}

// The reading keeps a stale snapshot through outages and drops it when it no longer belongs to the configured
// account (ARCH-62: every transition).
func TestQuotaReading(t *testing.T) {
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Minute), t0.Add(2*time.Minute)
	snap, err := NewQuotaSnapshot(tokens(t, 600), tokens(t, 400), tokens(t, 1000), time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	var zero QuotaReading
	if zero.Outcome() != QuotaUnchecked || zero.Stale() || !zero.CheckedAt().IsZero() {
		t.Fatalf("zero reading %+v", zero)
	}
	if _, ok := zero.Snapshot(); ok {
		t.Fatal("the zero reading has a snapshot")
	}
	if got, err := zero.Read(QuotaSnapshot{}, t0); !errors.Is(err, ErrInvalidQuota) || got != zero {
		t.Fatalf("Read(zero) = %+v, %v", got, err)
	}

	read, err := zero.Read(snap, t0)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := read.Snapshot(); !ok || got != snap || read.Outcome() != QuotaCurrent || read.Stale() ||
		!read.FetchedAt().Equal(t0) || !read.CheckedAt().Equal(t0) {
		t.Fatalf("after a read: %+v", read)
	}

	tests := []struct {
		name    string
		outcome QuotaOutcome
		want    QuotaOutcome
		keeps   bool
	}{
		{"unreachable keeps a stale value", QuotaUnreachable, QuotaUnreachable, true},
		{"failure keeps a stale value", QuotaFailed, QuotaFailed, true},
		{"not configured drops it", QuotaNotConfigured, QuotaNotConfigured, false},
		{"no key drops it", QuotaNoKey, QuotaNoKey, false},
		{"rejected key drops it", QuotaKeyRejected, QuotaKeyRejected, false},
		{"current is not a failure", QuotaCurrent, QuotaFailed, true},
		{"unchecked is not a failure", QuotaUnchecked, QuotaFailed, true},
		{"unknown outcome", QuotaOutcome(99), QuotaFailed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read.Failed(tt.outcome, t1)
			s, ok := got.Snapshot()
			if got.Outcome() != tt.want || ok != tt.keeps || got.Stale() != tt.keeps || !got.CheckedAt().Equal(t1) {
				t.Fatalf("Failed(%d) = %+v", tt.outcome, got)
			}
			if tt.keeps && (s != snap || !got.FetchedAt().Equal(t0)) {
				t.Fatalf("the stale value lost its data or time: %+v", got)
			}
			if !tt.keeps && !got.FetchedAt().IsZero() {
				t.Fatalf("a dropped value kept its time: %+v", got)
			}

			// A later read makes it current again.
			again, err := got.Read(snap, t2)
			if err != nil || again.Outcome() != QuotaCurrent || again.Stale() || !again.FetchedAt().Equal(t2) {
				t.Fatalf("read after a failure = %+v, %v", again, err)
			}
		})
	}
}
