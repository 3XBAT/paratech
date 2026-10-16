package wails

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func newHelpFixture() (*HelpService, *fakeLinks, *bytes.Buffer) {
	links, logs := &fakeLinks{}, &bytes.Buffer{}
	return NewHelpService(links, slog.New(slog.NewTextHandler(logs, nil))), links, logs
}

// TestHelpPageURLs pins the closed set of docs/refs/apple-calendar.md: a key the page knows opens exactly its page.
func TestHelpPageURLs(t *testing.T) {
	want := map[string]string{
		"apple-iphone-accounts":       "https://support.apple.com/ru-ru/guide/iphone/ipha0d932e96/ios",
		"apple-mac-internet-accounts": "https://support.apple.com/ru-ru/guide/mac-help/mh35565/mac",
		"apple-mac-calendar-accounts": "https://support.apple.com/ru-ru/guide/calendar/icl4308d6701/mac",
		"apple-mac-subscribe":         "https://support.apple.com/ru-ru/guide/calendar/icl1022/16.0/mac/26",
		"google-apple-sync":           "https://support.google.com/calendar/answer/99358",
		"google-secret-ical":          "https://support.google.com/calendar/answer/37648",
		"google-syncselect":           "https://www.google.com/calendar/syncselect",
	}
	if got := HelpPageURLs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("HelpPageURLs() = %v, want %v", got, want)
	}

	got := HelpPageURLs()
	got["google-syncselect"] = "https://evil.example/x"
	if HelpPageURLs()["google-syncselect"] != want["google-syncselect"] {
		t.Fatal("changing the returned map must not change the set")
	}

	for key, url := range want {
		t.Run(key, func(t *testing.T) {
			s, links, _ := newHelpFixture()
			if err := s.OpenHelpPage(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			if len(links.opened) != 1 || links.opened[0] != url {
				t.Fatalf("opened %v, want [%s]", links.opened, url)
			}
		})
	}
}

// Only a known key opens a page; anything else from the page, a URL included, reaches no browser.
func TestHelpRefusesUnknownKeys(t *testing.T) {
	keys := []string{
		"", "unknown", "Google-Syncselect", " google-syncselect", "google-syncselect ",
		"https://www.google.com/calendar/syncselect", "https://evil.example/x", "file:///C:/Windows/System32/calc.exe",
		"google-syncselect\x00", "../google-syncselect",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			s, links, _ := newHelpFixture()
			if err := s.OpenHelpPage(context.Background(), key); !errors.Is(err, ErrHelpPageUnknown) {
				t.Fatalf("OpenHelpPage(%q) = %v, want ErrHelpPageUnknown", key, err)
			}
			if len(links.opened) != 0 {
				t.Fatalf("opened %v for an unknown key", links.opened)
			}
		})
	}
}

func TestHelpOpenFailure(t *testing.T) {
	s, links, logs := newHelpFixture()
	links.err = errors.New("explorer failed")
	err := s.OpenHelpPage(context.Background(), "google-apple-sync")
	if !errors.Is(err, ErrHelpLinkFailed) {
		t.Fatalf("err = %v, want ErrHelpLinkFailed", err)
	}
	if err.Error() != "Не удалось открыть страницу справки в браузере" {
		t.Errorf("the page shows %q", err.Error())
	}
	if l := logs.String(); !strings.Contains(l, "support.google.com") || !strings.Contains(l, "explorer failed") {
		t.Errorf("the host and the cause must be logged, got %q", l)
	}
}

func TestHelpErrorTexts(t *testing.T) {
	if got := ErrHelpPageUnknown.Error(); got != "Такой страницы справки нет" {
		t.Errorf("ErrHelpPageUnknown = %q", got)
	}
}

// The help screens bind one method: every exported method is callable from the page.
func TestHelpServiceMethods(t *testing.T) {
	typ := reflect.TypeFor[*HelpService]()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "OpenHelpPage" {
		t.Fatalf("HelpService has %d exported methods, want only OpenHelpPage", typ.NumMethod())
	}
}

func TestOpenServicesBindsHelp(t *testing.T) {
	links := &fakeLinks{}
	opts := validOptions()
	opts.OpenCore = func(context.Context) (Core, error) {
		c := validCore()
		c.Links = links
		return c, nil
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.OpenServices(context.Background())
	if err != nil || b.Help == nil {
		t.Fatalf("OpenServices = %+v, %v", b, err)
	}
	if err := b.Help.OpenHelpPage(context.Background(), "google-syncselect"); err != nil {
		t.Fatal(err)
	}
	if len(links.opened) != 1 {
		t.Fatalf("the help service must use the core's link opener, opened %v", links.opened)
	}
}
