package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// bundlePicker is the save dialog of a test: it answers path.
type bundlePicker string

func (p bundlePicker) PickBundleFile(context.Context, string) (string, error) { return string(p), nil }

// Test of TASK-058 AC-4 (hard rule 4; extends the guards of TASK-057, TASK-062 and TASK-039): with the app, its log
// tee and the page services as New and the UI shell build them at --log-level debug, the SMTP password and the Hub key
// go through the bindings of the Connections page and into synthetic Debug lines of WailsLogger(), the popup binding
// lists a window title and starts a recording, a lesson title, a transcript and a homework text are stored. The
// support bundle saved through its binding then holds none of them in any entry (raw and decoded, UTF-8 and
// UTF-16LE/BE), no audio or database file and no entry name that leaves its folder.
func TestSupportBundleHoldsNoSecrets(t *testing.T) {
	ctx := context.Background()
	const (
		password   = "bundle-smtp-secret-7c1f9a3e"
		hubKey     = "la_bundle_hubkey_4e8d2b6a91"
		lessonText = "Тайная лекция по алгебре 4217"
		transcript = "секретный транскрипт урока 5530"
		homework   = "Домашнее задание секрет 9081: страницы 101-109"
		window     = "Окно с тайным заголовком 3346 - Chrome"
	)
	samples := []string{password, hubKey, lessonText, transcript, homework, window}

	mail := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: testMailUser, Password: password})
	hub := testenv.NewFakeHub()
	hub.SetRemaining(600, 400, 1000)
	hubURL, hubHTTP := testenv.StartFakeHub(t, hub)

	// The app, its tee and the page services as New and the UI shell build them, at --log-level debug.
	dir := t.TempDir()
	a, f, console := uiTeeApp(t, dir, slog.LevelDebug)
	a.env.mailRoots, a.env.hubHTTP = mail.RootCAs(), hubHTTP
	f.power.Set(false) // heavy jobs wait: the test is about what the bundle takes, not the pipeline
	f.apps.Set(values.AudioApp{App: "chrome.exe", Title: window, PID: 4242, Playing: true})
	svc := startUICore(t, a)

	// The secrets go in through the bindings of the Connections page; Wails would log each call at Debug.
	conn := svc.Connections
	wailsLog := a.shell.WailsLogger()
	wailsLog.Debug("Binding call complete:", "method", "SetMailPassword", "args", `["`+password+`"]`)
	wailsLog.Debug("Binding call complete:", "method", "SetHubKey", "args", `["`+hubKey+`"]`)
	must(t)(conn.SetMailPassword(ctx, password))
	must(t)(conn.SaveMailSettings(ctx, wails.MailSettingsInput{
		Recipient: "teacher@school.example", SMTPHost: mail.Host(), SMTPPort: mail.Port(), SMTPSecurity: "starttls",
		SMTPLogin: testMailUser,
	}))
	must(t)(conn.SetHubKey(ctx, hubKey))
	must(t)(conn.SaveHubURL(ctx, hubURL))
	if got, err := conn.CheckMail(ctx); err != nil || !got.OK {
		t.Fatalf("CheckMail = %+v, %v", got, err)
	}
	if got, err := conn.RefreshQuota(ctx); err != nil || !got.OK {
		t.Fatalf("RefreshQuota = %+v, %v", got, err)
	}

	// The popup binding shows the window title and starts a recording that completes with a track. The shell does
	// not run, so the popup window itself cannot follow.
	popup := svc.Prompt
	if _, err := a.core.prompts.OfferAdHoc(ctx); !errors.Is(err, wails.ErrNotRunning) {
		t.Fatalf("OfferAdHoc = %v, want the shell not running", err)
	}
	if src, err := popup.Sources(ctx); err != nil || len(src.Apps) != 1 || src.Apps[0].Title != window {
		t.Fatalf("Sources = %+v, %v", src, err)
	}
	startFromPopup(t, popup, popup.Current().ID)
	info, ok := a.core.recordings.Active()
	if !ok {
		t.Fatal("no recording runs")
	}
	if _, err := f.recorder.Feed(ctx, info.ID, []byte("RIFF fake audio")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.recordings.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}

	// The texts of the lesson, the transcript and the homework.
	start := time.Now().Add(time.Hour)
	l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
		Title: lessonText, Start: start, End: start.Add(time.Hour), TimeZone: "Europe/Moscow",
	}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewLessonRepository(a.core.db).Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	ref, err := shared.NewArtifactRef(shared.ArtifactTranscripts, "t1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.store.Write(ctx, ref, strings.NewReader(transcript)); err != nil {
		t.Fatal(err)
	}
	insertAssignment(t, dir, "a1", homework)
	a.log.Debug("debug line of the test", "note", "a debug record reaches the file")

	// The bundle, through its binding. The save dialog of the shell's service needs the running shell (it fails and
	// logs that), so the page that saves gets a test dialog.
	if got, err := svc.Diagnostics.SaveSupportBundle(ctx); err != nil || got.Status != "failed" {
		t.Fatalf("SaveSupportBundle without the shell = %+v, %v", got, err)
	}
	bundle := filepath.Join(t.TempDir(), "support.zip")
	page := wails.NewDiagnosticsService(wails.DiagnosticsDeps{
		Sound: a.core.ui.Sound, Bundle: a.core.ui.Bundle, Picker: bundlePicker(bundle),
	}, a.log)
	if got, err := page.SaveSupportBundle(ctx); err != nil || got.Status != "saved" || got.Name != "support.zip" {
		t.Fatalf("SaveSupportBundle = %+v, %v", got, err)
	}

	names, findings := scanBundle(t, bundle, samples)
	if len(findings) > 0 {
		t.Fatalf("the bundle leaks:\n%s", strings.Join(findings, "\n"))
	}
	for _, want := range []string{"about.json", "settings.json", "capture.json", "logs/desktop.log"} {
		if !slices.Contains(names, want) {
			t.Fatalf("the bundle lacks %s: %v", want, names)
		}
	}
	logText := entry(t, bundle, "logs/desktop.log")
	if !strings.Contains(logText, "instance lock acquired") || !strings.Contains(logText, "debug line of the test") ||
		!strings.Contains(logText, "recording finished") || !strings.Contains(logText, "ui: choose where to save the support bundle") {
		t.Fatalf("the log of the bundle is not the app's log:\n%s", logText)
	}
	if capture := entry(t, bundle, "capture.json"); !strings.Contains(capture, `"device": "`+testenv.ToneDevice+`"`) {
		t.Fatalf("capture.json:\n%s", capture)
	}
	if settings := entry(t, bundle, "settings.json"); !strings.Contains(settings, `"smtp_login": "***@example.org"`) {
		t.Fatalf("settings.json:\n%s", settings)
	}
	if strings.Contains(console.String(), password) || strings.Contains(console.String(), hubKey) {
		t.Fatal("the console log holds a secret")
	}

	// Negative control: the same scan finds a sample planted in a copy of the bundle, in each encoding, and an audio
	// entry.
	for _, enc := range []func(string) []byte{utf8Bytes, utf16le, utf16be} {
		planted := plant(t, bundle, "logs/desktop.log", enc(password))
		if _, found := scanBundle(t, planted, samples); len(found) == 0 {
			t.Fatal("the scan missed a planted password")
		}
	}
	planted := plantEntry(t, bundle, "recordings/x.WAV", []byte("RIFF"))
	if _, found := scanBundle(t, planted, samples); len(found) == 0 {
		t.Fatal("the scan missed an audio entry")
	}
	planted = plantEntry(t, bundle, "../escape.txt", []byte("x"))
	if _, found := scanBundle(t, planted, samples); len(found) == 0 {
		t.Fatal("the scan missed an entry name that leaves the folder")
	}
}

// startUICore opens the core through the shell, as Run does, and starts the background work; it returns the services
// the shell binds to the pages.
func startUICore(t *testing.T, a *App) wails.Services {
	t.Helper()
	svc, err := a.shell.OpenServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	life, ok := a.claimStart(false)
	if !ok {
		t.Fatal("no lifecycle to start")
	}
	if err := life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func must(t *testing.T) func(any, error) {
	return func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// scanBundle returns the entry names of the zip and every finding: a sample in the whole file, in an entry's raw
// (stored) or decoded bytes, in UTF-8, UTF-16LE or UTF-16BE; a .wav or .db entry; a name with "..", a backslash, a
// drive or a leading slash.
func scanBundle(t *testing.T, file string, samples []string) (names, findings []string) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	encoded := func(s string) [][]byte { return [][]byte{utf8Bytes(s), utf16le(s), utf16be(s)} }
	check := func(where string, data []byte) {
		for i, s := range samples {
			for j, e := range encoded(s) {
				if bytes.Contains(data, e) {
					findings = append(findings, fmt.Sprintf("%s holds sample %d (encoding %d)", where, i, j))
				}
			}
		}
	}
	check("the file", raw)

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		names = append(names, f.Name)
		lower := strings.ToLower(f.Name)
		if path.IsAbs(f.Name) || filepath.IsAbs(f.Name) || strings.ContainsAny(f.Name, `\:`) ||
			slices.Contains(strings.Split(f.Name, "/"), "..") {
			findings = append(findings, fmt.Sprintf("entry name %q leaves its folder", f.Name))
		}
		if strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".partial") || strings.Contains(lower, ".db") {
			findings = append(findings, fmt.Sprintf("entry %q is audio or a database", f.Name))
		}
		check("the name of "+f.Name, []byte(f.Name))
		for _, open := range []func() (io.Reader, error){
			func() (io.Reader, error) { return f.OpenRaw() },
			func() (io.Reader, error) { return f.Open() },
		} {
			r, err := open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			if c, ok := r.(io.Closer); ok {
				_ = c.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			check(f.Name, data)
		}
	}
	return names, findings
}

func entry(t *testing.T, file, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	rc, err := zr.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// plant copies the bundle with extra appended to the entry name.
func plant(t *testing.T, file, name string, extra []byte) string {
	t.Helper()
	return rewrite(t, file, func(n string, data []byte) []byte {
		if n == name {
			return append(data, extra...)
		}
		return data
	}, "", nil)
}

// plantEntry copies the bundle with one more entry, written without the checks of the zipfile adapter.
func plantEntry(t *testing.T, file, name string, data []byte) string {
	t.Helper()
	return rewrite(t, file, func(_ string, d []byte) []byte { return d }, name, data)
}

func rewrite(t *testing.T, file string, edit func(name string, data []byte) []byte, addName string, addData []byte) string {
	t.Helper()
	zr, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	put := func(name string, data []byte) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		put(f.Name, edit(f.Name, data))
	}
	if addName != "" {
		put(addName, addData)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "planted.zip")
	if err := os.WriteFile(p, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func utf8Bytes(s string) []byte { return []byte(s) }

func utf16be(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2*len(units))
	for _, u := range units {
		out = binary.BigEndian.AppendUint16(out, u)
	}
	return out
}
