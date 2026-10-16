package googlesignin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const (
	testClientID     = "123-fake.apps.googleusercontent.com"
	testClientSecret = "GOCSPX-fake-client-secret"
)

// fakeAuth is the Authorizer: signIn and signOut run as given; nil ones succeed.
type fakeAuth struct {
	signIn   func(ctx context.Context) error
	signOut  func(ctx context.Context) error
	signIns  atomic.Int32
	signOuts atomic.Int32
}

func (f *fakeAuth) SignIn(ctx context.Context) error {
	f.signIns.Add(1)
	if f.signIn == nil {
		return nil
	}
	return f.signIn(ctx)
}

func (f *fakeAuth) SignOut(ctx context.Context) error {
	f.signOuts.Add(1)
	if f.signOut == nil {
		return nil
	}
	return f.signOut(ctx)
}

func newService(t *testing.T, auth *fakeAuth) (*Service, *testenv.MemCredentials) {
	t.Helper()
	store := testenv.NewMemCredentials()
	s, err := New(Deps{Secrets: store, Auth: auth}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	return s, store
}

func mustSet(t *testing.T, store *testenv.MemCredentials, name, value string) {
	t.Helper()
	if err := store.Set(context.Background(), name, value); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, store *testenv.MemCredentials, name string) string {
	t.Helper()
	v, err := store.Get(context.Background(), name)
	if errors.Is(err, shared.ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewChecksThePorts(t *testing.T) {
	if _, err := New(Deps{Auth: &fakeAuth{}}, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without a store = %v", err)
	}
	if _, err := New(Deps{Secrets: testenv.NewMemCredentials()}, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without an authorizer = %v", err)
	}
}

func TestSetClient(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		id, secret string
		signedIn   bool
		want       error
	}{
		{name: "stored", id: " " + testClientID + "\r\n", secret: testClientSecret + "\n"},
		{name: "invalid id", id: "123-fake", secret: testClientSecret, want: ErrInvalidClientID},
		{name: "secret in the id field", id: testClientSecret, secret: testClientSecret, want: ErrInvalidClientID},
		{name: "invalid secret", id: testClientID, secret: "with space", want: ErrInvalidClientSecret},
		{name: "empty secret", id: testClientID, secret: "", want: ErrInvalidClientSecret},
		{name: "signed in", id: testClientID, secret: testClientSecret, signedIn: true, want: ErrSignedIn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, store := newService(t, &fakeAuth{})
			if tt.signedIn {
				mustSet(t, store, DefaultRefreshTokenName, "fake-refresh")
			}
			st, err := s.SetClient(ctx, tt.id, tt.secret)
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("SetClient = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if get(t, store, DefaultClientIDName) != "" || get(t, store, DefaultClientSecretName) != "" || st.Client != SecretUnset {
					t.Fatalf("a refused client was stored (status %v)", st.Client)
				}
				if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
					t.Fatal("the error quotes the secret")
				}
				return
			}
			if get(t, store, DefaultClientIDName) != testClientID || get(t, store, DefaultClientSecretName) != testClientSecret ||
				st.Client != SecretSet || st.SignedIn != SecretUnset {
				t.Fatalf("SetClient stored the wrong values (status %+v)", st)
			}
		})
	}
}

func TestClearClient(t *testing.T) {
	ctx := context.Background()
	s, store := newService(t, &fakeAuth{})
	if _, err := s.SetClient(ctx, testClientID, testClientSecret); err != nil {
		t.Fatal(err)
	}

	mustSet(t, store, DefaultRefreshTokenName, "fake-refresh")
	if _, err := s.ClearClient(ctx); !errors.Is(err, ErrSignedIn) || get(t, store, DefaultClientIDName) == "" {
		t.Fatalf("ClearClient while signed in = %v", err)
	}
	if err := store.Delete(ctx, DefaultRefreshTokenName); err != nil {
		t.Fatal(err)
	}
	st, err := s.ClearClient(ctx)
	if err != nil || st.Client != SecretUnset || store.Len() != 0 {
		t.Fatalf("ClearClient = %+v, %v (store %d)", st, err, store.Len())
	}
	if _, err := s.ClearClient(ctx); err != nil {
		t.Fatalf("ClearClient of nothing = %v", err)
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		entries []string
		fail    error
		want    Status
	}{
		{name: "nothing", want: Status{Client: SecretUnset, SignedIn: SecretUnset}},
		{name: "id only", entries: []string{DefaultClientIDName}, want: Status{Client: SecretUnset, SignedIn: SecretUnset}},
		{name: "client", entries: []string{DefaultClientIDName, DefaultClientSecretName},
			want: Status{Client: SecretSet, SignedIn: SecretUnset}},
		{name: "signed in", entries: []string{DefaultClientIDName, DefaultClientSecretName, DefaultRefreshTokenName},
			want: Status{Client: SecretSet, SignedIn: SecretSet}},
		{name: "unsupported", fail: fmt.Errorf("wincred: %w", errors.ErrUnsupported),
			want: Status{Client: SecretUnsupported, SignedIn: SecretUnsupported}},
		{name: "store down", fail: shared.ErrStorage, want: Status{Client: SecretUnavailable, SignedIn: SecretUnavailable}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, store := newService(t, &fakeAuth{})
			for _, e := range tt.entries {
				mustSet(t, store, e, "v")
			}
			store.Fail(tt.fail)
			if got := s.Status(ctx); got != tt.want {
				t.Fatalf("Status = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSetClientWithoutStore(t *testing.T) {
	s, store := newService(t, &fakeAuth{})
	store.Fail(fmt.Errorf("wincred: %w", errors.ErrUnsupported))
	st, err := s.SetClient(context.Background(), testClientID, testClientSecret)
	if !errors.Is(err, ErrSecretsUnsupported) || st.Client != SecretUnsupported {
		t.Fatalf("SetClient = %+v, %v", st, err)
	}
	store.Fail(shared.ErrStorage)
	if _, err := s.SetClient(context.Background(), testClientID, testClientSecret); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("SetClient with the store down = %v", err)
	}
}

// configured is a service with the client stored.
func configured(t *testing.T, auth *fakeAuth) (*Service, *testenv.MemCredentials) {
	t.Helper()
	s, store := newService(t, auth)
	if _, err := s.SetClient(context.Background(), testClientID, testClientSecret); err != nil {
		t.Fatal(err)
	}
	return s, store
}

func TestSignInNeedsTheClient(t *testing.T) {
	auth := &fakeAuth{}
	s, store := newService(t, auth)
	if err := s.SignIn(context.Background()); !errors.Is(err, ErrClientNotConfigured) {
		t.Fatalf("SignIn = %v, want ErrClientNotConfigured", err)
	}
	store.Fail(fmt.Errorf("wincred: %w", errors.ErrUnsupported))
	if err := s.SignIn(context.Background()); !errors.Is(err, ErrSecretsUnsupported) {
		t.Fatalf("SignIn without a store = %v", err)
	}
	if auth.signIns.Load() != 0 {
		t.Fatal("the authorizer was called without a client")
	}
}

func TestSignInResults(t *testing.T) {
	unclassified := errors.New("bridge: something else")
	tests := []struct {
		name string
		from error
		want error
	}{
		{name: "success"},
		{name: "cancelled ctx", from: fmt.Errorf("adapter: %w", context.Canceled), want: ErrSignInCancelled},
		{name: "caller deadline", from: fmt.Errorf("adapter: %w", context.DeadlineExceeded), want: ErrSignInCancelled},
		{name: "unclassified", from: unclassified, want: unclassified},
	}
	for _, r := range results {
		tests = append(tests, struct {
			name string
			from error
			want error
		}{name: r.Error(), from: fmt.Errorf("bridge: %w", r), want: r})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := configured(t, &fakeAuth{signIn: func(context.Context) error { return tt.from }})
			err := s.SignIn(context.Background())
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("SignIn = %v, want %v", err, tt.want)
			}
			if s.Status(context.Background()).SigningIn {
				t.Fatal("still signing in after the end")
			}
		})
	}
}

// At most one sign-in runs; while it waits, the client cannot change and nobody can sign out; CancelSignIn ends it.
func TestOneSignInAtATime(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	auth := &fakeAuth{signIn: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return fmt.Errorf("adapter: %w", ctx.Err())
	}}
	s, _ := configured(t, auth)
	done := make(chan error, 1)
	go func() { done <- s.SignIn(ctx) }()
	<-started

	if !s.Status(ctx).SigningIn {
		t.Fatal("Status does not show the running sign-in")
	}
	if err := s.SignIn(ctx); !errors.Is(err, ErrSignInInProgress) {
		t.Fatalf("second SignIn = %v, want ErrSignInInProgress", err)
	}
	if _, err := s.SetClient(ctx, testClientID, testClientSecret); !errors.Is(err, ErrSignInInProgress) {
		t.Fatalf("SetClient during the sign-in = %v", err)
	}
	if _, err := s.ClearClient(ctx); !errors.Is(err, ErrSignInInProgress) {
		t.Fatalf("ClearClient during the sign-in = %v", err)
	}
	if err := s.SignOut(ctx); !errors.Is(err, ErrSignInInProgress) || auth.signOuts.Load() != 0 {
		t.Fatalf("SignOut during the sign-in = %v", err)
	}

	if !s.CancelSignIn() {
		t.Fatal("CancelSignIn found nothing to cancel")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrSignInCancelled) {
			t.Fatalf("SignIn = %v, want ErrSignInCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled sign-in did not return")
	}
	if s.CancelSignIn() || s.Status(ctx).SigningIn || auth.signIns.Load() != 1 {
		t.Fatal("the sign-in is still marked as running")
	}
}

func TestSignOut(t *testing.T) {
	tests := []struct {
		name string
		from error
		want error
	}{
		{name: "revoked"},
		{name: "revoke not confirmed", from: fmt.Errorf("bridge: %w", ErrRevokeFailed), want: ErrRevokeFailed},
		{name: "store down", from: fmt.Errorf("bridge: %w", ErrSecretsUnavailable), want: ErrSecretsUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &fakeAuth{signOut: func(context.Context) error { return tt.from }}
			s, _ := configured(t, auth)
			err := s.SignOut(context.Background())
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) || auth.signOuts.Load() != 1 {
				t.Fatalf("SignOut = %v, want %v", err, tt.want)
			}
			if err := s.SignIn(context.Background()); err != nil {
				t.Fatalf("SignIn after SignOut = %v: the service stayed busy", err)
			}
		})
	}
}
