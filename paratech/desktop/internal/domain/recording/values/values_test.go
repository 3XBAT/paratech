package values

import (
	"errors"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestSource(t *testing.T) {
	tests := []struct {
		name  string
		app   shared.AppName
		whole bool
		str   string
		err   bool
	}{
		{name: "empty is the whole computer", app: "", whole: true, str: "whole computer"},
		{name: "an app", app: "zoom.exe", str: "app zoom.exe"},
		{name: "not normalized", app: "Zoom.exe", err: true},
		{name: "a path", app: `c:\zoom.exe`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := SourceOf(tt.app)
			if tt.err {
				if !errors.Is(err, ErrInvalidSource) || !errors.Is(err, shared.ErrInvalidAppName) || s != (Source{}) {
					t.Fatalf("SourceOf(%q) = %v, %v; want ErrInvalidSource", tt.app, s, err)
				}
				return
			}
			if err != nil || s.IsWholeComputer() != tt.whole || s.App() != tt.app || s.String() != tt.str {
				t.Fatalf("SourceOf(%q) = %+v (%s), %v", tt.app, s, s, err)
			}
		})
	}
	if WholeComputer() != (Source{}) || !WholeComputer().IsWholeComputer() {
		t.Fatal("the zero source must be the whole computer")
	}
}

func TestIsBrowser(t *testing.T) {
	for _, app := range []shared.AppName{"chrome.exe", "msedge.exe", "firefox.exe", "browser.exe", "opera.exe", "brave.exe", "vivaldi.exe"} {
		if !IsBrowser(app) {
			t.Errorf("%s is a browser", app)
		}
	}
	for _, app := range []shared.AppName{"zoom.exe", "ms-teams.exe", "telemost.exe", "", "Chrome.exe"} {
		if IsBrowser(app) {
			t.Errorf("%s is not a browser", app)
		}
	}
}

func TestSortAudioApps(t *testing.T) {
	apps := []AudioApp{
		{App: "zoom.exe", Title: "Zoom"},
		{App: "msedge.exe", Title: "lecture", Playing: true},
		{App: "b.exe", Title: "same"},
		{App: "a.exe", Title: "Same"},
		{App: "chrome.exe", Title: "Another tab", Playing: true},
		{App: "notepad.exe", Title: ""},
	}
	SortAudioApps(apps)
	var got []shared.AppName
	for _, a := range apps {
		got = append(got, a.App)
	}
	want := []shared.AppName{"chrome.exe", "msedge.exe", "notepad.exe", "a.exe", "b.exe", "zoom.exe"}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestPreselect(t *testing.T) {
	apps := []AudioApp{{App: "zoom.exe"}, {App: "chrome.exe"}}
	tests := []struct {
		name       string
		remembered shared.AppName
		want       Source
	}{
		{"remembered app is open", "chrome.exe", Source{app: "chrome.exe"}},
		{"remembered app is not open", "ms-teams.exe", WholeComputer()},
		{"nothing remembered", "", WholeComputer()},
		{"malformed name", "Chrome.exe", WholeComputer()},
	}
	for _, tt := range tests {
		if got := Preselect(tt.remembered, apps); got != tt.want {
			t.Errorf("%s: Preselect = %v, want %v", tt.name, got, tt.want)
		}
	}
	if got := Preselect("chrome.exe", nil); !got.IsWholeComputer() {
		t.Errorf("empty list: Preselect = %v", got)
	}
}
