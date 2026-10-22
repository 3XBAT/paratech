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
	if main.Name != mainWindowName || main.URL != mainPage || main.Hidden || main.Frameless || main.NoActivate || !main.HideOnClose ||
		!main.FileDrop {
		t.Errorf("main spec = %+v: want a visible framed window that hides on close and takes dropped files", main)
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

func TestPopupFitHeight(t *testing.T) {
	fullHD := rect{0, 0, 1920, 1040}
	tests := []struct {
		name string
		want int
		work rect
		got  int
	}{
		{name: "fits the normal size", want: 250, work: fullHD, got: popupHeight},
		{name: "exactly normal", want: popupHeight, work: fullHD, got: popupHeight},
		{name: "grows for an error", want: 372, work: fullHD, got: 372},
		{name: "capped at the max", want: 900, work: fullHD, got: popupMaxHeight},
		{name: "zero or negative is normal", want: -5, work: fullHD, got: popupHeight},
		{name: "small work area caps below the max", want: 900, work: rect{0, 0, 1280, 400}, got: 400 - 2*popupMargin},
		{name: "tiny work area keeps the normal size", want: 900, work: rect{0, 0, 800, 200}, got: popupHeight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := popupFitHeight(tt.want, tt.work, popupMargin); got != tt.got {
				t.Fatalf("popupFitHeight(%d) = %d, want %d", tt.want, got, tt.got)
			}
		})
	}
}

func TestPopupBounds(t *testing.T) {
	tests := []struct {
		name string
		work rect
		want int
		got  rect
	}{
		{name: "normal", work: rect{0, 0, 1920, 1040}, want: 0, got: rect{1544, 724, popupWidth, popupHeight}},
		{name: "grown keeps the bottom edge", work: rect{0, 0, 1920, 1040}, want: 400, got: rect{1544, 624, popupWidth, 400}},
		{name: "150 % of full HD", work: rect{0, 0, 1280, 693}, want: 1000, got: rect{904, 237, popupWidth, popupMaxHeight}},
		{name: "secondary area with offset", work: rect{-1280, 40, 1280, 984}, want: 380, got: rect{-376, 628, popupWidth, 380}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := popupBounds(tt.work, tt.want, popupMargin)
			if b != tt.got {
				t.Fatalf("popupBounds = %+v, want %+v", b, tt.got)
			}
			if b.X < tt.work.X || b.Y < tt.work.Y || b.X+b.Width > tt.work.X+tt.work.Width || b.Y+b.Height > tt.work.Y+tt.work.Height {
				t.Fatalf("popupBounds = %+v leaves the work area %+v", b, tt.work)
			}
		})
	}
}
