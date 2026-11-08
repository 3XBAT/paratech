package localhub

import (
	"log/slog"

	"paratech/desktop/internal/domain/localhub/values"
)

// Deps are the ports of the Service; Log is optional.
type Deps struct {
	Probe    HubProbe
	Launcher HubLauncher
	Secrets  SecretSlots
	Settings SettingsFile
	URL      HubURL
	Clock    Clock
	Log      *slog.Logger
}

// Status is what the diagnostics block shows (ADR-0036 section 7).
type Status struct {
	State    values.Status
	Reason   values.Reason
	Next     values.NextStep
	ExitCode int    // of an ended Hub (exited, config_error); values.ExitNotStarted when it could not be started
	Port     int    // the local port; 0 for a remote Hub
	URL      string // the remote server address, or the local Hub's base URL
}

// Settings are the non-secret Hub settings of the form; zero values mean the Hub default.
type Settings struct {
	BaseURL    string
	Auth       values.AuthMode
	QuotaLimit int64
}
