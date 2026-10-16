package connections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeRepo keeps the stored state and follows the compare-and-swap contract of the sqlite adapter.
type fakeRepo struct {
	stored    settings.State // Version 0: nothing stored
	conflicts int            // the next saves fail with a version conflict
	loadErr   error
	saves     int
}

func (r *fakeRepo) Load(context.Context) (*settings.Settings, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	if r.stored.Version == 0 {
		return settings.New(), nil
	}
	return settings.Restore(r.stored)
}

func (r *fakeRepo) Save(_ context.Context, s *settings.Settings) error {
	if r.conflicts > 0 {
		r.conflicts--
		if r.stored.Version == 0 {
			r.stored = settings.State{Version: 1, SMTPSecurity: settings.DefaultMailSecurity} // another writer won
		} else {
			r.stored.Version++
		}
		return shared.ErrVersionConflict
	}
	if s.Version() != r.stored.Version {
		return shared.ErrVersionConflict
	}
	r.saves++
	r.stored = settings.State{
		Version:                 s.Version() + 1,
		RecordingPromptsEnabled: s.RecordingPromptsEnabled(),
		ProcessOnlyOnACPower:    s.ProcessOnlyOnACPower(),
		HomeworkRecipient:       s.HomeworkRecipient(),
		SMTPHost:                s.SMTPHost(),
		SMTPPort:                s.SMTPPort(),
		SMTPSecurity:            s.SMTPSecurity(),
		SMTPLogin:               s.SMTPLogin(),
		HubURL:                  s.HubURL(),
		UpdatedAt:               s.UpdatedAt(),
	}
	s.Persisted(r.stored.Version)
	return nil
}

// fakeChecker answers the mail checks; block, when set, holds every check until it is closed.
type fakeChecker struct {
	mu       sync.Mutex
	err      error
	block    chan struct{}
	started  chan struct{}
	deadline bool // the context of the last check had a deadline
	waitDone bool // every check waits for its context to end
	calls    int
}

func (c *fakeChecker) CheckMail(ctx context.Context) error {
	c.mu.Lock()
	c.calls++
	_, c.deadline = ctx.Deadline()
	block, started, err, waitDone := c.block, c.started, c.err, c.waitDone
	c.mu.Unlock()
	if waitDone {
		<-ctx.Done()
		return ctx.Err()
	}
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		<-block
	}
	return err
}

// unsupportedStore is the credential store off Windows: every call fails like wincred.ErrUnsupported.
type unsupportedStore struct{}

var errNoCredman = fmt.Errorf("wincred: credential manager is available only on Windows: %w", errors.ErrUnsupported)

func (unsupportedStore) Set(context.Context, string, string) error    { return errNoCredman }
func (unsupportedStore) Exists(context.Context, string) (bool, error) { return false, errNoCredman }
func (unsupportedStore) Delete(context.Context, string) error         { return errNoCredman }

var start = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

const testPassword = "conn-test-password-7f3a"

type fixture struct {
	repo    *fakeRepo
	store   *testenv.MemCredentials
	checker *fakeChecker
	hub     *testenv.MemCredentials
	quota   *fakeQuota
	clock   *testenv.FakeClock
	svc     *Service
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	f := &fixture{
		repo: &fakeRepo{}, store: testenv.NewMemCredentials(), checker: &fakeChecker{}, hub: testenv.NewMemCredentials(),
		quota: &fakeQuota{}, clock: testenv.NewFakeClock(start),
	}
	svc, err := New(Deps{
		Settings: f.repo, Clock: f.clock, MailSecret: f.store, MailCheck: f.checker, HubSecret: f.hub, Quota: f.quota,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func TestNewValidates(t *testing.T) {
	valid := func() Deps {
		return Deps{
			Settings: &fakeRepo{}, Clock: testenv.NewFakeClock(start), MailSecret: testenv.NewMemCredentials(),
			MailCheck: &fakeChecker{}, HubSecret: testenv.NewMemCredentials(), Quota: &fakeQuota{},
		}
	}
	tests := []struct {
		name string
		edit func(*Deps, *Config)
		ok   bool
	}{
		{"valid", func(*Deps, *Config) {}, true},
		{"no settings", func(d *Deps, _ *Config) { d.Settings = nil }, false},
		{"no clock", func(d *Deps, _ *Config) { d.Clock = nil }, false},
		{"no mail secret store", func(d *Deps, _ *Config) { d.MailSecret = nil }, false},
		{"no mail checker", func(d *Deps, _ *Config) { d.MailCheck = nil }, false},
		{"negative check timeout", func(_ *Deps, c *Config) { c.CheckTimeout = -time.Second }, false},
		{"no hub secret store", func(d *Deps, _ *Config) { d.HubSecret = nil }, false},
		{"no quota source", func(d *Deps, _ *Config) { d.Quota = nil }, false},
		{"negative refresh timeout", func(_ *Deps, c *Config) { c.RefreshTimeout = -time.Second }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, cfg := valid(), Config{}
			tt.edit(&d, &cfg)
			_, err := New(d, cfg)
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidConfig)) {
				t.Fatalf("New = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func TestGetReturnsDefaults(t *testing.T) {
	got, err := newFixture(t, Config{}).svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := View{SMTPSecurity: settings.DefaultMailSecurity, MailPassword: SecretUnset, HubKey: SecretUnset}
	if got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}
}

func TestSaveMailStoresEveryField(t *testing.T) {
	f := newFixture(t, Config{})
	got, err := f.svc.SaveMail(context.Background(), MailInput{
		Recipient: " teacher@school.example ", Host: "smtp.example.org", Port: 465, Security: settings.MailSecurityTLS,
		Login: "student@example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := View{
		HomeworkRecipient: "teacher@school.example", SMTPHost: "smtp.example.org", SMTPPort: 465,
		SMTPSecurity: settings.MailSecurityTLS, SMTPLogin: "student@example.org", MailConfigured: true, MailPassword: SecretUnset,
		HubKey: SecretUnset,
	}
	if got != want {
		t.Fatalf("SaveMail = %+v, want %+v", got, want)
	}
	if f.repo.saves != 1 || f.repo.stored.SMTPHost != "smtp.example.org" || f.repo.stored.UpdatedAt != start {
		t.Fatalf("stored %+v after %d saves", f.repo.stored, f.repo.saves)
	}

	// A login that is not an address is kept, but mail is not configured: it is also the sender.
	got, err = f.svc.SaveMail(context.Background(), MailInput{
		Recipient: "teacher@school.example", Host: "smtp.example.org", Security: settings.MailSecurityStartTLS, Login: "student",
	})
	if err != nil || got.SMTPLogin != "student" || got.MailConfigured || got.SMTPPort != 0 {
		t.Fatalf("SaveMail with a plain login = %+v, %v", got, err)
	}

	// Empty values remove the account.
	got, err = f.svc.SaveMail(context.Background(), MailInput{Security: settings.MailSecurityStartTLS})
	if err != nil || got != (View{SMTPSecurity: settings.MailSecurityStartTLS, MailPassword: SecretUnset, HubKey: SecretUnset}) {
		t.Fatalf("SaveMail with empty values = %+v, %v", got, err)
	}
}

// Every refused field is reported at once and nothing is saved, not even the valid fields.
func TestSaveMailReportsEveryInvalidField(t *testing.T) {
	valid := MailInput{
		Recipient: "teacher@school.example", Host: "smtp.example.org", Port: 587, Security: settings.MailSecurityStartTLS,
		Login: "student@example.org",
	}
	const marker = "VALUEMARKER"
	tests := []struct {
		name string
		edit func(*MailInput)
		want []Field
	}{
		{"recipient with a display name", func(in *MailInput) { in.Recipient = marker + " <t@school.example>" }, []Field{FieldRecipient}},
		{"host with a scheme", func(in *MailInput) { in.Host = "smtp://" + marker }, []Field{FieldSMTPHost}},
		{"host with a port", func(in *MailInput) { in.Host = "smtp.example.org:587" }, []Field{FieldSMTPHost}},
		{"port too large", func(in *MailInput) { in.Port = 65536 }, []Field{FieldSMTPPort}},
		{"negative port", func(in *MailInput) { in.Port = -1 }, []Field{FieldSMTPPort}},
		{"unknown security", func(in *MailInput) { in.Security = marker }, []Field{FieldSMTPSecurity}},
		{"empty security", func(in *MailInput) { in.Security = "" }, []Field{FieldSMTPSecurity}},
		{"login with a control character", func(in *MailInput) { in.Login = marker + "\x00" }, []Field{FieldSMTPLogin}},
		{"everything", func(in *MailInput) {
			*in = MailInput{Recipient: marker, Host: marker + "/", Port: 70000, Security: marker, Login: strings.Repeat("x", 255)}
		}, []Field{FieldRecipient, FieldSMTPHost, FieldSMTPPort, FieldSMTPSecurity, FieldSMTPLogin}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{})
			in := valid
			tt.edit(&in)
			_, err := f.svc.SaveMail(context.Background(), in)

			var fields *InvalidFieldsError
			if !errors.As(err, &fields) || !errors.Is(err, settings.ErrInvalidSettings) {
				t.Fatalf("SaveMail = %v, want an *InvalidFieldsError", err)
			}
			if !slices.Equal(fields.Fields, tt.want) {
				t.Fatalf("fields %v, want %v", fields.Fields, tt.want)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("error %q quotes a value", err)
			}
			if f.repo.saves != 0 {
				t.Fatal("an invalid input was saved")
			}
		})
	}
}

func TestSaveHubURL(t *testing.T) {
	tests := []struct {
		name, url, want string
		invalid         bool
	}{
		{name: "https", url: "https://hub.example.org/api", want: "https://hub.example.org/api"},
		{name: "http to loopback", url: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "empty removes it", url: " ", want: ""},
		{name: "plain http", url: "http://hub.example.org", invalid: true},
		{name: "credentials", url: "https://user:pass@hub.example.org", invalid: true},
		{name: "query", url: "https://hub.example.org/?key=1", invalid: true},
		{name: "fragment", url: "https://hub.example.org/#x", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{})
			f.repo.stored = settings.State{Version: 1, HubURL: "https://old.example.org", SMTPSecurity: settings.DefaultMailSecurity}
			got, err := f.svc.SaveHubURL(context.Background(), tt.url)
			if tt.invalid {
				var fields *InvalidFieldsError
				if !errors.As(err, &fields) || !slices.Equal(fields.Fields, []Field{FieldHubURL}) {
					t.Fatalf("SaveHubURL = %v, want the hubUrl field", err)
				}
				if f.repo.stored.HubURL != "https://old.example.org" || strings.Contains(err.Error(), "pass") {
					t.Fatalf("stored %q, error %q", f.repo.stored.HubURL, err)
				}
				return
			}
			if err != nil || got.HubURL != tt.want || f.repo.stored.HubURL != tt.want {
				t.Fatalf("SaveHubURL = %+v, %v; stored %q", got, err, f.repo.stored.HubURL)
			}
		})
	}
}

// A save that meets a newer version reloads and applies the change again; it gives up after a bound.
func TestSaveRetriesOnVersionConflict(t *testing.T) {
	f := newFixture(t, Config{})
	f.repo.conflicts = conflictAttempts - 1
	got, err := f.svc.SaveHubURL(context.Background(), "https://hub.example.org")
	if err != nil || got.HubURL != "https://hub.example.org" || f.repo.saves != 1 {
		t.Fatalf("SaveHubURL after conflicts = %+v, %v (saves %d)", got, err, f.repo.saves)
	}

	f.repo.conflicts = conflictAttempts
	if _, err := f.svc.SaveHubURL(context.Background(), "https://other.example.org"); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("SaveHubURL with endless conflicts = %v, want ErrVersionConflict", err)
	}
}

func TestLoadFailure(t *testing.T) {
	f := newFixture(t, Config{})
	boom := errors.New("database is locked")
	f.repo.loadErr = boom
	if _, err := f.svc.Get(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Get = %v", err)
	}
	if _, err := f.svc.SaveMail(context.Background(), MailInput{Security: settings.MailSecurityTLS}); !errors.Is(err, boom) {
		t.Errorf("SaveMail = %v", err)
	}
	if _, err := f.svc.SaveHubURL(context.Background(), ""); !errors.Is(err, boom) {
		t.Errorf("SaveHubURL = %v", err)
	}
}

// The password goes only into the store, under the entry the smtp adapter reads; the page learns only its status.
func TestMailPasswordLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{})

	status, err := f.svc.SetMailPassword(ctx, testPassword)
	if err != nil || status != SecretSet {
		t.Fatalf("SetMailPassword = %v, %v", status, err)
	}
	if got, err := f.store.Get(ctx, DefaultMailSecretName); err != nil || got != testPassword || f.store.Len() != 1 {
		t.Fatalf("store holds %d secrets, %q under %q: %v", f.store.Len(), got, DefaultMailSecretName, err)
	}
	if v, err := f.svc.Get(ctx); err != nil || v.MailPassword != SecretSet {
		t.Fatalf("Get = %+v, %v", v, err)
	}
	if f.repo.saves != 0 {
		t.Fatal("setting the password saved the settings")
	}

	// A refused value changes nothing and reports what is stored.
	for _, bad := range []string{"", strings.Repeat("p", 2561)} {
		status, err = f.svc.SetMailPassword(ctx, bad)
		if !errors.Is(err, ErrInvalidSecret) || !errors.Is(err, shared.ErrInvalidSecret) || status != SecretSet {
			t.Fatalf("SetMailPassword(%d bytes) = %v, %v", len(bad), status, err)
		}
	}
	if got, _ := f.store.Get(ctx, DefaultMailSecretName); got != testPassword {
		t.Fatal("a refused value replaced the password")
	}

	status, err = f.svc.RemoveMailPassword(ctx)
	if err != nil || status != SecretUnset || f.store.Len() != 0 {
		t.Fatalf("RemoveMailPassword = %v, %v (store %d)", status, err, f.store.Len())
	}
	if status, err = f.svc.RemoveMailPassword(ctx); err != nil || status != SecretUnset {
		t.Fatalf("RemoveMailPassword again = %v, %v", status, err)
	}
	if status, err = f.svc.SetMailPassword(ctx, ""); !errors.Is(err, ErrInvalidSecret) || status != SecretUnset {
		t.Fatalf("SetMailPassword(empty) without a password = %v, %v", status, err)
	}
}

func TestMailSecretNameFromConfig(t *testing.T) {
	f := newFixture(t, Config{MailSecretName: "other-entry"})
	if _, err := f.svc.SetMailPassword(context.Background(), testPassword); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.store.Exists(context.Background(), "other-entry"); !ok {
		t.Fatal("the password is not under the configured entry")
	}
}

// Off Windows there is no credential store: the section still works and says so; a failing store is "unavailable".
func TestCredentialStoreFailures(t *testing.T) {
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
				Settings: &fakeRepo{}, Clock: testenv.NewFakeClock(start), MailSecret: tt.store, MailCheck: &fakeChecker{},
				HubSecret: testenv.NewMemCredentials(), Quota: &fakeQuota{},
			}, Config{})
			if err != nil {
				t.Fatal(err)
			}
			v, err := svc.Get(ctx)
			if err != nil || v.MailPassword != tt.status {
				t.Fatalf("Get = %+v, %v; want status %v", v, err, tt.status)
			}
			status, err := svc.SetMailPassword(ctx, testPassword)
			if !errors.Is(err, tt.want) || status != tt.status || strings.Contains(err.Error(), testPassword) {
				t.Fatalf("SetMailPassword = %v, %v", status, err)
			}
			status, err = svc.RemoveMailPassword(ctx)
			if !errors.Is(err, tt.want) || status != tt.status {
				t.Fatalf("RemoveMailPassword = %v, %v", status, err)
			}
			if _, err := svc.SaveHubURL(ctx, "https://hub.example.org"); err != nil {
				t.Fatalf("SaveHubURL with a broken store = %v", err)
			}
		})
	}
}

func TestCheckMailResults(t *testing.T) {
	for _, known := range checkResults {
		t.Run(known.Error(), func(t *testing.T) {
			f := newFixture(t, Config{})
			f.checker.err = fmt.Errorf("bridge: %w", known)
			if err := f.svc.CheckMail(context.Background()); !errors.Is(err, known) {
				t.Fatalf("CheckMail = %v, want %v", err, known)
			}
		})
	}

	f := newFixture(t, Config{})
	if err := f.svc.CheckMail(context.Background()); err != nil {
		t.Fatalf("CheckMail = %v, want nil", err)
	}
	other := errors.New("settings cannot be read")
	f.checker.err = other
	err := f.svc.CheckMail(context.Background())
	if !errors.Is(err, other) || slices.ContainsFunc(checkResults, func(k error) bool { return errors.Is(err, k) }) {
		t.Fatalf("CheckMail = %v, want the unclassified cause", err)
	}
}

// A check that hangs ends at the configured bound; by default it has one too.
func TestCheckMailIsBounded(t *testing.T) {
	f := newFixture(t, Config{CheckTimeout: 50 * time.Millisecond})
	f.checker.waitDone = true
	if err := f.svc.CheckMail(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CheckMail = %v, want the deadline", err)
	}

	f = newFixture(t, Config{})
	if err := f.svc.CheckMail(context.Background()); err != nil || !f.checker.deadline {
		t.Fatalf("CheckMail = %v; deadline set %v", err, f.checker.deadline)
	}
}

// One check at a time: a second click while a check runs is answered at once.
func TestCheckMailOneAtATime(t *testing.T) {
	f := newFixture(t, Config{})
	f.checker.block, f.checker.started = make(chan struct{}), make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- f.svc.CheckMail(context.Background()) }()
	<-f.checker.started

	if err := f.svc.CheckMail(context.Background()); !errors.Is(err, ErrCheckBusy) {
		t.Fatalf("second CheckMail = %v, want ErrCheckBusy", err)
	}
	close(f.checker.block)
	if err := <-done; err != nil {
		t.Fatalf("first CheckMail = %v", err)
	}

	f.checker.mu.Lock()
	f.checker.started = nil
	f.checker.mu.Unlock()
	if err := f.svc.CheckMail(context.Background()); err != nil {
		t.Fatalf("CheckMail after the first ended = %v", err)
	}
	if f.checker.calls != 2 {
		t.Fatalf("checker called %d times, want 2", f.checker.calls)
	}
}
