package wails

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// pngIcon is a fake PNG of size bytes: only its signature matters to the binding.
func pngIcon(size int) []byte {
	b := make([]byte, size)
	copy(b, pngSignature)
	return b
}

// The picker JSON: exe names, untrusted titles as they are, the browser flag and icons only as small PNG data URLs.
func TestPromptServiceSources(t *testing.T) {
	small := pngIcon(64)
	uc := &fakePrompt{sources: recordingprompt.Sources{
		Apps: []values.AudioApp{
			{App: "chrome.exe", Name: "Google Chrome", Title: "<img src=x onerror=alert(1)>", PID: 7, Playing: true, Sounding: true, IconPNG: small},
			{App: "zoom.exe", Name: "Zoom", Title: "Zoom", Muted: true, IconPNG: []byte("GIF89a")},
			{App: "teams.exe", IconPNG: pngIcon(maxIconBytes + 1)},
			{App: "telemost.exe", IconPNG: pngIcon(maxIconBytes)},
			{App: "obs64.exe", IconPNG: pngSignature[:4]},
		},
		Selected:     mustSource(t, "zoom.exe"),
		AutoSelected: true,
		Remembered:   "zoom.exe",
	}}
	got, err := NewPromptService(uc, discard()).Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Apps) != 5 || got.Apps[3].Icon != iconURLPrefix+base64.StdEncoding.EncodeToString(pngIcon(maxIconBytes)) {
		t.Fatalf("apps %+v", got.Apps)
	}
	got.Apps[3].Icon = "max"

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"apps":[` +
		`{"app":"chrome.exe","name":"Google Chrome","title":"\u003cimg src=x onerror=alert(1)\u003e","playing":true,` +
		`"sounding":true,"muted":false,"browser":true,` +
		`"icon":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(small) + `"},` +
		`{"app":"zoom.exe","name":"Zoom","title":"Zoom","playing":false,"sounding":false,"muted":true,"browser":false,"icon":""},` +
		`{"app":"teams.exe","name":"","title":"","playing":false,"sounding":false,"muted":false,"browser":false,"icon":""},` +
		`{"app":"telemost.exe","name":"","title":"","playing":false,"sounding":false,"muted":false,"browser":false,"icon":"max"},` +
		`{"app":"obs64.exe","name":"","title":"","playing":false,"sounding":false,"muted":false,"browser":false,"icon":""}],` +
		`"selected":"zoom.exe","autoSelected":true,"remembered":"zoom.exe","listError":false}`
	if string(raw) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", raw, want)
	}
}

// No apps is an empty list, not null, and the whole computer is the empty name.
func TestPromptServiceSourcesEmpty(t *testing.T) {
	raw, err := json.Marshal(mustSources(t, NewPromptService(&fakePrompt{}, discard())))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"apps":[],"selected":"","autoSelected":false,"remembered":"","listError":false}`; string(raw) != want {
		t.Fatalf("JSON = %s, want %s", raw, want)
	}
}

// A list failure still answers: the whole computer, the remembered app and listError. The page asks every 3 s, so
// the cause is logged once per failure streak.
func TestPromptServiceSourcesListError(t *testing.T) {
	var logs bytes.Buffer
	uc := &fakePrompt{
		sources:    recordingprompt.Sources{Remembered: "zoom.exe"},
		sourcesErr: errors.New(`list sources: enumerate windows: access denied`),
	}
	s := NewPromptService(uc, slog.New(slog.NewTextHandler(&logs, nil)))
	for range 3 {
		raw, err := json.Marshal(mustSources(t, s))
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"apps":[],"selected":"","autoSelected":false,"remembered":"zoom.exe","listError":true}`; string(raw) != want {
			t.Fatalf("JSON = %s, want %s", raw, want)
		}
	}
	if n := strings.Count(logs.String(), "access denied"); n != 1 {
		t.Fatalf("the failure is logged %d times, want once: %q", n, logs.String())
	}

	uc.sourcesErr = nil
	if v := mustSources(t, s); v.ListError {
		t.Fatalf("Sources after recovery = %+v", v)
	}
	uc.sourcesErr = errors.New("access denied")
	mustSources(t, s)
	if n := strings.Count(logs.String(), "access denied"); n != 2 {
		t.Fatalf("a new failure streak must be logged again: %d times", n)
	}
}

// Start passes the chosen app to the use case: "" is the whole computer, a name is normalized, anything else is
// refused with a fixed error before the use case runs.
func TestPromptServiceStartSource(t *testing.T) {
	ctx := context.Background()
	uc := &fakePrompt{view: lessonView("Europe/Moscow")}
	s := NewPromptService(uc, discard())
	for _, app := range []string{"", "zoom.exe", " Chrome.EXE "} {
		if _, err := s.Start(ctx, "prompt-1", app); err != nil {
			t.Fatalf("Start(%q) = %v", app, err)
		}
	}
	want := []values.Source{values.WholeComputer(), mustSource(t, "zoom.exe"), mustSource(t, "chrome.exe")}
	if !slices.Equal(uc.gotSources, want) {
		t.Fatalf("sources %v, want %v", uc.gotSources, want)
	}

	uc.calls = nil
	bad := []string{" ", "zoom", `c:\apps\zoom.exe`, "../zoom.exe", "con.exe", "a\x00.exe", strings.Repeat("a", 300) + ".exe"}
	for _, app := range bad {
		_, err := s.Start(ctx, "prompt-1", app)
		if !errors.Is(err, ErrInvalidSource) || err.Error() != "invalid source" {
			t.Errorf("Start(%q) = %v, want ErrInvalidSource", app, err)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("invalid sources reached the use case: %v", uc.calls)
	}
}

func mustSource(t *testing.T, app string) values.Source {
	t.Helper()
	src, err := values.SourceOf(shared.AppName(app))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func mustSources(t *testing.T, s *PromptService) SourcesView {
	t.Helper()
	v, err := s.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v
}
