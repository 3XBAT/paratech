package wails

import (
	"io/fs"
	"strings"
	"testing"
)

func TestPopupOrigin(t *testing.T) {
	tests := []struct {
		name       string
		work       rect
		wantX      int
		wantY      int
		w, h, marg int
	}{
		{name: "bottom right of primary", work: rect{0, 0, 1920, 1040}, w: 360, h: 130, marg: 16, wantX: 1544, wantY: 894},
		{name: "work area with offset", work: rect{-1280, 40, 1280, 984}, w: 360, h: 130, marg: 16, wantX: -376, wantY: 878},
		{name: "area smaller than popup is clamped", work: rect{100, 200, 300, 100}, w: 360, h: 130, marg: 16, wantX: 100, wantY: 200},
		{name: "no margin", work: rect{0, 0, 800, 600}, w: 360, h: 130, marg: 0, wantX: 440, wantY: 470},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := popupOrigin(tt.work, tt.w, tt.h, tt.marg)
			if x != tt.wantX || y != tt.wantY {
				t.Fatalf("popupOrigin = (%d, %d), want (%d, %d)", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestWindowSpecs(t *testing.T) {
	popup := popupWindowSpec("LA")
	want := windowSpec{
		Name: popupWindowName, Title: "LA", URL: popupPage, Width: popupWidth, Height: popupHeight,
		Frameless: true, AlwaysOnTop: true, Hidden: true, DisableResize: true,
		NoActivate: true, HiddenOnTaskbar: true, HideOnClose: true,
	}
	if popup != want {
		t.Errorf("popup spec = %+v, want %+v", popup, want)
	}

	main := mainWindowSpec("LA")
	if main.Name != mainWindowName || main.URL != mainPage || main.Hidden || main.Frameless || main.NoActivate || !main.HideOnClose {
		t.Errorf("main spec = %+v: want a visible framed window that hides on close", main)
	}
}

func TestAssetsServePages(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{file: "index.html", want: `href="/main.css"`},
		{file: "main.css", want: "background"},
		{file: strings.TrimPrefix(popupPage, "/"), want: `src="/popup.js"`},
		{file: "popup.js", want: "Window.Hide()"},
		{file: "popup.css", want: "body"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			// The committed layer: a local frontend build would shadow index.html in assets().
			b, err := fs.ReadFile(mustSub(assetsDir), tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tt.want) {
				t.Fatalf("%s does not contain %q", tt.file, tt.want)
			}
		})
	}
}
