package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// spyStore counts every call, to prove that a profile's stores were not even read.
type spyStore struct {
	*testenv.MemCredentials
	mu    sync.Mutex
	calls int
}

func (s *spyStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *spyStore) touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
}

func (s *spyStore) Set(ctx context.Context, name, secret string) error {
	s.touch()
	return s.MemCredentials.Set(ctx, name, secret)
}

func (s *spyStore) Get(ctx context.Context, name string) (string, error) {
	s.touch()
	return s.MemCredentials.Get(ctx, name)
}

func (s *spyStore) Exists(ctx context.Context, name string) (bool, error) {
	s.touch()
	return s.MemCredentials.Exists(ctx, name)
}

func (s *spyStore) Delete(ctx context.Context, name string) error {
	s.touch()
	return s.MemCredentials.Delete(ctx, name)
}

// profileStores is a fake Credential Manager: one store per profile and service, made on first use.
type profileStores struct {
	mu     sync.Mutex
	stores map[wincred.Profile]map[string]*spyStore
}

func (p *profileStores) open(profile wincred.Profile, service string) (secretStore, error) {
	return p.get(profile, service), nil
}

func (p *profileStores) get(profile wincred.Profile, service string) *spyStore {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stores == nil {
		p.stores = map[wincred.Profile]map[string]*spyStore{}
	}
	if p.stores[profile] == nil {
		p.stores[profile] = map[string]*spyStore{}
	}
	s := p.stores[profile][service]
	if s == nil {
		s = &spyStore{MemCredentials: testenv.NewMemCredentials()}
		p.stores[profile][service] = s
	}
	return s
}

// Only the default data dir, however it is spelled, opens the stores of the default profile; any other dir opens
// those of its own profile and says so in the log.
func TestOpenCoreSecretsProfile(t *testing.T) {
	base := t.TempDir()
	def := filepath.Join(base, "LearningAssistant")
	other := filepath.Join(base, "temp-data")
	for _, d := range []string{def, other} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	isolated := wincred.ProfileFor(other, def)
	if !isolated.Isolated() {
		t.Fatal("another dir must have an isolated profile")
	}

	cases := []struct {
		desc       string
		dataDir    string
		defaultDir string
		want       wincred.Profile
		wantLog    string
	}{
		{"default dir", def, def, wincred.DefaultProfile, "secrets_profile=default"},
		{"default dir with a trailing separator", def + string(filepath.Separator), def, wincred.DefaultProfile, "secrets_profile=default"},
		{"default dir spelled with ..", filepath.Join(other, "..", "LearningAssistant"), def, wincred.DefaultProfile, "secrets_profile=default"},
		{"other dir", other, def, isolated, "secrets_profile=isolated"},
		{"new dir created by the app", filepath.Join(base, "new"), def, "", "secrets_profile=isolated"},
		{"default unknown", def, "", wincred.ProfileFor(def, ""), "secrets_profile=isolated"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			a, f := newTestAppWith(t, c.dataDir)
			a.cfg.DefaultDataDir = c.defaultDir
			var logs syncBuffer
			a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

			if _, err := a.openCore(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := c.want
			if want == "" {
				want = wincred.ProfileFor(c.dataDir, c.defaultDir)
			}
			for _, service := range []string{hubSecretsService, mailSecretsService, googleSecretsService} {
				got, ok := f.profiles[service]
				if !ok || got != want {
					t.Errorf("%s store opened in profile %q (%v), want %q", service, got, ok, want)
				}
			}
			out := logs.String()
			if !strings.Contains(out, c.wantLog) {
				t.Errorf("log has no %q:\n%s", c.wantLog, out)
			}
			// The id hashes a path with the user name: only a debug line of an isolated profile has it.
			wantID := "secrets_profile_id=" + string(want)
			var idLines int
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, "secrets_profile_id") {
					continue
				}
				idLines++
				if !strings.Contains(line, "level=DEBUG") || !strings.Contains(line, wantID) {
					t.Errorf("profile id line %q, want level=DEBUG and %q", line, wantID)
				}
			}
			if wantLines := map[bool]int{true: 1, false: 0}[want.Isolated()]; idLines != wantLines {
				t.Errorf("%d lines with the profile id, want %d:\n%s", idLines, wantLines, out)
			}
		})
	}
}

// AC of TASK-071: a run on a temporary data dir neither reads nor changes the Hub key, the mail password, the Google
// client and the Google sign-in of the default profile. The default dir, as the control, sees them.
func TestTemporaryDataDirLeavesDefaultSecretsAlone(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	def := filepath.Join(base, "LearningAssistant")
	if err := os.Mkdir(def, 0o700); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		desc        string
		dataDir     string
		wantDefault bool
	}{
		{"temporary dir", filepath.Join(base, "smoke-data"), false},
		{"default dir", def, true},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			var stores profileStores
			seed := map[string]map[string]string{
				hubSecretsService:  {hubclient.DefaultCredentialName: "hub-key-of-the-owner"},
				mailSecretsService: {connections.DefaultMailSecretName: "mail-password-of-the-owner"},
				googleSecretsService: {
					googleauth.ClientIDName: testenv.FakeGoogleClientID, googleauth.ClientSecretName: testenv.FakeGoogleClientSecret,
					googleauth.RefreshTokenName: "refresh-token-of-the-owner",
				},
			}
			for service, entries := range seed {
				for name, secret := range entries {
					if err := stores.get(wincred.DefaultProfile, service).MemCredentials.Set(ctx, name, secret); err != nil {
						t.Fatal(err)
					}
				}
			}

			a, _ := newTestAppWith(t, c.dataDir)
			a.cfg.DefaultDataDir = def
			a.env.secrets = stores.open
			core := startCore(t, a)

			google, err := wails.NewGoogleService(core.Google, a.log).GoogleStatus(ctx)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := wails.NewConnectionsService(core.Connections, a.log).GetConnections(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}

			if c.wantDefault {
				if google.Client != "set" || google.Account != "signedIn" || conn.HubKey != "set" {
					t.Fatalf("default dir: google %+v, hub key %q; want the default secrets", google, conn.HubKey)
				}
				return
			}
			if google.Client != "unset" || google.Account != "signedOut" || conn.HubKey != "unset" || conn.MailPassword == "set" {
				t.Fatalf("temporary dir: google %+v, hub key %q, mail %q; want nothing configured", google, conn.HubKey, conn.MailPassword)
			}
			for service, entries := range seed {
				s := stores.get(wincred.DefaultProfile, service)
				if n := s.count(); n != 0 {
					t.Errorf("%s store of the default profile was called %d times", service, n)
				}
				for name, secret := range entries {
					if got, err := s.MemCredentials.Get(ctx, name); err != nil || got != secret {
						t.Errorf("%s/%s of the default profile changed: %v", service, name, err)
					}
				}
			}
		})
	}
}
