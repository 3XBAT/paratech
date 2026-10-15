package connections

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeQuota answers the quota reads; block, when set, holds every read until it is closed, and during runs inside
// the read.
type fakeQuota struct {
	mu       sync.Mutex
	snap     values.QuotaSnapshot
	err      error
	block    chan struct{}
	started  chan struct{}
	during   func()
	waitDone bool // every read waits for its context to end
	deadline bool // the context of the last read had a deadline
	calls    int
}

func (q *fakeQuota) Remaining(ctx context.Context) (values.QuotaSnapshot, error) {
	q.mu.Lock()
	q.calls++
	_, q.deadline = ctx.Deadline()
	snap, err, block, started, during, waitDone := q.snap, q.err, q.block, q.started, q.during, q.waitDone
	q.mu.Unlock()
	if waitDone {
		<-ctx.Done()
		return values.QuotaSnapshot{}, fmt.Errorf("hubclient: get remaining: %w", ctx.Err())
	}
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		<-block
	}
	if during != nil {
		during()
	}
	return snap, err
}

func (q *fakeQuota) answer(snap values.QuotaSnapshot, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.snap, q.err = snap, err
}

const testHubKey = "la_test_conn_hubkey_8c2d"

func snapshot(t *testing.T, remaining int64) values.QuotaSnapshot {
	t.Helper()
	amount := func(v int64) values.QuotaAmount {
		a, err := values.NewQuotaAmount(values.QuotaUnitTokens, v)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	s, err := values.NewQuotaSnapshot(amount(remaining), amount(100), amount(1000), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The key goes only into the hub store, under the entry the Hub client reads; the page learns only its status.
func TestHubKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{})

	status, err := f.svc.SetHubKey(ctx, "  "+testHubKey+"\r\n")
	if err != nil || status != SecretSet {
		t.Fatalf("SetHubKey = %v, %v", status, err)
	}
	if got, err := f.hub.Get(ctx, DefaultHubSecretName); err != nil || got != testHubKey || f.hub.Len() != 1 {
		t.Fatalf("hub store holds %d secrets, %q under %q: %v", f.hub.Len(), got, DefaultHubSecretName, err)
	}
	if f.store.Len() != 0 || f.repo.saves != 0 {
		t.Fatal("the key reached the mail store or the settings")
	}
	if v, err := f.svc.Get(ctx); err != nil || v.HubKey != SecretSet || v.MailPassword != SecretUnset {
		t.Fatalf("Get = %+v, %v", v, err)
	}

	// A key that could not be sent is refused before the store, changes nothing and reports what is stored.
	for _, bad := range []string{"", " \r\n", "la key", "abc\r\nX-Evil: 1", "ключ", strings.Repeat("k", values.MaxHubAPIKeyLen+1)} {
		status, err = f.svc.SetHubKey(ctx, bad)
		if !errors.Is(err, ErrInvalidSecret) || !errors.Is(err, shared.ErrInvalidSecret) || status != SecretSet {
			t.Fatalf("SetHubKey(%d bytes) = %v, %v", len(bad), status, err)
		}
		if strings.TrimSpace(bad) != "" && strings.Contains(err.Error(), strings.TrimSpace(bad)) {
			t.Fatalf("error %q quotes the value", err)
		}
	}
	if got, _ := f.hub.Get(ctx, DefaultHubSecretName); got != testHubKey {
		t.Fatal("a refused value replaced the key")
	}

	status, err = f.svc.RemoveHubKey(ctx)
	if err != nil || status != SecretUnset || f.hub.Len() != 0 {
		t.Fatalf("RemoveHubKey = %v, %v (store %d)", status, err, f.hub.Len())
	}
	if status, err = f.svc.RemoveHubKey(ctx); err != nil || status != SecretUnset {
		t.Fatalf("RemoveHubKey again = %v, %v", status, err)
	}
	if status, err = f.svc.SetHubKey(ctx, ""); !errors.Is(err, ErrInvalidSecret) || status != SecretUnset {
		t.Fatalf("SetHubKey(empty) without a key = %v, %v", status, err)
	}
}

func TestHubSecretNameFromConfig(t *testing.T) {
	f := newFixture(t, Config{HubSecretName: "tester-key"})
	if _, err := f.svc.SetHubKey(context.Background(), testHubKey); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.hub.Exists(context.Background(), "tester-key"); !ok {
		t.Fatal("the key is not under the configured entry")
	}
}

// Off Windows there is no store for the key: the section says so; a failing store is "unavailable".
func TestHubKeyStoreFailures(t *testing.T) {
	ctx := context.Background()
	failing := testenv.NewMemCredentials()
	failing.Fail(fmt.Errorf("%w: vault locked", shared.ErrStorage))
	tests := []struct {
		name   string
		store  SecretWriter
		status SecretStatus
		want   error
	}{
		{"unsupported", unsupportedStore{}, SecretUnsupported, ErrSecretsUnsupported},
		{"failing", failing, SecretUnavailable, ErrSecretsUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := New(Deps{
				Settings: &fakeRepo{}, Clock: testenv.NewFakeClock(start), MailSecret: testenv.NewMemCredentials(),
				MailCheck: &fakeChecker{}, HubSecret: tt.store, Quota: &fakeQuota{},
			}, Config{})
			if err != nil {
				t.Fatal(err)
			}
			v, err := svc.Get(ctx)
			if err != nil || v.HubKey != tt.status || v.MailPassword != SecretUnset {
				t.Fatalf("Get = %+v, %v; want hub key status %v", v, err, tt.status)
			}
			status, err := svc.SetHubKey(ctx, testHubKey)
			if !errors.Is(err, tt.want) || status != tt.status || strings.Contains(err.Error(), testHubKey) {
				t.Fatalf("SetHubKey = %v, %v", status, err)
			}
			if status, err = svc.RemoveHubKey(ctx); !errors.Is(err, tt.want) || status != tt.status {
				t.Fatalf("RemoveHubKey = %v, %v", status, err)
			}
			if status, err = svc.SetHubKey(ctx, "bad key"); !errors.Is(err, ErrInvalidSecret) || status != tt.status {
				t.Fatalf("SetHubKey(invalid) = %v, %v", status, err)
			}
		})
	}
}

func TestRefreshQuota(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{})
	if v, _ := f.svc.Get(ctx); v.Quota != (values.QuotaReading{}) {
		t.Fatalf("before a refresh: %+v", v.Quota)
	}

	snap := snapshot(t, 600)
	f.quota.answer(snap, nil)
	got, err := f.svc.RefreshQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got.Snapshot(); !ok || s != snap || got.Outcome() != values.QuotaCurrent || got.Stale() ||
		!got.FetchedAt().Equal(start) {
		t.Fatalf("RefreshQuota = %+v", got)
	}
	if !f.quota.deadline {
		t.Fatal("the read has no bound")
	}
	if v, err := f.svc.Get(ctx); err != nil || v.Quota != got {
		t.Fatalf("Get after a refresh = %+v, %v", v.Quota, err)
	}
	if f.quota.calls != 1 {
		t.Fatalf("Get called the Hub: %d reads", f.quota.calls)
	}
}

// Every result: what is shown afterwards and which error the page gets.
func TestRefreshQuotaResults(t *testing.T) {
	unclassified := errors.New("hub settings cannot be read")
	tests := []struct {
		name    string
		snap    values.QuotaSnapshot
		err     error
		is      []error
		outcome values.QuotaOutcome
		keeps   bool
	}{
		{"not configured", values.QuotaSnapshot{}, ErrHubNotConfigured, []error{ErrHubNotConfigured}, values.QuotaNotConfigured, false},
		{"no key", values.QuotaSnapshot{}, ErrHubKeyMissing, []error{ErrHubKeyMissing}, values.QuotaNoKey, false},
		{"key rejected", values.QuotaSnapshot{}, ErrHubKeyRejected, []error{ErrHubKeyRejected}, values.QuotaKeyRejected, false},
		{"unreachable", values.QuotaSnapshot{}, ErrHubUnreachable, []error{ErrHubUnreachable}, values.QuotaUnreachable, true},
		{"unavailable", values.QuotaSnapshot{}, ErrHubUnavailable, []error{ErrHubUnavailable}, values.QuotaUnreachable, true},
		{"refused", values.QuotaSnapshot{}, ErrHubRefused, []error{ErrHubRefused}, values.QuotaFailed, true},
		{"store unsupported", values.QuotaSnapshot{}, ErrSecretsUnsupported, []error{ErrSecretsUnsupported}, values.QuotaFailed, true},
		{"store unavailable", values.QuotaSnapshot{}, ErrSecretsUnavailable, []error{ErrSecretsUnavailable}, values.QuotaFailed, true},
		{"unclassified", values.QuotaSnapshot{}, unclassified, []error{unclassified}, values.QuotaFailed, true},
		{"empty snapshot", values.QuotaSnapshot{}, nil, []error{values.ErrInvalidQuota}, values.QuotaFailed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t, Config{})
			first := snapshot(t, 600)
			f.quota.answer(first, nil)
			if _, err := f.svc.RefreshQuota(ctx); err != nil {
				t.Fatal(err)
			}
			f.clock.Advance(time.Minute)

			f.quota.answer(tt.snap, fmt.Errorf("bridge: %w", tt.err))
			if tt.err == nil {
				f.quota.answer(tt.snap, nil)
			}
			got, err := f.svc.RefreshQuota(ctx)
			for _, w := range tt.is {
				if !errors.Is(err, w) {
					t.Fatalf("RefreshQuota = %v, want %v", err, w)
				}
			}
			if s, ok := got.Snapshot(); got.Outcome() != tt.outcome || ok != tt.keeps || got.Stale() != tt.keeps ||
				(ok && (s != first || !got.FetchedAt().Equal(start))) || !got.CheckedAt().Equal(start.Add(time.Minute)) {
				t.Fatalf("RefreshQuota = %+v, want outcome %v keeping the first value %v", got, tt.outcome, tt.keeps)
			}
			if v, _ := f.svc.Get(ctx); v.Quota != got {
				t.Fatalf("Get = %+v, want %+v", v.Quota, got)
			}
		})
	}
}

// A refresh that hangs ends at the configured bound and counts as a Hub that did not answer.
func TestRefreshQuotaIsBounded(t *testing.T) {
	f := newFixture(t, Config{RefreshTimeout: 50 * time.Millisecond})
	f.quota.waitDone = true
	got, err := f.svc.RefreshQuota(context.Background())
	if !errors.Is(err, ErrHubUnreachable) || !errors.Is(err, context.DeadlineExceeded) || got.Outcome() != values.QuotaUnreachable {
		t.Fatalf("RefreshQuota = %+v, %v; want unreachable", got, err)
	}
}

// A done context of the caller (the page went away) is not a result of the Hub: nothing changes.
func TestRefreshQuotaWithADoneContext(t *testing.T) {
	f := newFixture(t, Config{})
	f.quota.answer(snapshot(t, 600), nil)
	before, err := f.svc.RefreshQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := f.svc.RefreshQuota(ctx); !errors.Is(err, context.Canceled) || got != before || f.quota.calls != 1 {
		t.Fatalf("RefreshQuota(cancelled) = %+v, %v after %d reads", got, err, f.quota.calls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	f.quota.mu.Lock()
	f.quota.during = cancel
	f.quota.err = context.Canceled
	f.quota.mu.Unlock()
	if got, err := f.svc.RefreshQuota(ctx); !errors.Is(err, context.Canceled) || got != before {
		t.Fatalf("RefreshQuota cancelled while reading = %+v, %v", got, err)
	}
}

func TestRefreshQuotaOneAtATime(t *testing.T) {
	f := newFixture(t, Config{})
	f.quota.answer(snapshot(t, 600), nil)
	f.quota.block, f.quota.started = make(chan struct{}), make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.RefreshQuota(context.Background())
		done <- err
	}()
	<-f.quota.started

	if _, err := f.svc.RefreshQuota(context.Background()); !errors.Is(err, ErrQuotaBusy) {
		t.Fatalf("second RefreshQuota = %v, want ErrQuotaBusy", err)
	}
	close(f.quota.block)
	if err := <-done; err != nil {
		t.Fatalf("first RefreshQuota = %v", err)
	}
	if f.quota.calls != 1 {
		t.Fatalf("%d reads, want 1", f.quota.calls)
	}
}

// The quota shown belongs to one key and one Hub: a change of either forgets it; a refused change does not.
func TestKeyOrAddressChangeForgetsTheQuota(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{})
	f.repo.stored = settings.State{Version: 1, HubURL: "https://hub.example.org", SMTPSecurity: settings.DefaultMailSecurity}
	f.quota.answer(snapshot(t, 600), nil)
	refresh := func() values.QuotaReading {
		t.Helper()
		r, err := f.svc.RefreshQuota(ctx)
		if err != nil || r.Outcome() != values.QuotaCurrent {
			t.Fatalf("RefreshQuota = %+v, %v", r, err)
		}
		return r
	}
	shown := func() values.QuotaReading {
		t.Helper()
		v, err := f.svc.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v.Quota
	}

	tests := []struct {
		name   string
		change func() (values.QuotaReading, error)
		forget bool
	}{
		{"new key", func() (values.QuotaReading, error) { _, err := f.svc.SetHubKey(ctx, testHubKey); return shown(), err }, true},
		{"refused key", func() (values.QuotaReading, error) {
			_, err := f.svc.SetHubKey(ctx, "bad key")
			return shown(), ignore(err, ErrInvalidSecret)
		}, false},
		{"removed key", func() (values.QuotaReading, error) { _, err := f.svc.RemoveHubKey(ctx); return shown(), err }, true},
		{"same address", func() (values.QuotaReading, error) {
			v, err := f.svc.SaveHubURL(ctx, "https://hub.example.org")
			return v.Quota, err
		}, false},
		{"refused address", func() (values.QuotaReading, error) {
			_, err := f.svc.SaveHubURL(ctx, "http://hub.example.org")
			return shown(), ignore(err, settings.ErrInvalidSettings)
		}, false},
		{"new address", func() (values.QuotaReading, error) {
			v, err := f.svc.SaveHubURL(ctx, "https://other.example.org")
			return v.Quota, err
		}, true},
		{"address removed", func() (values.QuotaReading, error) {
			v, err := f.svc.SaveHubURL(ctx, "")
			return v.Quota, err
		}, true},
		{"mail settings", func() (values.QuotaReading, error) {
			v, err := f.svc.SaveMail(ctx, MailInput{Security: settings.MailSecurityTLS})
			return v.Quota, err
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := refresh()
			got, err := tt.change()
			if err != nil {
				t.Fatal(err)
			}
			if tt.forget && (got != values.QuotaReading{} || shown() != values.QuotaReading{}) {
				t.Fatalf("after the change the quota is still shown: %+v", got)
			}
			if !tt.forget && (got != before || shown() != before) {
				t.Fatalf("the quota changed: %+v, want %+v", got, before)
			}
		})
	}
}

func ignore(err, want error) error {
	if errors.Is(err, want) {
		return nil
	}
	return fmt.Errorf("unexpected: %w", err)
}

// An answer that arrives after the key changed belongs to the old key: it is discarded, not shown as current.
func TestRefreshAcrossAKeyChangeIsDiscarded(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{})
	f.quota.answer(snapshot(t, 600), nil)
	f.quota.during = func() {
		if _, err := f.svc.SetHubKey(ctx, testHubKey); err != nil {
			t.Error(err)
		}
	}
	got, err := f.svc.RefreshQuota(ctx)
	if !errors.Is(err, ErrQuotaSuperseded) || got != (values.QuotaReading{}) {
		t.Fatalf("RefreshQuota = %+v, %v; want ErrQuotaSuperseded and nothing shown", got, err)
	}
	f.quota.during = nil
	if got, err := f.svc.RefreshQuota(ctx); err != nil || got.Outcome() != values.QuotaCurrent {
		t.Fatalf("RefreshQuota after the change = %+v, %v", got, err)
	}
}
