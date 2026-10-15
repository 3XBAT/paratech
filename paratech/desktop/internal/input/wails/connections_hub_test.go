package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/coursework/values"
)

func quotaReading(t *testing.T, period bool, failure values.QuotaOutcome) values.QuotaReading {
	t.Helper()
	amount := func(v int64) values.QuotaAmount {
		a, err := values.NewQuotaAmount(values.QuotaUnitTokens, v)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	var end time.Time
	if period {
		end = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	}
	snap, err := values.NewQuotaSnapshot(amount(600), amount(400), amount(1000), end)
	if err != nil {
		t.Fatal(err)
	}
	fetched := time.Date(2026, 10, 15, 12, 30, 5, 999, time.FixedZone("MSK", 3*3600))
	r, err := values.QuotaReading{}.Read(snap, fetched)
	if err != nil {
		t.Fatal(err)
	}
	if failure != values.QuotaCurrent {
		r = r.Failed(failure, fetched.Add(time.Minute))
	}
	return r
}

func TestQuotaJSON(t *testing.T) {
	tests := []struct {
		name string
		r    values.QuotaReading
		want string
	}{
		{"unchecked", values.QuotaReading{}, `{"status":"unchecked","stale":false,"checkedAt":"","values":null}`},
		{"current with a period", quotaReading(t, true, values.QuotaCurrent),
			`{"status":"current","stale":false,"checkedAt":"2026-10-15T09:30:05Z","values":{"unit":"tokens","remaining":600,` +
				`"reserved":400,"limit":1000,"periodEnd":"2026-11-01T00:00:00Z","fetchedAt":"2026-10-15T09:30:05Z"}}`},
		{"offline keeps stale values", quotaReading(t, false, values.QuotaUnreachable),
			`{"status":"offline","stale":true,"checkedAt":"2026-10-15T09:31:05Z","values":{"unit":"tokens","remaining":600,` +
				`"reserved":400,"limit":1000,"periodEnd":"","fetchedAt":"2026-10-15T09:30:05Z"}}`},
		{"failed", quotaReading(t, false, values.QuotaFailed), `"status":"failed","stale":true`},
		{"rejected key shows nothing", quotaReading(t, false, values.QuotaKeyRejected),
			`{"status":"keyRejected","stale":false,"checkedAt":"2026-10-15T09:31:05Z","values":null}`},
		{"no key", quotaReading(t, false, values.QuotaNoKey), `"status":"noKey","stale":false`},
		{"not configured", quotaReading(t, false, values.QuotaNotConfigured), `"status":"notConfigured","stale":false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(quotaJSON(tt.r))
			if err != nil || !strings.Contains(string(raw), tt.want) {
				t.Fatalf("JSON = %s (%v), want %s", raw, err, tt.want)
			}
		})
	}
	if got := quotaStatus(values.QuotaOutcome(99)); got != "failed" {
		t.Fatalf("unknown outcome = %q", got)
	}
}

func TestHubKeyResults(t *testing.T) {
	tests := []struct {
		name   string
		status connections.SecretStatus
		err    error
		want   SecretResult
	}{
		{"saved", connections.SecretSet, nil, SecretResult{Status: "set", Message: msgHubKeyStored}},
		{"invalid", connections.SecretUnset, fmt.Errorf("op: %w", connections.ErrInvalidSecret),
			SecretResult{Status: "unset", Message: msgHubKeyRefused}},
		{"unsupported", connections.SecretUnsupported, fmt.Errorf("op: %w", connections.ErrSecretsUnsupported),
			SecretResult{Status: "unsupported", Message: msgHubKeyNoStore}},
		{"unavailable", connections.SecretUnavailable, fmt.Errorf("op: %w: locked", connections.ErrSecretsUnavailable),
			SecretResult{Status: "unavailable", Message: msgStoreDown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeConnections{status: tt.status, secretErr: tt.err}
			got, err := NewConnectionsService(f, discard()).SetHubKey(context.Background(), "la_key")
			if err != nil || got != tt.want {
				t.Fatalf("SetHubKey = %+v, %v; want %+v", got, err, tt.want)
			}
			if len(f.keys) != 1 || f.keys[0] != "la_key" || len(f.passwords) != 0 {
				t.Fatal("the key did not reach the hub slot unchanged")
			}
		})
	}

	s := NewConnectionsService(&fakeConnections{status: connections.SecretUnset}, discard())
	if got, err := s.RemoveHubKey(context.Background()); err != nil || got != (SecretResult{Status: "unset", Message: msgHubKeyRemoved}) {
		t.Fatalf("RemoveHubKey = %+v, %v", got, err)
	}
	if msgHubKeyRefused == msgMailRefused || msgHubKeyNoStore == msgStoreMissing {
		t.Fatal("the key and the password share a text that names the other")
	}
}

// Every result of a refresh is a normal answer with its own text and the quota shown afterwards.
func TestRefreshQuotaMessages(t *testing.T) {
	current := quotaReading(t, false, values.QuotaCurrent)
	s := NewConnectionsService(&fakeConnections{reading: current}, discard())
	got, err := s.RefreshQuota(context.Background())
	if err != nil || !got.OK || got.Message != msgQuotaOK || !sameJSON(got.Quota, quotaJSON(current)) {
		t.Fatalf("RefreshQuota ok = %+v, %v", got, err)
	}

	stale := quotaReading(t, false, values.QuotaUnreachable)
	results := []error{
		connections.ErrQuotaBusy, connections.ErrQuotaSuperseded, connections.ErrHubNotConfigured,
		connections.ErrHubKeyMissing, connections.ErrHubKeyRejected, connections.ErrHubUnreachable,
		connections.ErrHubUnavailable, connections.ErrHubRefused, connections.ErrSecretsUnsupported,
		connections.ErrSecretsUnavailable,
	}
	if len(results) != len(quotaMessages) {
		t.Fatalf("%d results tested, %d texts", len(results), len(quotaMessages))
	}
	seen := map[string]bool{msgQuotaOK: true, msgQuotaFailed: true}
	for _, r := range results {
		s := NewConnectionsService(&fakeConnections{reading: stale, quotaErr: fmt.Errorf("bridge: %w", r)}, discard())
		got, err := s.RefreshQuota(context.Background())
		if err != nil || got.OK || got.Message == "" || seen[got.Message] || !sameJSON(got.Quota, quotaJSON(stale)) {
			t.Errorf("%v: RefreshQuota = %+v, %v; want its own text and the stale values", r, got, err)
		}
		seen[got.Message] = true
	}
}

// An unclassified failure is logged and answered with a fixed text; neither the answer nor the log carries the key.
func TestRefreshQuotaHidesTheCause(t *testing.T) {
	var logs bytes.Buffer
	cause := errors.New(`open C:\Users\student\db: locked`)
	f := &fakeConnections{reading: quotaReading(t, false, values.QuotaFailed), quotaErr: cause}
	s := NewConnectionsService(f, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	got, err := s.RefreshQuota(context.Background())
	if err != nil || got.OK || got.Message != msgQuotaFailed || got.Quota.Values == nil || !got.Quota.Stale {
		t.Fatalf("RefreshQuota = %+v, %v", got, err)
	}
	if raw, _ := json.Marshal(got); strings.Contains(string(raw), "student") {
		t.Fatalf("answer %s carries the cause", raw)
	}
	if !strings.Contains(logs.String(), "locked") {
		t.Fatalf("the cause must be logged, got %q", logs.String())
	}
}

func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}
