//go:build windows

package wails

import (
	"context"
	"io/fs"
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
	called := false
	opts := appOptions(validOptions(), func() { called = true })

	if !opts.DisableDefaultSignalHandler {
		t.Error("the Wails signal handler must be disabled: main owns signals")
	}
	if !opts.Windows.DisableQuitOnLastWindowClosed {
		t.Error("hiding the last window must not quit the tray app")
	}
	if opts.SingleInstance == nil || opts.SingleInstance.UniqueID != "test.la" {
		t.Fatalf("single instance = %+v", opts.SingleInstance)
	}
	opts.SingleInstance.OnSecondInstanceLaunch(application.SecondInstanceData{})
	if !called {
		t.Error("a second instance must bring the main window up")
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
