package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// e2eDirs is a fake profile layout: the owner's default data dir and WebView2 folder, and the run's folders.
type e2eDirs struct {
	base, def, webview, run, data string
}

func newE2EDirs(t *testing.T) e2eDirs {
	t.Helper()
	base := t.TempDir()
	d := e2eDirs{
		base:    base,
		def:     filepath.Join(base, "Local", "LearningAssistant"),
		webview: filepath.Join(base, "Roaming", "learning-assistant.exe"),
		run:     filepath.Join(base, "run"),
	}
	d.data = filepath.Join(d.run, "data")
	for _, dir := range []string{d.def, d.webview, d.run} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func (d e2eDirs) config(headless bool) config.Config {
	cfg := config.Default()
	cfg.Headless = headless
	cfg.DataDir, cfg.DefaultDataDir, cfg.WebviewDataDir = d.data, d.def, d.webview
	cfg.E2E = config.E2E{Dir: d.run, CDPPort: 9333, Window: "1280x800"}
	return cfg
}

func TestShellOptions(t *testing.T) {
	d := newE2EDirs(t)
	normal := d.config(false)
	normal.E2E = config.E2E{}

	got := shellOptions(normal)
	if got.Name != appName || got.InstanceID != instanceID || got.WebviewDataDir != d.webview || got.Title != "" ||
		got.BrowserArgs != nil || got.MainWidth != 0 || got.MainHeight != 0 {
		t.Fatalf("normal mode: %+v, want the app's own identity and nothing of the E2E mode", got)
	}

	got = shellOptions(d.config(false))
	if got.Name != appName || got.InstanceID != "paratech.learning-assistant.desktop.e2e" {
		t.Errorf("e2e name %q, instance id %q", got.Name, got.InstanceID)
	}
	if want := filepath.Join(d.run, "webview"); got.WebviewDataDir != want {
		t.Errorf("e2e WebView2 folder %q, want %q", got.WebviewDataDir, want)
	}
	if got.Title != "paratech · E2E" || !strings.HasSuffix(got.Title, " · E2E") {
		t.Errorf("e2e title %q", got.Title)
	}
	if !slices.Equal(got.BrowserArgs, []string{"--remote-debugging-port=9333"}) {
		t.Errorf("e2e browser args %q", got.BrowserArgs)
	}
	if got.MainWidth != 1280 || got.MainHeight != 800 {
		t.Errorf("e2e main window %dx%d, want 1280x800", got.MainWidth, got.MainHeight)
	}

	cfg := d.config(false)
	cfg.E2E.CDPPort, cfg.E2E.Window = 0, ""
	if got := shellOptions(cfg); got.BrowserArgs != nil || got.MainWidth != 0 || got.MainHeight != 0 {
		t.Errorf("e2e without port and window: %+v, want no browser args and the normal size", got)
	}
}

// The E2E mode never registers the paratech: links; the same app outside it does.
func TestE2EDoesNotRegisterTheScheme(t *testing.T) {
	for _, e2e := range []bool{true, false} {
		d := newE2EDirs(t)
		cfg := d.config(false)
		if !e2e {
			cfg.E2E = config.E2E{}
		}
		a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if a.shell == nil {
			t.Fatal("no UI shell")
		}
		a.env, _ = testEnv(t)
		scheme := &fakeScheme{}
		a.env.scheme = scheme
		if _, err := a.openCore(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		want := map[bool]int{true: 0, false: 1}[e2e]
		if len(scheme.calls) != want {
			t.Fatalf("e2e %v: %d registrations, want %d", e2e, len(scheme.calls), want)
		}
	}
}

func TestE2ERefusesTheOwnersFolders(t *testing.T) {
	type tc struct {
		name    string
		edit    func(d e2eDirs, c *config.Config)
		wantErr string
	}
	cases := []tc{
		{"default data dir", func(d e2eDirs, c *config.Config) { c.DataDir = d.def }, "data dir must be apart"},
		{"default with a trailing separator", func(d e2eDirs, c *config.Config) { c.DataDir = d.def + string(filepath.Separator) }, "data dir must be apart"},
		{"default spelled with ..", func(d e2eDirs, c *config.Config) {
			c.DataDir = filepath.Join(d.run, "..", "Local", "x", "..", "LearningAssistant")
		}, "data dir must be apart"},
		{"default with doubled separators", func(d e2eDirs, c *config.Config) {
			c.DataDir = strings.Replace(d.def, string(filepath.Separator)+"LearningAssistant", strings.Repeat(string(filepath.Separator), 2)+"LearningAssistant", 1)
		}, "data dir must be apart"},
		{"inside the default", func(d e2eDirs, c *config.Config) { c.DataDir = filepath.Join(d.def, "artifacts") }, "data dir must be apart"},
		{"holding the default", func(d e2eDirs, c *config.Config) { c.DataDir = filepath.Join(d.base, "Local") }, "data dir must be apart"},
		{"e2e dir inside the default", func(d e2eDirs, c *config.Config) {
			c.E2E.Dir = filepath.Join(d.def, "run")
			c.DataDir = filepath.Join(d.base, "elsewhere")
		}, "e2e dir must be apart from the default data dir"},
		{"e2e dir is the owner's WebView2 folder", func(d e2eDirs, c *config.Config) { c.E2E.Dir = d.webview }, "WebView2 folder"},
		{"default data dir unknown", func(_ e2eDirs, c *config.Config) { c.DefaultDataDir = "" }, "default data dir is unknown"},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, tc{"default in another case", func(d e2eDirs, c *config.Config) { c.DataDir = strings.ToUpper(d.def) }, "data dir must be apart"})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newE2EDirs(t)
			cfg := d.config(true)
			c.edit(d, &cfg)
			if _, err := New(cfg, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("New = %v, want %q", err, c.wantErr)
			}

			// openCore checks again: an app built before the mode was set must not open the owner's data either.
			a, _ := newTestAppWith(t, d.data)
			a.cfg = cfg
			_, err := a.openCore(context.Background())
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("openCore = %v, want %q", err, c.wantErr)
			}
			if _, err := os.Stat(filepath.Join(cfg.DataDir, dbFile)); err == nil {
				t.Fatal("the refused start created a database")
			}
		})
	}
}

// A link to the default data dir has another name but the same file identity.
func TestE2ERefusesALinkToTheDefaultDir(t *testing.T) {
	d := newE2EDirs(t)
	link := filepath.Join(d.base, "link")
	if err := os.Symlink(d.def, link); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	for _, dataDir := range []string{link, filepath.Join(link, "sub")} {
		cfg := d.config(true)
		cfg.DataDir = dataDir
		if err := checkE2EDirs(cfg); err == nil {
			t.Errorf("%s: accepted", dataDir)
		}
	}
}

func TestCheckE2EProfile(t *testing.T) {
	d := newE2EDirs(t)
	cfg := d.config(true)
	cfg.DataDir = d.def
	if err := checkE2EProfile(cfg); err == nil || !strings.Contains(err.Error(), "default credential profile") {
		t.Fatalf("default dir: %v", err)
	}
	cfg.DataDir = d.run
	if err := checkE2EProfile(cfg); err != nil {
		t.Fatalf("own dir: %v", err)
	}
	cfg.E2E, cfg.DataDir = config.E2E{}, d.def
	if err := checkE2EProfile(cfg); err != nil {
		t.Fatalf("outside E2E mode: %v", err)
	}
}

// The start log says the mode is on, with the run's folders and port, and keeps the profile isolated.
func TestE2ELogsTheMode(t *testing.T) {
	d := newE2EDirs(t)
	a, _ := newTestAppWith(t, d.data)
	a.cfg = d.config(true)
	var logs syncBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := a.openCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, `msg="e2e mode on"`) {
			line = l
		}
	}
	for _, want := range []string{"level=INFO", "data_dir=", "cdp_port=9333", "window=1280x800"} {
		if !strings.Contains(line, want) {
			t.Errorf("e2e line %q has no %q", line, want)
		}
	}
	if !strings.Contains(out, "secrets_profile=isolated") || strings.Contains(out, "secrets_profile_id") {
		t.Errorf("log:\n%s\nwant an isolated profile and no profile id at info", out)
	}

	// Outside the mode there is no such line.
	plain, _ := newTestAppWith(t, filepath.Join(t.TempDir(), "data"))
	var plainLogs syncBuffer
	plain.log = slog.New(slog.NewTextHandler(&plainLogs, nil))
	if _, err := plain.openCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plainLogs.String(), "e2e") {
		t.Errorf("normal start logs the e2e mode:\n%s", plainLogs.String())
	}
}

// toFakeHub sends every request to the fake Hub whatever its host, and records the hosts asked for.
type toFakeHub struct {
	hub   *url.URL
	next  http.RoundTripper
	mu    sync.Mutex
	hosts []string
}

func (r *toFakeHub) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, req.URL.Host)
	r.mu.Unlock()
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host, req.Host = r.hub.Scheme, r.hub.Host, ""
	return r.next.RoundTrip(req)
}

func (r *toFakeHub) asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.hosts)
}

// AC6 of TASK-372 (ADR-0035 §3): in E2E mode a stored Hub URL of another host reads as not configured and nothing is
// sent, a loopback one works; outside the mode the same remote URL is used.
func TestE2EHubIsLoopbackOnly(t *testing.T) {
	const remote = "https://hub.example.org"
	tests := []struct {
		name       string
		e2e        bool
		loopback   bool
		wantStatus string
	}{
		{"e2e, remote url", true, false, "notConfigured"},
		{"e2e, loopback url", true, true, "current"},
		{"normal, remote url", false, false, "current"},
		{"normal, loopback url", false, true, "current"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			hub := testenv.NewFakeHub()
			hub.SetRemaining(600, 0, 1000)
			hubURL, hubHTTP := testenv.StartFakeHub(t, hub)
			target, err := url.Parse(hubURL)
			if err != nil {
				t.Fatal(err)
			}
			transport := &toFakeHub{hub: target, next: hubHTTP.Transport}

			d := newE2EDirs(t)
			a, f := newTestAppWith(t, d.data)
			if tt.e2e {
				a.cfg = d.config(true)
			}
			a.env.hubHTTP = &http.Client{Transport: transport}
			if err := f.secrets[hubSecretsService].Set(ctx, hubclient.DefaultCredentialName, testHubKey); err != nil {
				t.Fatal(err)
			}
			page := wails.NewConnectionsService(startCore(t, a).Connections, a.log)

			stored := map[bool]string{true: hubURL, false: remote}[tt.loopback]
			if saved, err := page.SaveHubURL(ctx, stored); err != nil || !saved.Saved {
				t.Fatalf("SaveHubURL(%q) = %+v, %v", stored, saved, err)
			}
			got, err := page.RefreshQuota(ctx)
			if err != nil || got.Quota.Status != tt.wantStatus {
				t.Fatalf("RefreshQuota = %+v, %v; want status %q", got, err, tt.wantStatus)
			}

			asked := transport.asked()
			if tt.wantStatus == "notConfigured" {
				if len(asked) != 0 || len(hub.QuotaAuthorizations()) != 0 {
					t.Fatalf("a not configured Hub was asked: hosts %q", asked)
				}
				return
			}
			want := map[bool]string{true: target.Host, false: "hub.example.org"}[tt.loopback]
			if len(asked) == 0 || asked[0] != want {
				t.Fatalf("hosts asked %q, want %q", asked, want)
			}
		})
	}
}

// fakeHubURL is a hubURLSource with one stored value.
type fakeHubURL struct {
	url string
	err error
}

func (f fakeHubURL) HubURL(context.Context) (string, error) { return f.url, f.err }

func TestLoopbackHub(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"http://127.0.0.1:8090", true},
		{"http://127.0.0.1", true},
		{"https://127.0.0.1:8443/api", true},
		{"http://localhost:8090", true},
		{"http://LOCALHOST:8090", true},
		{"http://[::1]:8090", true},
		{"http://[::1]", true},
		{"", false},
		{"http://localhost.evil.com", false},
		{"http://127.0.0.1.evil.com", false},
		{"http://127.0.0.1@evil.com", false},
		{"http://user:pw@127.0.0.1:8090", false},
		{"http://0.0.0.0:8090", false},
		{"http://127.0.0.2:8090", false},
		{"http://[::ffff:127.0.0.1]:8090", false},
		{"http://[fe80::1%25eth0]:8090", false},
		{"http://[::1%25lo]:8090", false},
		{"https://hub.example.com", false},
		{"http://evil.com/127.0.0.1", false},
		{"http://evil.com?h=localhost", false},
		{"127.0.0.1:8090", false},
		{"localhost", false},
		{"http://%zz", false},
		{"http://localhost.:8090", false},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := loopbackURL(tt.raw); got != tt.want {
				t.Fatalf("loopbackURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			got, err := loopbackHub{fakeHubURL{url: tt.raw}}.HubURL(ctx)
			want := map[bool]string{true: tt.raw, false: ""}[tt.want]
			if err != nil || got != want {
				t.Fatalf("HubURL = %q, %v; want %q (empty: not configured)", got, err, want)
			}
		})
	}

	boom := errors.New("settings unreadable")
	if _, err := (loopbackHub{fakeHubURL{url: "http://127.0.0.1", err: boom}}).HubURL(ctx); !errors.Is(err, boom) {
		t.Fatalf("a failed read: %v, want it passed on", err)
	}
}
