package values

import (
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name               string
		app                shared.AppName
		description, prodn string
		want               string
	}{
		{"known wins over the description", "browser.exe", "Yandex", "Yandex", "Яндекс Браузер"},
		{"known Comet", "comet.exe", "", "", "Comet"},
		{"known Teams", "ms-teams.exe", "", "", "Microsoft Teams"},
		{"description", "telemost.exe", "Яндекс Телемост", "Telemost", "Яндекс Телемост"},
		{"product when no description", "foo.exe", "  ", "Foo Player", "Foo Player"},
		{"exe name without the extension", "foo.exe", "", "", "foo"},
		{"control characters cleaned", "foo.exe", "Foo\r\n\tPlayer\u202e", "", "Foo Player"},
		{"only format characters fall through", "foo.exe", "\u200b\u202e", "", "foo"},
		{"empty app", "", "", "", ""},
	}
	for _, tt := range tests {
		if got := DisplayName(tt.app, tt.description, tt.prodn); got != tt.want {
			t.Errorf("%s: DisplayName = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestKnownAppName(t *testing.T) {
	for app, want := range map[shared.AppName]string{
		"browser.exe": "Яндекс Браузер", "chrome.exe": "Google Chrome", "msedge.exe": "Microsoft Edge",
		"zoom.exe": "Zoom", "telegram.exe": "Telegram", "teams.exe": "Microsoft Teams",
	} {
		if got, ok := KnownAppName(app); !ok || got != want {
			t.Errorf("KnownAppName(%s) = %q, %v; want %q", app, got, ok, want)
		}
	}
	if _, ok := KnownAppName("notepad.exe"); ok {
		t.Error("notepad.exe is not in the list")
	}
}

func TestCleanAppName(t *testing.T) {
	long := strings.Repeat("я", MaxAppNameBytes) // two bytes each
	tests := []struct{ in, want string }{
		{"  Google   Chrome ", "Google Chrome"},
		{"a\x00b", "a b"},
		{"bad\xffutf8", "badutf8"},
		{"zero\u200bwidth", "zerowidth"},
	}
	for _, tt := range tests {
		if got := CleanAppName(tt.in); got != tt.want {
			t.Errorf("CleanAppName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	got := CleanAppName(long)
	if len(got) > MaxAppNameBytes || !utf8.ValidString(got) || got == "" {
		t.Errorf("long name cut to %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
}
