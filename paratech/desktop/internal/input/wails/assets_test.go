package wails

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestLayeredPrefersTheBuiltApp(t *testing.T) {
	built := fstest.MapFS{"index.html": {Data: []byte("react")}, "assets/index.js": {Data: []byte("js")}}
	committed := fstest.MapFS{"index.html": {Data: []byte("fallback")}, "popup.html": {Data: []byte("popup")}}

	tests := []struct {
		name  string
		fsys  fs.FS
		file  string
		want  string
		found bool
	}{
		{name: "built main page wins", fsys: layered{built, committed}, file: "index.html", want: "react", found: true},
		{name: "built asset", fsys: layered{built, committed}, file: "assets/index.js", want: "js", found: true},
		{name: "popup from committed pages", fsys: layered{built, committed}, file: "popup.html", want: "popup", found: true},
		{name: "fallback without a build", fsys: layered{fstest.MapFS{}, committed}, file: "index.html", want: "fallback", found: true},
		{name: "missing everywhere", fsys: layered{built, committed}, file: "nope.js"},
		{name: "dotfile refused", fsys: layered{fstest.MapFS{".env": {Data: []byte("x")}}}, file: ".env"},
		{name: "dot directory refused", fsys: layered{fstest.MapFS{".git/config": {Data: []byte("x")}}}, file: ".git/config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := fs.ReadFile(tt.fsys, tt.file)
			if !tt.found {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("err = %v, want ErrNotExist", err)
				}
				return
			}
			if err != nil || string(b) != tt.want {
				t.Fatalf("ReadFile = %q, %v; want %q", b, err, tt.want)
			}
		})
	}
}

func TestAssetsHaveMainAndPopupPages(t *testing.T) {
	// Holds with and without a local frontend build.
	for _, file := range []string{"index.html", "popup.html", "popup.js"} {
		if _, err := fs.Stat(assets(), file); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
}
