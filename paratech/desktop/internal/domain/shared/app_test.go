package shared

import (
	"errors"
	"strings"
	"testing"
)

func TestAppNameValidate(t *testing.T) {
	tests := []struct {
		name    string
		app     AppName
		invalid bool
	}{
		{"browser", "chrome.exe", false},
		{"with a space", "yandex music.exe", false},
		{"non-ascii", "телемост.exe", false},
		{"max length", AppName(strings.Repeat("a", maxAppNameLen-4) + ".exe"), false},
		{"empty", "", true},
		{"only the extension", ".exe", true},
		{"too long", AppName(strings.Repeat("a", maxAppNameLen-3) + ".exe"), true},
		{"upper case", "Chrome.exe", true},
		{"no extension", "chrome", true},
		{"other extension", "chrome.dll", true},
		{"path", `c:\apps\chrome.exe`, true},
		{"slash", "apps/chrome.exe", true},
		{"colon", "c:chrome.exe", true},
		{"control character", "chr\x00ome.exe", true},
		{"newline", "chrome\n.exe", true},
		{"invalid utf-8", "chr\xffome.exe", true},
		{"reserved device name", "con.exe", true},
		{"wildcard", "*.exe", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.app.Validate()
			if tt.invalid != (err != nil) {
				t.Fatalf("Validate(%q) = %v, want invalid %v", tt.app, err, tt.invalid)
			}
			if err != nil && !errors.Is(err, ErrInvalidAppName) {
				t.Fatalf("error %v does not wrap ErrInvalidAppName", err)
			}
		})
	}
}

func TestParseAppName(t *testing.T) {
	tests := []struct {
		in   string
		want AppName
		ok   bool
	}{
		{"chrome.exe", "chrome.exe", true},
		{"  Zoom.EXE ", "zoom.exe", true},
		{"MS-Teams.exe", "ms-teams.exe", true},
		{"", "", false},
		{`C:\Program Files\Zoom\Zoom.exe`, "", false},
	}
	for _, tt := range tests {
		got, err := ParseAppName(tt.in)
		if got != tt.want || tt.ok != (err == nil) {
			t.Errorf("ParseAppName(%q) = %q, %v; want %q, ok %v", tt.in, got, err, tt.want, tt.ok)
		}
	}
	if !AppName("").IsZero() || AppName("a.exe").IsZero() || AppName("a.exe").String() != "a.exe" {
		t.Error("IsZero or String is wrong")
	}
}
