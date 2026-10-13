package wincred

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// fakeBackend stores copies and remembers the last blob it was given, to check that Set wipes it.
type fakeBackend struct {
	data     map[string][]byte
	lastBlob []byte
	failWith error
}

func newFakeBackend() *fakeBackend { return &fakeBackend{data: map[string][]byte{}} }

func (f *fakeBackend) write(target string, blob []byte) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.lastBlob = blob
	f.data[target] = append([]byte(nil), blob...)
	return nil
}

func (f *fakeBackend) read(target string) ([]byte, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	v, ok := f.data[target]
	if !ok {
		return nil, fmt.Errorf("%w: credential", shared.ErrNotFound)
	}
	return append([]byte(nil), v...), nil
}

func (f *fakeBackend) remove(target string) error {
	if f.failWith != nil {
		return f.failWith
	}
	if _, ok := f.data[target]; !ok {
		return fmt.Errorf("%w: credential", shared.ErrNotFound)
	}
	delete(f.data, target)
	return nil
}

// requireCredmanEnv=1 turns a session without a credential set into a failure instead of a skip.
const requireCredmanEnv = "PARATECH_REQUIRE_CREDMAN"

type verdict int

const (
	verdictRun verdict = iota
	verdictSkip
	verdictFail
)

// sessionVerdict decides what a real-API test does with the error of its first call: only ErrNoSession may skip.
func sessionVerdict(err error, getenv func(string) string) verdict {
	switch {
	case err == nil:
		return verdictRun
	case errors.Is(err, ErrNoSession) && getenv(requireCredmanEnv) != "1":
		return verdictSkip
	}
	return verdictFail
}

func TestSessionVerdict(t *testing.T) {
	noEnv := func(string) string { return "" }
	force := func(k string) string {
		if k == requireCredmanEnv {
			return "1"
		}
		return ""
	}
	wrapped := fmt.Errorf("credential manager read: %w", ErrNoSession)

	cases := []struct {
		desc   string
		err    error
		getenv func(string) string
		want   verdict
	}{
		{"no error", nil, noEnv, verdictRun},
		{"no error, forced", nil, force, verdictRun},
		{"no session", wrapped, noEnv, verdictSkip},
		{"no session, forced", wrapped, force, verdictFail},
		{"other storage error", fmt.Errorf("%w: x", shared.ErrStorage), noEnv, verdictFail},
		{"access denied", shared.ErrStoragePermission, noEnv, verdictFail},
		{"unsupported", ErrUnsupported, noEnv, verdictFail},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := sessionVerdict(tc.err, tc.getenv); got != tc.want {
				t.Fatalf("sessionVerdict = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrNoSessionIsStorage(t *testing.T) {
	if !errors.Is(ErrNoSession, shared.ErrStorage) {
		t.Fatal("ErrNoSession must wrap shared.ErrStorage")
	}
	if strings.Contains(ErrNoSession.Error(), TargetPrefix) {
		t.Fatalf("ErrNoSession names a target: %v", ErrNoSession)
	}
}

func newTestStore(t *testing.T, service string) (*Store, *fakeBackend) {
	t.Helper()
	fb := newFakeBackend()
	s, err := newStore(service, fb)
	if err != nil {
		t.Fatal(err)
	}
	return s, fb
}

func TestCheckName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"smtp-password", true},
		{"api_key.v1", true},
		{"0", true},
		{strings.Repeat("a", MaxNameLen), true},
		{"", false},
		{strings.Repeat("a", MaxNameLen+1), false},
		{"Upper", false},
		{".hidden", false},
		{"-dash", false},
		{"a/b", false},
		{`a\b`, false},
		{"../x", false},
		{"c:x", false},
		{"a*", false},
		{"a b", false},
		{"a\x00b", false},
		{"a\nb", false},
		{"a\tb", false},
		{"пароль", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.name), func(t *testing.T) {
			err := CheckName(tc.name)
			if tc.ok && err != nil {
				t.Fatalf("CheckName = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, shared.ErrInvalidID) {
				t.Fatalf("CheckName = %v, want ErrInvalidID", err)
			}
		})
	}
}

func TestCheckSecret(t *testing.T) {
	cases := []struct {
		desc   string
		secret string
		want   error
	}{
		{"one byte", "x", nil},
		{"at the limit", strings.Repeat("s", MaxSecretBytes), nil},
		{"unicode within the limit", strings.Repeat("я", MaxSecretBytes/2), nil},
		{"empty", "", ErrEmptySecret},
		{"one byte over", strings.Repeat("s", MaxSecretBytes+1), ErrSecretTooLarge},
		{"unicode over in bytes", strings.Repeat("я", MaxSecretBytes/2+1), ErrSecretTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := CheckSecret(tc.secret)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("CheckSecret = %v, want %v", err, tc.want)
			}
			if tc.want != nil && !errors.Is(err, shared.ErrInvalidSecret) {
				t.Fatalf("CheckSecret = %v, want it to wrap shared.ErrInvalidSecret", err)
			}
		})
	}
}

func TestNewValidatesService(t *testing.T) {
	for _, svc := range []string{"", "Mail", "mail/x", strings.Repeat("a", MaxServiceLen+1)} {
		if _, err := newStore(svc, newFakeBackend()); !errors.Is(err, ErrInvalidName) {
			t.Errorf("newStore(%q) = %v, want ErrInvalidName", svc, err)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, fb := newTestStore(t, "mail")

	if err := s.Set(ctx, "password", "first"); err != nil {
		t.Fatal(err)
	}
	if _, ok := fb.data["paratech/mail/password"]; !ok {
		t.Fatalf("target names = %v, want paratech/mail/password", fb.data)
	}
	if err := s.Set(ctx, "password", "second"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "password")
	if err != nil || got != "second" {
		t.Fatalf("Get = %q, %v; want the overwritten value", got, err)
	}
	if ok, err := s.Exists(ctx, "password"); err != nil || !ok {
		t.Fatalf("Exists = %v, %v; want true", ok, err)
	}

	if err := s.Delete(ctx, "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "password"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists(ctx, "password"); err != nil || ok {
		t.Fatalf("Exists after Delete = %v, %v; want false", ok, err)
	}
	if err := s.Delete(ctx, "password"); err != nil {
		t.Fatalf("Delete of a missing secret = %v, want nil", err)
	}
}

// Two services never see each other's secrets.
func TestStoreServicesAreSeparate(t *testing.T) {
	ctx := context.Background()
	fb := newFakeBackend()
	mail, _ := newStore("mail", fb)
	hub, _ := newStore("hub", fb)

	if err := mail.Set(ctx, "key", "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Get(ctx, "key"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("hub.Get = %v, want ErrNotFound", err)
	}
}

func TestSetWipesItsCopy(t *testing.T) {
	s, fb := newTestStore(t, "mail")
	if err := s.Set(context.Background(), "password", "hunter2"); err != nil {
		t.Fatal(err)
	}
	for _, b := range fb.lastBlob {
		if b != 0 {
			t.Fatalf("blob handed to the backend was not wiped: %v", fb.lastBlob)
		}
	}
}

func TestStoreRejectsBeforeBackend(t *testing.T) {
	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	s, fb := newTestStore(t, "mail")
	fb.failWith = errors.New("backend must not be called")

	cases := []struct {
		desc string
		call func() error
		want error
	}{
		{"set bad name", func() error { return s.Set(ctx, "a/b", "x") }, shared.ErrInvalidID},
		{"get bad name", func() error { _, err := s.Get(ctx, "A"); return err }, shared.ErrInvalidID},
		{"exists bad name", func() error { _, err := s.Exists(ctx, ""); return err }, shared.ErrInvalidID},
		{"delete bad name", func() error { return s.Delete(ctx, "a\x00") }, shared.ErrInvalidID},
		{"set empty", func() error { return s.Set(ctx, "k", "") }, ErrEmptySecret},
		{"set too large", func() error { return s.Set(ctx, "k", strings.Repeat("x", MaxSecretBytes+1)) }, ErrSecretTooLarge},
		{"set canceled", func() error { return s.Set(canceled, "k", "x") }, context.Canceled},
		{"get canceled", func() error { _, err := s.Get(canceled, "k"); return err }, context.Canceled},
		{"exists canceled", func() error { _, err := s.Exists(canceled, "k"); return err }, context.Canceled},
		{"delete canceled", func() error { return s.Delete(canceled, "k") }, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Backend failures other than "not found" pass through Exists and Delete.
func TestStoreBackendFailures(t *testing.T) {
	ctx := context.Background()
	s, fb := newTestStore(t, "mail")
	fb.failWith = fmt.Errorf("%w: credential manager read failed", shared.ErrStorage)

	if _, err := s.Exists(ctx, "k"); !errors.Is(err, shared.ErrStorage) {
		t.Errorf("Exists = %v, want ErrStorage", err)
	}
	if err := s.Delete(ctx, "k"); !errors.Is(err, shared.ErrStorage) {
		t.Errorf("Delete = %v, want ErrStorage", err)
	}
	if _, err := s.Get(ctx, "k"); !errors.Is(err, shared.ErrStorage) {
		t.Errorf("Get = %v, want ErrStorage", err)
	}
}

// Neither error texts nor the formatted store reveal a secret or a target name.
func TestNoSecretInOutput(t *testing.T) {
	const secret = "sk-very-secret-value"
	ctx := context.Background()
	s, _ := newTestStore(t, "mail")
	if err := s.Set(ctx, "password", secret); err != nil {
		t.Fatal(err)
	}

	errs := []error{
		s.Set(ctx, "password", secret+strings.Repeat("x", MaxSecretBytes)),
		s.Set(ctx, "bad/"+secret, "x"),
	}
	_, err := s.Get(ctx, "missing")
	errs = append(errs, err)
	for _, e := range errs {
		if e == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(e.Error(), secret) || strings.Contains(e.Error(), "paratech/") {
			t.Errorf("error text reveals a secret or target: %q", e)
		}
	}

	for _, out := range []string{fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s)} {
		if strings.Contains(out, secret) {
			t.Errorf("formatted store reveals the secret: %s", out)
		}
	}
}
