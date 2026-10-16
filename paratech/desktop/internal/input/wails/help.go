package wails

import (
	"context"
	"log/slog"

	"paratech/desktop/internal/domain/transcription/values"
)

// helpError is an error of the help screens: a fixed Russian text the page shows as is.
type helpError string

func (e helpError) Error() string { return string(e) }

// Errors of the help screens.
const (
	ErrHelpPageUnknown helpError = "Такой страницы справки нет"
	ErrHelpLinkFailed  helpError = "Не удалось открыть страницу справки в браузере"
)

// HelpPageURLs returns the official pages the help screen «Как увидеть события в Apple Calendar» may open, by key
// (docs/refs/apple-calendar.md, "Links for the help screen" and section 3). A new map on every call, so nobody can
// change the set.
func HelpPageURLs() map[string]string {
	return map[string]string{
		"apple-iphone-accounts":       "https://support.apple.com/ru-ru/guide/iphone/ipha0d932e96/ios",
		"apple-mac-internet-accounts": "https://support.apple.com/ru-ru/guide/mac-help/mh35565/mac",
		"apple-mac-calendar-accounts": "https://support.apple.com/ru-ru/guide/calendar/icl4308d6701/mac",
		"apple-mac-subscribe":         "https://support.apple.com/ru-ru/guide/calendar/icl1022/16.0/mac/26",
		"google-apple-sync":           "https://support.google.com/calendar/answer/99358",
		"google-ical-address":         "https://support.google.com/calendar/answer/37648",
		"google-syncselect":           "https://www.google.com/calendar/syncselect",
	}
}

// HelpService is bound to the help screens ("<package path>.HelpService.<Method>"); TASK-052, PRD section 11.3. The
// page sends only a key of HelpPageURLs, never a URL: a closed set of official pages keeps a compromised or buggy page
// from opening an arbitrary link through the app.
type HelpService struct {
	links LinkOpener
	log   *slog.Logger
}

// NewHelpService returns the binding of the help screens; OpenServices checks the opener before calling it.
func NewHelpService(links LinkOpener, log *slog.Logger) *HelpService {
	return &HelpService{links: links, log: log}
}

// OpenHelpPage opens the official page key in the default browser.
func (s *HelpService) OpenHelpPage(ctx context.Context, key string) error {
	raw, ok := HelpPageURLs()[key]
	if !ok {
		return ErrHelpPageUnknown
	}
	u, err := values.ParseSourceURL(raw)
	if err != nil {
		s.log.Error("ui: help page", "err", err)
		return ErrHelpLinkFailed
	}
	if err := s.links.OpenLink(ctx, u); err != nil {
		s.log.Error("ui: open help page in the browser", "host", u.Host(), "err", err)
		return ErrHelpLinkFailed
	}
	return nil
}
