package values

import (
	"cmp"
	"slices"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// AudioApp is an app the user may record (TASK-059): an app with a visible window, which has or may get an audio
// session. One per executable name.
type AudioApp struct {
	App shared.AppName
	// Title is the title of its topmost window: untrusted text, never logged; render it as plain text only.
	Title string
	// PID is the process captured for it: the root of the process tree that owns that window.
	PID uint32
	// Playing reports an active audio session in its process tree: it plays sound now.
	Playing bool
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
		"browser.exe": // Yandex Browser
		return true
	}
	return false
}

// SortAudioApps orders the picker: apps that play now first, then by window title and executable name, letter case
// ignored. The order is stable for equal entries.
func SortAudioApps(apps []AudioApp) {
	slices.SortStableFunc(apps, func(a, b AudioApp) int {
		if a.Playing != b.Playing {
			if a.Playing {
				return -1
			}
			return 1
		}
		return cmp.Or(
			strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			strings.Compare(a.App.String(), b.App.String()),
		)
	})
}

// Preselect is the source the picker selects first: the remembered app when it is in the list, else the whole
// computer.
func Preselect(remembered shared.AppName, apps []AudioApp) Source {
	if remembered.IsZero() || remembered.Validate() != nil {
		return WholeComputer()
	}
	for _, a := range apps {
		if a.App == remembered {
			return Source{app: remembered}
		}
	}
	return WholeComputer()
}
