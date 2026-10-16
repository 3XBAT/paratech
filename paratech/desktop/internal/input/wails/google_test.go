package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/googlesignin"
)

// fakeGoogle is the use case behind the Google binding.
type fakeGoogle struct {
	status    googlesignin.Status
	err       error // the result of every action
	cancelled bool

	gotID, gotSecret string
}

func (f *fakeGoogle) Status(context.Context) googlesignin.Status { return f.status }

func (f *fakeGoogle) SetClient(_ context.Context, id, secret string) (googlesignin.Status, error) {
	f.gotID, f.gotSecret = id, secret
	return f.status, f.err
}

func (f *fakeGoogle) ClearClient(context.Context) (googlesignin.Status, error) {
	return f.status, f.err
}
func (f *fakeGoogle) SignIn(context.Context) error  { return f.err }
func (f *fakeGoogle) CancelSignIn() bool            { return f.cancelled }
func (f *fakeGoogle) SignOut(context.Context) error { return f.err }

// The page calls these names (frontend/src/bindings/google.ts, to be written with the page).
func TestGoogleServiceMethodSet(t *testing.T) {
	typ := reflect.TypeFor[*GoogleService]()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	want := []string{"ClearGoogleClient", "GoogleCancelSignIn", "GoogleSignIn", "GoogleSignOut", "GoogleStatus", "SetGoogleClient"}
	if !slices.Equal(got, want) {
		t.Fatalf("GoogleService methods %v, want %v: every exported method is callable from the page", got, want)
	}
}

func TestGoogleStateJSON(t *testing.T) {
	s := NewGoogleService(&fakeGoogle{status: googlesignin.Status{
		Client: googlesignin.SecretSet, SignedIn: googlesignin.SecretSet, SigningIn: true,
	}}, discard())
	st, err := s.GoogleStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(GoogleResult{OK: true, Message: "m", State: st})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"ok":true,"message":"m","state":{"client":"set","account":"signedIn","signingIn":true}}`; string(raw) != want {
		t.Fatalf("JSON %s, want %s", raw, want)
	}

	for status, want := range map[googlesignin.SecretStatus][2]string{
		googlesignin.SecretSet:         {"set", "signedIn"},
		googlesignin.SecretUnset:       {"unset", "signedOut"},
		googlesignin.SecretUnsupported: {"unsupported", "unsupported"},
		googlesignin.SecretUnavailable: {"unavailable", "unavailable"},
	} {
		got := googleState(googlesignin.Status{Client: status, SignedIn: status})
		if got.Client != want[0] || got.Account != want[1] {
			t.Errorf("status %d: %+v, want %v", status, got, want)
		}
	}
}

// Every result of the use case has its own fixed text; anything else is a generic text, never an error to the page.
func TestGoogleAnswers(t *testing.T) {
	ctx := context.Background()
	for _, m := range googleMessages {
		t.Run(m.err.Error(), func(t *testing.T) {
			f := &fakeGoogle{err: fmt.Errorf("case: %w", m.err)}
			s := NewGoogleService(f, discard())
			for name, call := range map[string]func() (GoogleResult, error){
				"set":      func() (GoogleResult, error) { return s.SetGoogleClient(ctx, "id", "secret") },
				"clear":    func() (GoogleResult, error) { return s.ClearGoogleClient(ctx) },
				"sign in":  func() (GoogleResult, error) { return s.GoogleSignIn(ctx) },
				"sign out": func() (GoogleResult, error) { return s.GoogleSignOut(ctx) },
			} {
				got, err := call()
				if err != nil || got.OK || got.Message != m.msg {
					t.Fatalf("%s = %+v, %v; want %q", name, got, err, m.msg)
				}
			}
		})
	}

	s := NewGoogleService(&fakeGoogle{err: errors.New("bridge: loopback refused")}, discard())
	if got, err := s.GoogleSignIn(ctx); err != nil || got.OK || got.Message != msgGoogleFailed {
		t.Fatalf("unclassified = %+v, %v", got, err)
	}

	ok := NewGoogleService(&fakeGoogle{status: googlesignin.Status{SignedIn: googlesignin.SecretSet}}, discard())
	for want, call := range map[string]func() (GoogleResult, error){
		msgGoogleClientStored:  func() (GoogleResult, error) { return ok.SetGoogleClient(ctx, "id", "secret") },
		msgGoogleClientRemoved: func() (GoogleResult, error) { return ok.ClearGoogleClient(ctx) },
		msgGoogleSignedIn:      func() (GoogleResult, error) { return ok.GoogleSignIn(ctx) },
		msgGoogleSignedOut:     func() (GoogleResult, error) { return ok.GoogleSignOut(ctx) },
		msgGoogleNotSigningIn:  func() (GoogleResult, error) { return ok.GoogleCancelSignIn(ctx) },
	} {
		got, err := call()
		if err != nil || !got.OK || got.Message != want || got.State.Account != accountSignedIn {
			t.Fatalf("%+v, %v; want %q", got, err, want)
		}
	}
	running := NewGoogleService(&fakeGoogle{cancelled: true}, discard())
	if got, _ := running.GoogleCancelSignIn(ctx); !got.OK || got.Message != msgGoogleCancelled {
		t.Fatalf("GoogleCancelSignIn = %+v", got)
	}
}

// The client secret reaches the use case, but no answer and no log line.
func TestGoogleClientSecretStaysSecret(t *testing.T) {
	const secret = "GOCSPX-binding-marker-7f3a"
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, err := range []error{nil, googlesignin.ErrInvalidClientSecret, errors.New("bridge: unclassified")} {
		f := &fakeGoogle{err: err}
		got, _ := NewGoogleService(f, log).SetGoogleClient(context.Background(), " 123-fake.apps.googleusercontent.com ", secret)
		if f.gotSecret != secret || f.gotID != " 123-fake.apps.googleusercontent.com " {
			t.Fatal("the binding changed the input")
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the answer contains the secret: %s", raw)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("the log contains the secret")
	}
}

// fakeWindow counts the times the main window was brought forward.
type fakeWindow struct {
	shown int
	err   error
}

func (f *fakeWindow) ShowMain() error { f.shown++; return f.err }

// A successful sign-in brings the main window forward from behind the browser; a failed one leaves it alone, and a
// window that cannot be shown does not change the answer.
func TestGoogleSignInShowsTheMainWindow(t *testing.T) {
	tests := []struct {
		name      string
		signInErr error
		windowErr error
		wantShown int
		wantOK    bool
		wantLog   string
	}{
		{name: "signed in", wantShown: 1, wantOK: true},
		{name: "window not shown", windowErr: ErrNotRunning, wantShown: 1, wantOK: true, wantLog: "the main window was not brought forward"},
		{name: "denied", signInErr: googlesignin.ErrAccessDenied},
		{name: "timeout", signInErr: googlesignin.ErrSignInTimeout},
		{name: "cancelled", signInErr: googlesignin.ErrSignInCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			s := NewGoogleService(&fakeGoogle{err: tt.signInErr}, slog.New(slog.NewTextHandler(&logs, nil)))
			w := &fakeWindow{err: tt.windowErr}
			s.window = w
			got, err := s.GoogleSignIn(context.Background())
			if err != nil || got.OK != tt.wantOK {
				t.Fatalf("GoogleSignIn = %+v, %v", got, err)
			}
			if w.shown != tt.wantShown {
				t.Fatalf("main window shown %d times, want %d", w.shown, tt.wantShown)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Fatalf("logs %q, want %q", logs.String(), tt.wantLog)
			}
		})
	}
}

// The Google binding of OpenServices brings the shell's main window forward.
func TestOpenServicesBindsGoogleToTheMainWindow(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.OpenServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeUI{}
	s.setUI(f)
	if got, _ := b.Google.GoogleSignIn(t.Context()); !got.OK {
		t.Fatalf("GoogleSignIn = %+v", got)
	}
	if !slices.Equal(f.calls, []string{"showMain"}) {
		t.Fatalf("forwarded calls = %v, want [showMain]", f.calls)
	}
}
