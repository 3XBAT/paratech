package values

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		raw  string
		port int // 0: not local
	}{
		{raw: "http://127.0.0.1:8080", port: 8080},
		{raw: "http://127.0.0.1:8080/", port: 8080},
		{raw: "http://localhost:1", port: 1},
		{raw: "http://LOCALHOST:65535", port: 65535},
		{raw: "HTTP://localhost:8080", port: 8080},

		{raw: ""},
		{raw: "https://127.0.0.1:8080"},
		{raw: "http://127.0.0.1"},
		{raw: "http://127.0.0.1:"},
		{raw: "http://127.0.0.1:0"},
		{raw: "http://127.0.0.1:65536"},
		{raw: "http://127.0.0.1:08080"},
		{raw: "http://127.0.0.2:8080"},
		{raw: "http://[::1]:8080"},
		{raw: "http://0.0.0.0:8080"},
		{raw: "http://hub.example.test:8080"},
		{raw: "http://localhost.example.test:8080"},
		{raw: "http://127.0.0.1:8080/hub"},
		{raw: "http://127.0.0.1:8080//"},
		{raw: "http://127.0.0.1:8080/%2F"},
		{raw: "http://127.0.0.1:8080?x=1"},
		{raw: "http://127.0.0.1:8080/?"},
		{raw: "http://127.0.0.1:8080#f"},
		{raw: "http://127.0.0.1:8080/#"},
		{raw: "http://user:pass@127.0.0.1:8080"},
		{raw: "http:127.0.0.1:8080"},
		{raw: "127.0.0.1:8080"},
		{raw: "http://127.0.0.1:80 80"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseTarget(tt.raw)
			if tt.port == 0 {
				if !errors.Is(err, ErrNotLocal) || !got.IsZero() {
					t.Fatalf("ParseTarget = %v, %v; want ErrNotLocal", got, err)
				}
				if strings.Contains(tt.raw, "pass") && strings.Contains(err.Error(), "pass") {
					t.Fatalf("error quotes the address: %v", err)
				}
				return
			}
			if err != nil || got.Port() != tt.port {
				t.Fatalf("ParseTarget = %v, %v; want port %d", got, err, tt.port)
			}
			wantListen := "127.0.0.1:" + strconv.Itoa(tt.port)
			if got.ListenAddr() != wantListen || got.BaseURL() != "http://"+wantListen {
				t.Fatalf("ListenAddr = %s, BaseURL = %s; want %s", got.ListenAddr(), got.BaseURL(), wantListen)
			}
		})
	}
}

func TestNewServerSettings(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		auth  AuthMode
		quota int64
		ok    bool
	}{
		{name: "all defaults", ok: true},
		{name: "https", base: "https://api.anthropic.example", auth: AuthXAPIKey, quota: 1000, ok: true},
		{name: "https with gateway path", base: "https://gw.example.test/anthropic/", auth: AuthBearer, ok: true},
		{name: "http to localhost", base: "http://localhost:9000", ok: true},
		{name: "http to 127.0.0.2", base: "http://127.0.0.2:9000", ok: true},
		{name: "http to ::1", base: "http://[::1]:9000", ok: true},

		{name: "http to a remote host", base: "http://gw.example.test"},
		{name: "ftp", base: "ftp://gw.example.test"},
		{name: "relative", base: "/anthropic"},
		{name: "no host", base: "https://"},
		{name: "user info", base: "https://u:secretpw@gw.example.test"},
		{name: "query", base: "https://gw.example.test?k=v"},
		{name: "empty query", base: "https://gw.example.test?"},
		{name: "fragment", base: "https://gw.example.test#x"},
		{name: "dot segment", base: "https://gw.example.test/a/../b"},
		{name: "empty segment", base: "https://gw.example.test/a//b"},
		{name: "escaped path", base: "https://gw.example.test/a%2Fb"},
		{name: "space", base: "https://gw.example.test/a b"},
		{name: "backslash", base: "https://gw.example.test\\a"},
		{name: "too long", base: "https://gw.example.test/" + strings.Repeat("a", maxBaseURLLen)},
		{name: "path too long", base: "https://gw.example.test/" + strings.Repeat("a", maxBasePath+1)},
		{name: "unknown auth", auth: "sk-fake-key-pasted-here"},
		{name: "negative quota", quota: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewServerSettings(tt.base, tt.auth, tt.quota)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidSettings) {
					t.Fatalf("err = %v, want ErrInvalidSettings", err)
				}
				for _, secret := range []string{"secretpw", "sk-fake"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error quotes the input: %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.BaseURL() != tt.base || s.Auth() != tt.auth || s.QuotaLimit() != tt.quota {
				t.Fatalf("settings = %+v", s)
			}
		})
	}
}

func TestParseTypedSecret(t *testing.T) {
	for _, name := range []string{"provider-key", "database-url"} {
		if n, err := ParseTypedSecret(name); err != nil || n.String() != name {
			t.Fatalf("ParseTypedSecret(%s) = %v, %v", name, n, err)
		}
	}
	for _, name := range []string{"seal-keys", "", "api-key", "PROVIDER-KEY", "sk-fake-value"} {
		_, err := ParseTypedSecret(name)
		if !errors.Is(err, ErrUnknownSecret) {
			t.Fatalf("ParseTypedSecret(%q) err = %v", name, err)
		}
		if strings.Contains(err.Error(), "sk-fake-value") {
			t.Fatalf("error quotes the name: %v", err)
		}
	}
}

func TestEveryReasonHasANextStep(t *testing.T) {
	seen := map[NextStep]bool{}
	for _, r := range Reasons() {
		step := r.NextStep()
		if step == NextNone {
			t.Errorf("reason %s has no next step", r)
		}
		seen[step] = true
	}
	if len(Reasons()) != 8 || len(seen) != 8 {
		t.Fatalf("reasons %d, distinct next steps %d; want 8 and 8", len(Reasons()), len(seen))
	}
	if ReasonNone.NextStep() != NextNone || Reason("bogus").NextStep() != NextNone {
		t.Fatal("no reason must have no next step")
	}
}

func TestDiagnosisConstructors(t *testing.T) {
	tests := []struct {
		name     string
		d        Diagnosis
		status   Status
		reason   Reason
		exit     int
		canStart bool
	}{
		{name: "remote", d: Remote(), status: StatusRemote},
		{name: "running", d: Running(), status: StatusRunning},
		{name: "starting", d: Starting(), status: StatusStarting},
		{name: "degraded", d: Degraded(ReasonDatabaseDown), status: StatusDegraded, reason: ReasonDatabaseDown},
		{name: "not running", d: NotRunning(ReasonDockerOff), status: StatusNotRunning, reason: ReasonDockerOff},
		{name: "exit 2", d: Exited(ExitConfigError), status: StatusNotRunning, reason: ReasonConfigError, exit: 2},
		{name: "exit 1", d: Exited(1), status: StatusNotRunning, reason: ReasonExited, exit: 1},
		{name: "not started", d: Exited(ExitNotStarted), status: StatusNotRunning, reason: ReasonExited, exit: -1},
		{name: "ready", d: ReadyToStart(), status: StatusNotRunning, canStart: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.d
			if d.Status() != tt.status || d.Reason() != tt.reason || d.ExitCode() != tt.exit || d.CanStart() != tt.canStart || d.IsZero() {
				t.Fatalf("diagnosis = %+v", d)
			}
		})
	}
	if !(Diagnosis{}).IsZero() {
		t.Fatal("zero diagnosis")
	}
}
