package supportbundle

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// memArchive keeps the entries of the last bundle.
type memArchive struct {
	path    string
	names   []string
	entries map[string]string
	err     error
}

func (a *memArchive) WriteZip(ctx context.Context, path string, fill func(add func(string, []byte) error) error) error {
	if a.err != nil {
		return a.err
	}
	a.path, a.names, a.entries = path, nil, map[string]string{}
	return fill(func(name string, data []byte) error {
		a.names = append(a.names, name)
		a.entries[name] = string(data)
		return nil
	})
}

type fakeLogs struct {
	files map[string]string
	limit int64
}

func (f *fakeLogs) EachLog(_ context.Context, maxBytes int64, fn func(string, []byte) error) error {
	f.limit = maxBytes
	for _, name := range []string{"desktop.log", "desktop.1.log"} {
		if data, ok := f.files[name]; ok {
			if err := fn(name, []byte(data)); err != nil {
				return err
			}
		}
	}
	return nil
}

type fakeSystem struct{ profile string }

func (fakeSystem) Windows(context.Context) values.WindowsVersion {
	return values.WindowsVersion{Product: "Windows 10 Pro", DisplayVersion: "23H2", Major: 10, Build: 22631, UBR: 4317}
}

func (fakeSystem) Build() (*debug.BuildInfo, bool) {
	return &debug.BuildInfo{GoVersion: "go1.26.0", Main: debug.Module{Path: "paratech/desktop", Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.time", Value: "2026-10-15T21:00:00Z"},
			{Key: "vcs.modified", Value: "true"}, {Key: "GOOS", Value: "windows"}, {Key: "GOARCH", Value: "amd64"},
		}}, true
}

func (s fakeSystem) ProfileDir() string { return s.profile }

type fakeSettings struct{ err error }

func (f fakeSettings) Load(context.Context) (*settings.Settings, error) {
	if f.err != nil {
		return nil, f.err
	}
	s := settings.New()
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	if err := s.SetSMTPLogin("student@mail.example", now); err != nil {
		return nil, err
	}
	return s, nil
}

type fakeCaptures []values.CaptureDiagnostics

func (f fakeCaptures) RecentCaptures(context.Context) []values.CaptureDiagnostics { return f }

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

const profile = `C:\Users\Студент`

func newService(t *testing.T, archive *memArchive, logs *fakeLogs, set fakeSettings) *Service {
	t.Helper()
	s, err := New(Deps{
		Archive: archive, Logs: logs, System: fakeSystem{profile: profile}, Settings: set,
		Captures: fakeCaptures{{
			TargetMode: values.CaptureInclude, App: "chrome.exe", Mode: values.CaptureSystem, Device: "Speakers",
			Attempts: []values.CaptureAttempt{{Mode: values.CaptureInclude, HResult: "0x80070005",
				Error: `open c:\users\студент\AppData\Local\LearningAssistant\x: denied`}},
			Switches: []values.CaptureSwitch{{From: values.CaptureInclude, To: values.CaptureSystem, Cause: values.SwitchActivationFailed}},
		}},
		Clock: fixedClock(time.Date(2026, 10, 16, 9, 5, 7, 0, time.FixedZone("MSK", 3*3600))),
	}, Config{MaxLogBytes: 1234})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreate(t *testing.T) {
	archive := &memArchive{}
	logs := &fakeLogs{files: map[string]string{
		"desktop.log": `time=... level=INFO msg="model imported" path="C:\\Users\\Студент\\Downloads\\m.bin"` + "\n" +
			`level=INFO msg=x dir=C:/Users/Студент/AppData` + "\n",
		"desktop.1.log": "level=INFO msg=y dir=c:\\USERS\\студент\\AppData\n",
	}}
	s := newService(t, archive, logs, fakeSettings{})
	if err := s.Create(context.Background(), `D:\bundle.zip`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"about.json", "settings.json", "capture.json", "logs/desktop.log", "logs/desktop.1.log"}; !slices.Equal(archive.names, want) {
		t.Fatalf("entries %v, want %v", archive.names, want)
	}
	if archive.path != `D:\bundle.zip` || logs.limit != 1234 {
		t.Fatalf("path %q, log limit %d", archive.path, logs.limit)
	}
	for name, data := range archive.entries {
		if strings.Contains(strings.ToLower(data), "студент") {
			t.Errorf("%s keeps the profile directory:\n%s", name, data)
		}
	}
	if !strings.Contains(archive.entries["logs/desktop.log"], `path="%USERPROFILE%\\Downloads\\m.bin"`) ||
		!strings.Contains(archive.entries["logs/desktop.log"], "dir=%USERPROFILE%/AppData") ||
		!strings.Contains(archive.entries["logs/desktop.1.log"], `dir=%USERPROFILE%\AppData`) {
		t.Fatalf("logs:\n%s", archive.entries)
	}

	var a about
	if err := json.Unmarshal([]byte(archive.entries["about.json"]), &a); err != nil {
		t.Fatal(err)
	}
	want := about{
		Created: "2026-10-16T09:05:07+03:00",
		App: appBuild{Module: "paratech/desktop", Version: "(devel)", Revision: "abc123", RevisionTime: "2026-10-15T21:00:00Z",
			Modified: true, Go: "go1.26.0", OS: "windows/amd64"},
		Windows: windowsInfo{Name: "Windows 11 Pro", DisplayVersion: "23H2", Build: "10.0.22631.4317", Windows11: true},
	}
	if a != want {
		t.Fatalf("about = %+v\nwant %+v", a, want)
	}

	var set settingsInfo
	if err := json.Unmarshal([]byte(archive.entries["settings.json"]), &set); err != nil {
		t.Fatal(err)
	}
	if set.SMTPLogin != "***@mail.example" || !set.RecordingPromptsEnabled || set.SMTPSecurity != "starttls" {
		t.Fatalf("settings = %+v", set)
	}

	var caps []captureInfo
	if err := json.Unmarshal([]byte(archive.entries["capture.json"]), &caps); err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 || caps[0].App != "chrome.exe" || caps[0].Switches[0].Cause != "activation_failed" ||
		!strings.HasPrefix(caps[0].Attempts[0].Error, `open %USERPROFILE%\AppData`) {
		t.Fatalf("capture = %+v", caps)
	}
}

func TestCreateWithoutSettings(t *testing.T) {
	archive := &memArchive{}
	s := newService(t, archive, &fakeLogs{}, fakeSettings{err: errors.New("database locked")})
	if err := s.Create(context.Background(), `D:\bundle.zip`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"about.json", "capture.json"}; !slices.Equal(archive.names, want) {
		t.Fatalf("entries %v, want %v", archive.names, want)
	}
}

func TestCreateFails(t *testing.T) {
	boom := errors.New("disk full")
	s := newService(t, &memArchive{err: boom}, &fakeLogs{}, fakeSettings{})
	if err := s.Create(context.Background(), `D:\bundle.zip`); !errors.Is(err, boom) {
		t.Fatalf("Create = %v", err)
	}
}

func TestSuggestedName(t *testing.T) {
	s := newService(t, &memArchive{}, &fakeLogs{}, fakeSettings{})
	if got := s.SuggestedName(); got != "paratech-support-20261016-090507.zip" {
		t.Fatalf("SuggestedName = %q", got)
	}
}

func TestNewNeedsThePorts(t *testing.T) {
	if _, err := New(Deps{}, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New = %v", err)
	}
}

func TestReplaceFold(t *testing.T) {
	tests := []struct{ s, old, want string }{
		{`C:\Users\Ann\x`, `C:\Users\Ann`, `#\x`},
		{`c:\users\ANN\x and C:\USERS\ann`, `C:\Users\Ann`, `#\x and #`},
		{`nothing here`, `C:\Users\Ann`, `nothing here`},
		{`C:\Users\Annette`, `C:\Users\Ann`, `#ette`}, // a prefix: still hidden, the name is the private part
		{`short`, `longer than s`, `short`},
	}
	for _, tt := range tests {
		if got := replaceFold(tt.s, tt.old, "#"); got != tt.want {
			t.Errorf("replaceFold(%q) = %q, want %q", tt.s, got, tt.want)
		}
	}
	if r := newRedactor(`C:\`); len(r.needles) != 0 {
		t.Fatalf("a drive root is not hidden: %v", r.needles)
	}
}
