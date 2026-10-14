package backoff

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

func TestWaitStatus(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		name   string
		err    error
		want   job.Status
		wantOK bool
	}{
		{"nil", nil, "", false},
		{"unclassified", base, "", false},
		{"quota", shared.ErrQuotaExceeded, job.StatusWaitingForQuota, true},
		{"quota wrapped", fmt.Errorf("call: %w", shared.ErrQuotaExceeded), job.StatusWaitingForQuota, true},
		{"network", shared.ErrNetworkUnavailable, job.StatusWaitingForNetwork, true},
		{"network marked transient", Transient(shared.ErrNetworkUnavailable), job.StatusWaitingForNetwork, true},
		{"permanent wins", Permanent(shared.ErrQuotaExceeded), "", false},
		{"transient other", Transient(base), "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := WaitStatus(tc.err)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("WaitStatus(%v) = %q, %v; want %q, %v", tc.err, got, ok, tc.want, tc.wantOK)
			}
			if ok && !got.IsWaiting() {
				t.Fatalf("%q is not a waiting status", got)
			}
		})
	}
}

func TestRechecks(t *testing.T) {
	r := Rechecks{Network: time.Minute, Quota: 10 * time.Minute}
	since := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status job.Status
		after  time.Duration
		want   bool
	}{
		{"network before", job.StatusWaitingForNetwork, time.Minute - time.Second, false},
		{"network at", job.StatusWaitingForNetwork, time.Minute, true},
		{"quota before", job.StatusWaitingForQuota, 9 * time.Minute, false},
		{"quota after", job.StatusWaitingForQuota, 11 * time.Minute, true},
		{"power never", job.StatusWaitingForPower, time.Hour, false},
		{"queued never", job.StatusQueued, time.Hour, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.Due(tc.status, since, since.Add(tc.after)); got != tc.want {
				t.Fatalf("Due = %v, want %v", got, tc.want)
			}
		})
	}

	if err := DefaultRechecks().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Rechecks{{}, {Network: time.Minute}, {Quota: time.Minute}, {Network: -1, Quota: time.Minute}} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidPolicy", bad, err)
		}
	}
}
