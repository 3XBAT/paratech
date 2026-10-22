//go:build windows

package wails

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// These tests only build Wails option values; they never call application.New, so no window or tray appears.

func TestWindowOptions(t *testing.T) {
	popup := windowOptions(popupWindowSpec("LA"))
	if !popup.Frameless || !popup.AlwaysOnTop || !popup.Hidden || !popup.DisableResize || !popup.Windows.HiddenOnTaskbar {
		t.Errorf("popup options = %+v: want frameless, always on top, hidden, fixed size, off the taskbar", popup)
	}
	if popup.Width != popupWidth || popup.Height != popupHeight || popup.URL != popupPage {
		t.Errorf("popup geometry/url = %dx%d %q", popup.Width, popup.Height, popup.URL)
	}

	main := windowOptions(mainWindowSpec("LA"))
	if main.Frameless || main.AlwaysOnTop || main.Hidden || main.Windows.ExStyle != 0 {
		t.Errorf("main options = %+v: want a normal window with the Wails default extended style", main)
	}
	if !main.EnableFileDrop || popup.EnableFileDrop {
		t.Errorf("file drop main %v, popup %v: want the main window only", main.EnableFileDrop, popup.EnableFileDrop)
	}

	for _, o := range []application.WebviewWindowOptions{popup, main} {
		if o.BackgroundColour.Alpha != 255 {
			t.Errorf("%s background %+v: want opaque, the zero colour is transparent black", o.Name, o.BackgroundColour)
		}
	}
}

func TestPopupExStyle(t *testing.T) {
	tests := []struct {
		name    string
		spec    windowSpec
		want    []int
		notWant []int
	}{
		{
			name:    "popup",
			spec:    popupWindowSpec("LA"),
			want:    []int{w32.WS_EX_NOACTIVATE, w32.WS_EX_TOPMOST, w32.WS_EX_TOOLWINDOW, w32.WS_EX_CONTROLPARENT},
			notWant: []int{w32.WS_EX_APPWINDOW},
		},
		{
			name:    "on taskbar, not topmost",
			spec:    windowSpec{NoActivate: true},
			want:    []int{w32.WS_EX_NOACTIVATE, w32.WS_EX_APPWINDOW, w32.WS_EX_CONTROLPARENT},
			notWant: []int{w32.WS_EX_TOPMOST, w32.WS_EX_TOOLWINDOW},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := windowOptions(tt.spec).Windows.ExStyle
			for _, bit := range tt.want {
				if got&bit == 0 {
					t.Errorf("ExStyle %#x lacks %#x", got, bit)
				}
			}
			for _, bit := range tt.notWant {
				if got&bit != 0 {
					t.Errorf("ExStyle %#x has %#x", got, bit)
				}
			}
		})
	}
}

func TestAppOptions(t *testing.T) {
	called := 0
	opts := appOptions(validOptions(), func() { called++ })

	if !opts.DisableDefaultSignalHandler {
		t.Error("the Wails signal handler must be disabled: main owns signals")
	}
	if !opts.Windows.DisableQuitOnLastWindowClosed {
		t.Error("hiding the last window must not quit the tray app")
	}
	if opts.Windows.WebviewUserDataPath != "" {
		t.Errorf("no WebviewDataDir must keep the Wails default, got %q", opts.Windows.WebviewUserDataPath)
	}
	if !bytes.Equal(opts.Icon, appIcon()) {
		t.Error("the window icon must be the app icon")
	}
	if opts.SingleInstance == nil || opts.SingleInstance.UniqueID != "test.la" {
		t.Fatalf("single instance = %+v", opts.SingleInstance)
	}
	opts.SingleInstance.OnSecondInstanceLaunch(application.SecondInstanceData{})
	// A launch by a paratech: link (ADR-0013) does the same and nothing else, whatever its arguments say.
	opts.SingleInstance.OnSecondInstanceLaunch(application.SecondInstanceData{
		Args:       []string{`C:\LA\paratech.exe`, `paratech://open/../x" --data-dir "C:\evil`, "--recorder"},
		WorkingDir: `C:\evil`, AdditionalData: map[string]string{"action": "record"},
	})
	if called != 2 {
		t.Errorf("a second instance must bring the main window up, once per launch: %d calls", called)
	}

	// The built popup or the fallback one, whichever assets() serves: the handler must return it unchanged.
	want, err := fs.ReadFile(assets(), strings.TrimPrefix(popupPage, "/"))
	if err != nil || len(want) == 0 {
		t.Fatalf("read %s: %d bytes, %v", popupPage, len(want), err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, popupPage, nil)
	opts.Assets.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
		t.Fatalf("GET %s = %d %q, want the page %q", popupPage, rec.Code, rec.Body.String(), want)
	}
}

// Wails logs binding call arguments at Debug; with a debug-level process logger they would carry the mail password.
func TestAppOptionsLoggerHasNoDebug(t *testing.T) {
	o := validOptions()
	o.Log = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts := appOptions(o, func() {})

	if !o.Log.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("the process logger must keep the user's debug level")
	}
	if opts.Logger == nil || opts.Logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("the Wails logger must not have Debug enabled")
	}
	if !opts.Logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("the Wails logger must keep Info")
	}
}

// The asset server plays the recordings and serves the pages as before.
func TestAppOptionsServeMedia(t *testing.T) {
	opts := appOptions(validOptions(), func() {})
	media := withMedia(&opts, slog.New(slog.DiscardHandler))

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		opts.Assets.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		return rec
	}
	if rec := get(popupPage); rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", popupPage, rec.Code)
	}
	if rec := get("/media/recording/r1"); rec.Code != http.StatusNotFound {
		t.Fatalf("before the core is open: %d, want 404", rec.Code)
	}
	media.setSource(&fakeAudio{data: []byte("RIFF")})
	if rec := get("/media/recording/r1"); rec.Code != http.StatusOK || rec.Body.String() != "RIFF" {
		t.Fatalf("after the core is open: %d %q", rec.Code, rec.Body.String())
	}
}

// The WebView2 folder is the one passed in, so the window keeps its state over the exe rename (ADR-0015).
func TestAppOptionsWebviewDataDir(t *testing.T) {
	o := validOptions()
	o.WebviewDataDir = `C:\Users\Student\AppData\Roaming\learning-assistant.exe`
	if got := appOptions(o, func() {}).Windows.WebviewUserDataPath; got != o.WebviewDataDir {
		t.Fatalf("WebviewUserDataPath = %q, want %q", got, o.WebviewDataDir)
	}
}

// Windows makes the tray (small) and window (large) icons from the embedded ICO the way Wails does.
func TestAppIconLoadsOnWindows(t *testing.T) {
	for name, create := range map[string]func([]byte) (w32.HICON, error){
		"small": w32.CreateSmallHIconFromImage, "large": w32.CreateLargeHIconFromImage,
	} {
		icon, err := create(appIcon())
		if err != nil || icon == 0 {
			t.Fatalf("%s icon: %v, %v", name, icon, err)
		}
		w32.DestroyIcon(icon)
	}
}
