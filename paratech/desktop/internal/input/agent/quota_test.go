package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	cwvalues "paratech/desktop/internal/domain/coursework/values"
)

// memQuota is «Обновить» of the connections page: the reading and the error it answers.
type memQuota struct {
	reading cwvalues.QuotaReading
	err     error
	calls   int
}

func (q *memQuota) RefreshQuota(context.Context) (cwvalues.QuotaReading, error) {
	q.calls++
	return q.reading, q.err
}

func quotaSnapshot(t *testing.T, remaining, limit int64, end time.Time) cwvalues.QuotaSnapshot {
	t.Helper()
	amount := func(v int64) cwvalues.QuotaAmount {
		a, err := cwvalues.NewQuotaAmount(cwvalues.QuotaUnitTokens, v)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	s, err := cwvalues.NewQuotaSnapshot(amount(remaining), amount(123), amount(limit), end)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func currentReading(t *testing.T, s cwvalues.QuotaSnapshot) cwvalues.QuotaReading {
	t.Helper()
	r, err := cwvalues.QuotaReading{}.Read(s, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// quota_status shows the remaining share and the day it renews in the student's zone, never an amount, a unit or a key.
func TestQuotaStatus(t *testing.T) {
	tests := []struct {
		name     string
		snapshot func(t *testing.T) cwvalues.QuotaSnapshot
		want     string
	}{
		{"a quarter left, renews on the 1st (Moscow)", func(t *testing.T) cwvalues.QuotaSnapshot {
			return quotaSnapshot(t, 2_500_000, 10_000_000, time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC))
		}, `{"remaining_percent":25,"resets":"2026-11-01"}`},
		{"rounded down, does not renew", func(t *testing.T) cwvalues.QuotaSnapshot {
			return quotaSnapshot(t, 999, 1000, time.Time{})
		}, `{"remaining_percent":99}`},
		{"used up", func(t *testing.T) cwvalues.QuotaSnapshot {
			return quotaSnapshot(t, 0, 1000, time.Time{})
		}, `{"remaining_percent":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f := newControlsWorld(t)
			f.quota.reading = currentReading(t, tt.snapshot(t))
			out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolQuotaStatus, `{}`, actionpolicy.ClassR))
			if out.Result.IsError || out.Result.ResultJSON != tt.want || len(out.Seen) != 0 {
				t.Fatalf("result = %+v, want %s", out.Result, tt.want)
			}
			for _, leak := range []string{"token", "limit", "reserved", "123"} {
				if strings.Contains(out.Result.ResultJSON, leak) {
					t.Fatalf("result shows %q: %s", leak, out.Result.ResultJSON)
				}
			}
		})
	}
}

// A Hub that is not set up is "unavailable"; any other failure "failed", also when a stale value is still kept, and a
// limit of zero, which has no honest share.
func TestQuotaStatusErrors(t *testing.T) {
	stale := currentReading(t, quotaSnapshot(t, 500, 1000, time.Time{}))
	noLimit := currentReading(t, quotaSnapshot(t, 0, 0, time.Time{}))
	tests := []struct {
		name    string
		reading cwvalues.QuotaReading
		err     error
		want    values.Refusal
	}{
		{"not configured", cwvalues.QuotaReading{}.Failed(cwvalues.QuotaNotConfigured, now), errors.New("no url"), values.RefuseUnavailable},
		{"no key", cwvalues.QuotaReading{}.Failed(cwvalues.QuotaNoKey, now), errors.New("no key"), values.RefuseUnavailable},
		{"key rejected", cwvalues.QuotaReading{}.Failed(cwvalues.QuotaKeyRejected, now), errors.New("rejected"), values.RefuseUnavailable},
		{"unreachable, stale value kept", stale.Failed(cwvalues.QuotaUnreachable, now), errors.New("timeout"), values.RefuseFailed},
		{"busy", stale, errors.New("busy"), values.RefuseFailed},
		{"stale without an error", stale.Failed(cwvalues.QuotaFailed, now), nil, values.RefuseFailed},
		{"no snapshot without an error", cwvalues.QuotaReading{}, nil, values.RefuseFailed},
		{"no limit", noLimit, nil, values.RefuseFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f := newControlsWorld(t)
			f.quota.reading, f.quota.err = tt.reading, tt.err
			out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolQuotaStatus, `{}`, actionpolicy.ClassR))
			if why, ok := values.RefusalOf(out.Result); !ok || why != tt.want {
				t.Fatalf("result = %+v, want %s", out.Result, tt.want)
			}
		})
	}

	w := newWorld(t)
	out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolQuotaStatus, `{}`, actionpolicy.ClassR))
	if out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("without the use case = %s", out.Result.ResultJSON)
	}
}
