package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/googlesignin"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/schedule/values"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// useFakeGoogle points the core's Google endpoints to g.
func useFakeGoogle(a *App, g *testenv.FakeGoogleOAuth) {
	a.env.googleOAuth = []googleauth.Option{
		googleauth.WithEndpoints(g.AuthURL(), g.TokenURL(), g.RevokeURL()),
		googleauth.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}), googleauth.WithSignInTimeout(10 * time.Second),
	}
}

// Test step 2 of TASK-050 (AC-19, hard rule 4) through the page bindings of the core as the app builds it: the client
// goes into the google credential store, the sign-in exchanges the code with the verifier of its challenge, the
// refresh token lands in the same store and the access tokens only in memory; refresh and sign-out work. Afterwards no
// client secret, code, verifier, state or token is in any answer of the page, the log, the database files or any row.
func TestGooglePageKeepsTokensSecret(t *testing.T) {
	ctx := context.Background()
	g := testenv.StartFakeGoogleOAuth(t)
	g.ExpiresIn(1) // every use of the access token refreshes it
	g.RotateRefreshTokens(true)

	dir := t.TempDir()
	a, f := newTestAppWith(t, dir)
	useFakeGoogle(a, g)
	var logs syncBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	page := wails.NewGoogleService(startCore(t, a).Google, a.log)

	var answers []any
	act := func(res wails.GoogleResult, err error) wails.GoogleResult {
		t.Helper()
		if err != nil {
			t.Fatalf("binding error: %v", err)
		}
		answers = append(answers, res)
		return res
	}

	if st, err := page.GoogleStatus(ctx); err != nil || st.Client != "unset" || st.Account != "signedOut" {
		t.Fatalf("GoogleStatus = %+v, %v", st, err)
	}
	if got := act(page.GoogleSignIn(ctx)); got.OK || !strings.Contains(got.Message, "Сначала сохраните") {
		t.Fatalf("GoogleSignIn without a client = %+v", got)
	}
	if got := act(page.SetGoogleClient(ctx, testenv.FakeGoogleClientSecret, testenv.FakeGoogleClientSecret)); got.OK {
		t.Fatalf("SetGoogleClient with the secret as the id = %+v", got)
	}
	if got := act(page.SetGoogleClient(ctx, " "+testenv.FakeGoogleClientID+"\r\n", testenv.FakeGoogleClientSecret)); !got.OK ||
		got.State.Client != "set" {
		t.Fatalf("SetGoogleClient = %+v", got)
	}
	if got := act(page.GoogleSignIn(ctx)); !got.OK || got.State.Account != "signedIn" || got.State.SigningIn {
		t.Fatalf("GoogleSignIn = %+v", got)
	}
	// The sign-in asks for a calendar sync (TASK-051): its run refreshes the access token once and finds no Calendar
	// API (a closed port), so the job waits for the network. Waiting here keeps the counts below exact.
	if st := waitCalendarJobs(t, a, 1); st[0] != job.StatusWaitingForNetwork {
		t.Fatalf("calendar sync after the sign-in: %v", st)
	}

	store := f.secrets[googleSecretsService]
	live := g.LiveRefreshTokens()
	if stored, err := store.Get(ctx, googleauth.RefreshTokenName); err != nil || len(live) != 1 || stored != live[0] || store.Len() != 3 ||
		f.secrets[hubSecretsService].Len() != 0 || f.secrets[mailSecretsService].Len() != 0 {
		t.Fatalf("google store %d secrets (%v), hub %d, mail %d", store.Len(), err, f.secrets[hubSecretsService].Len(),
			f.secrets[mailSecretsService].Len())
	}
	if id, _ := store.Get(ctx, googleauth.ClientIDName); id != testenv.FakeGoogleClientID {
		t.Fatal("the client id was not stored trimmed")
	}
	auth, forms := g.AuthRequests(), g.TokenRequests()
	if len(auth) != 1 || len(forms) != 2 || forms[0].Get("code_verifier") == "" || forms[1].Get("grant_type") != "refresh_token" ||
		auth[0].Get("scope") != values.GoogleCalendarScope {
		t.Fatalf("authorizations %d, exchanges %d", len(auth), len(forms))
	}

	// The Calendar side (TASK-051): a refreshed access token from the stored, now rotated, refresh token.
	tok, err := a.core.google.Token(ctx)
	if err != nil || !g.IsLiveAccessToken(tok.AccessToken) {
		t.Fatalf("Token = %v", err)
	}
	if rotated, _ := store.Get(ctx, googleauth.RefreshTokenName); rotated == live[0] {
		t.Fatal("the rotated refresh token was not stored")
	}

	if got := act(page.SetGoogleClient(ctx, testenv.FakeGoogleClientID, "GOCSPX-other")); got.OK || !strings.Contains(got.Message, "Сначала выйдите") {
		t.Fatalf("SetGoogleClient while signed in = %+v", got)
	}
	refresh, _ := store.Get(ctx, googleauth.RefreshTokenName)
	if got := act(page.GoogleSignOut(ctx)); !got.OK || got.State.Account != "signedOut" || got.State.Client != "set" {
		t.Fatalf("GoogleSignOut = %+v", got)
	}
	if revoked := g.Revoked(); len(revoked) != 1 || revoked[0] != refresh || store.Len() != 2 {
		t.Fatalf("revoked %d tokens; store %d secrets", len(revoked), store.Len())
	}
	if _, err := a.core.google.Token(ctx); !errors.Is(err, googleauth.ErrNotSignedIn) {
		t.Fatalf("Token after sign-out = %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	secrets := append([]string{testenv.FakeGoogleClientSecret}, g.Issued()...)
	if len(secrets) < 8 {
		t.Fatalf("only %d values to look for", len(secrets))
	}
	for _, res := range answers {
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range secrets {
			if strings.Contains(string(raw), s) {
				t.Fatalf("a page answer contains a secret: %s", raw)
			}
		}
	}
	dbPath := filepath.Join(dir, dbFile)
	assertNoSecretsIn(t, dbPath, logs.String(), migrationRows(t, dbPath), secrets...)
}

// migrationRows is the number of rows a full scan of a fresh database must see: the migration versions.
func migrationRows(t *testing.T, dbPath string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var versions int
	if err := raw.QueryRow(`SELECT count(*) FROM goose_db_version`).Scan(&versions); err != nil || versions == 0 {
		t.Fatalf("migration versions: %d, %v", versions, err)
	}
	return versions
}

// A sign-in waiting for the browser is cancelled from the page, and the app's shutdown cancels one too.
func TestGoogleSignInCancel(t *testing.T) {
	ctx := context.Background()
	g := testenv.StartFakeGoogleOAuth(t)
	a, f := newTestAppWith(t, t.TempDir())
	useFakeGoogle(a, g)
	f.browser.SetMode(testenv.BrowserIgnore)
	page := wails.NewGoogleService(startCore(t, a).Google, a.log)
	if got, _ := page.SetGoogleClient(ctx, testenv.FakeGoogleClientID, testenv.FakeGoogleClientSecret); !got.OK {
		t.Fatalf("SetGoogleClient = %+v", got)
	}

	signIn := func() chan wails.GoogleResult {
		done := make(chan wails.GoogleResult, 1)
		go func() {
			res, _ := page.GoogleSignIn(ctx)
			done <- res
		}()
		testenv.Eventually(t, "the sign-in waits", func() bool { st, _ := page.GoogleStatus(ctx); return st.SigningIn }, nil)
		return done
	}
	wait := func(done chan wails.GoogleResult) wails.GoogleResult {
		t.Helper()
		select {
		case res := <-done:
			return res
		case <-time.After(5 * time.Second):
			t.Fatal("the sign-in did not end")
			return wails.GoogleResult{}
		}
	}

	done := signIn()
	if got, _ := page.GoogleSignIn(ctx); got.OK || !strings.Contains(got.Message, "уже выполняется") {
		t.Fatalf("a second GoogleSignIn = %+v", got)
	}
	if got, _ := page.GoogleCancelSignIn(ctx); !got.OK || got.Message != "Вход отменён." {
		t.Fatalf("GoogleCancelSignIn = %+v", got)
	}
	if got := wait(done); got.OK || got.Message != "Вход отменён." || got.State.SigningIn {
		t.Fatalf("the cancelled GoogleSignIn = %+v", got)
	}

	done = signIn()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := wait(done); got.OK {
		t.Fatalf("GoogleSignIn after Close = %+v", got)
	}
	if len(g.TokenRequests()) != 0 {
		t.Fatal("a cancelled sign-in exchanged a code")
	}
}

// The bridge maps every class of the adapter to its result; a ctx error is not a result.
func TestGoogleResultMapsTheAdapterClasses(t *testing.T) {
	unsupported := fmt.Errorf("%w: %w", googleauth.ErrCredentials, wincred.ErrUnsupported)
	tests := []struct {
		from, want error
	}{
		{unsupported, googlesignin.ErrSecretsUnsupported},
		{googleauth.ErrCredentials, googlesignin.ErrSecretsUnavailable},
		{googleauth.ErrNotConfigured, googlesignin.ErrClientNotConfigured},
		{fmt.Errorf("%w: %w", googleauth.ErrRevokeFailed, googleauth.ErrNetwork), googlesignin.ErrRevokeFailed},
		{errors.Join(fmt.Errorf("%w: x", googleauth.ErrCredentials), googleauth.ErrRevokeFailed), googlesignin.ErrSecretsUnavailable},
		{googleauth.ErrTimeout, googlesignin.ErrSignInTimeout},
		{googleauth.ErrBrowser, googlesignin.ErrBrowserUnavailable},
		{googleauth.ErrDenied, googlesignin.ErrAccessDenied},
		{fmt.Errorf("%w: %w", googleauth.ErrScope, values.ErrCalendarScopeNotGranted), googlesignin.ErrScopeNotGranted},
		{fmt.Errorf("%w: %w", googleauth.ErrScope, values.ErrScopeTooBroad), googlesignin.ErrScopeTooBroad},
		{googleauth.ErrNoRefreshToken, googlesignin.ErrNoRefreshToken},
		{googleauth.ErrInvalidClient, googlesignin.ErrInvalidClient},
		{googleauth.ErrAuthFailed, googlesignin.ErrGoogleRefused},
		{googleauth.ErrRejected, googlesignin.ErrGoogleRefused},
		{googleauth.ErrBadResponse, googlesignin.ErrGoogleRefused},
		{googleauth.ErrNetwork, googlesignin.ErrGoogleUnreachable},
		{googleauth.ErrUnavailable, googlesignin.ErrGoogleUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.from.Error(), func(t *testing.T) {
			from := fmt.Errorf("googleauth: op: %w", tt.from)
			if err := googleResult(from); !errors.Is(err, tt.want) || !errors.Is(err, tt.from) {
				t.Fatalf("googleResult = %v, want %v", err, tt.want)
			}
		})
	}
	for _, err := range []error{nil, context.Canceled, googleauth.ErrLoopback} {
		if got := googleResult(err); got != err {
			t.Fatalf("googleResult(%v) = %v, want it unchanged", err, got)
		}
	}
}

// Off Windows the google store refuses every call: the section says so.
func TestGoogleWithoutCredentialStore(t *testing.T) {
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	f.secrets[googleSecretsService].Fail(wincred.ErrUnsupported)
	page := wails.NewGoogleService(startCore(t, a).Google, a.log)

	if st, _ := page.GoogleStatus(ctx); st.Client != "unsupported" || st.Account != "unsupported" {
		t.Fatalf("GoogleStatus = %+v", st)
	}
	for name, call := range map[string]func() (wails.GoogleResult, error){
		"set":     func() (wails.GoogleResult, error) { return page.SetGoogleClient(ctx, testenv.FakeGoogleClientID, "s") },
		"sign in": func() (wails.GoogleResult, error) { return page.GoogleSignIn(ctx) },
	} {
		if got, err := call(); err != nil || got.OK || !strings.Contains(got.Message, "недоступен в этой системе") {
			t.Fatalf("%s = %+v, %v", name, got, err)
		}
	}
	if n := len(f.browser.Opened()); n != 0 {
		t.Fatalf("the browser was opened %d times", n)
	}
}
