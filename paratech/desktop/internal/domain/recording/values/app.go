package values

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// MaxAppNameBytes bounds the display name of an app.
const MaxAppNameBytes = 96

// AudioApp is an app the user may record (TASK-059): an app with a visible window, which has or may get an audio
// session. One per executable name.
type AudioApp struct {
	App shared.AppName
	// Name is the readable name of the app (TASK-082): «Яндекс Браузер», «Zoom». Untrusted text (it may come from the
	// executable's version resource), plain text only; never empty in a list (see DisplayName).
	Name string
	// Title is the title of its topmost window: untrusted text, never logged; render it as plain text only.
	Title string
	// PID is the process captured for it: the root of the process tree that owns that window.
	PID uint32
	// Playing reports an active audio session in its process tree: it may play sound now.
	Playing bool
	// Sounding reports a session of its tree whose meter shows sound right now (TASK-082): it really plays.
	Sounding bool
	// Muted reports a session of its tree that is muted or at volume 0 in the Windows volume mixer: then the
	// recording is silent too (ADR-0002 Known limits).
	Muted bool
	// IconPNG is a small PNG of its icon; nil when there is none.
	IconPNG []byte
}

// IsBrowser reports whether app is a known browser. All tabs of a browser are one process tree, so a recording of a
// browser records all its tabs (ADR-0002 Known limits).
func IsBrowser(app shared.AppName) bool {
	switch app {
	case "chrome.exe", "msedge.exe", "firefox.exe", "opera.exe", "brave.exe", "vivaldi.exe",
		"browser.exe", // Yandex Browser
		"comet.exe", "arc.exe", "chromium.exe", "thorium.exe", "librewolf.exe", "waterfox.exe":
		return true
	}
	return false
}

// KnownAppName returns the readable name of a well-known app. Known names win over the version resource, which is
// often missing or too technical («Yandex» for Yandex Browser).
func KnownAppName(app shared.AppName) (string, bool) {
	switch app {
	case "browser.exe":
		return "Яндекс Браузер", true
	case "comet.exe":
		return "Comet", true
	case "chrome.exe":
		return "Google Chrome", true
	case "msedge.exe":
		return "Microsoft Edge", true
	case "firefox.exe":
		return "Firefox", true
	case "opera.exe":
		return "Opera", true
	case "brave.exe":
		return "Brave", true
	case "vivaldi.exe":
		return "Vivaldi", true
	case "arc.exe":
		return "Arc", true
	case "chromium.exe":
		return "Chromium", true
	case "zoom.exe":
		return "Zoom", true
	case "telegram.exe":
		return "Telegram", true
	case "teams.exe", "ms-teams.exe":
		return "Microsoft Teams", true
	case "discord.exe":
		return "Discord", true
	case "skype.exe":
		return "Skype", true
	case "slack.exe":
		return "Slack", true
	case "whatsapp.exe":
		return "WhatsApp", true
	case "spotify.exe":
		return "Spotify", true
	case "vlc.exe":
		return "VLC", true
	}
	return "", false
}

// DisplayName is the readable name of app: the known name, else the file description of its executable, else its
// product name, else the executable name without ".exe". description and product are untrusted text: they are cleaned
// (CleanAppName) and an empty result falls through. Empty only for an empty app.
func DisplayName(app shared.AppName, description, product string) string {
	if name, ok := KnownAppName(app); ok {
		return name
	}
	for _, s := range []string{description, product} {
		if name := CleanAppName(s); name != "" {
			return name
		}
	}
	return strings.TrimSuffix(app.String(), ".exe")
}

// CleanAppName makes untrusted text a safe app name: one line without control and format characters (bidi overrides,
// zero-width ones), spaces collapsed, at most MaxAppNameBytes of valid UTF-8.
func CleanAppName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > MaxAppNameBytes {
		cut := MaxAppNameBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut])
	}
	return s
}

// SortAudioApps orders the picker: apps that sound now first, then those with an active session, then by window
// title and executable name, letter case ignored. The order is stable for equal entries.
func SortAudioApps(apps []AudioApp) {
	slices.SortStableFunc(apps, func(a, b AudioApp) int {
		return cmp.Or(
			first(a.Sounding, b.Sounding),
			first(a.Playing, b.Playing),
			strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			strings.Compare(a.App.String(), b.App.String()),
		)
	})
}

// first orders true before false.
func first(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// Preselection is the source the picker selects first.
type Preselection struct {
	Source Source
	// Auto reports that the source was chosen because it plays sound while the remembered app does not (TASK-082):
	// the picker says so.
	Auto bool
}

// Preselect chooses the source the picker selects first:
//   - nothing (or the whole computer) remembered: the whole computer, which records everything anyway;
//   - the remembered app plays: it;
//   - otherwise, when exactly one other app plays (sounding apps count first; without any, apps with an active
//     session), that app, with Auto set;
//   - otherwise the remembered app when it is in the list, else the whole computer.
func Preselect(remembered shared.AppName, apps []AudioApp) Preselection {
	if remembered.IsZero() || remembered.Validate() != nil {
		return Preselection{Source: WholeComputer()}
	}
	present := false
	var sounding, playing []shared.AppName
	for _, a := range apps {
		if a.App == remembered {
			present = true
			if a.Sounding || a.Playing {
				return Preselection{Source: Source{app: remembered}}
			}
			continue
		}
		if a.Sounding {
			sounding = append(sounding, a.App)
		}
		if a.Playing {
			playing = append(playing, a.App)
		}
	}
	others := sounding
	if len(others) == 0 {
		others = playing
	}
	if len(others) == 1 {
		if s, err := SourceOf(others[0]); err == nil {
			return Preselection{Source: s, Auto: true}
		}
	}
	if present {
		return Preselection{Source: Source{app: remembered}}
	}
	return Preselection{Source: WholeComputer()}
}
