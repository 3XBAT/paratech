package localhub

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/localhub/values"
)

// HubProbe looks at the local Hub and what it needs (ADR-0036 section 4). Implemented by the hubprocess adapter.
type HubProbe interface {
	// Facts returns what the probe finds for t: hub.exe next to the exe, /healthz (1 s timeout) and /readyz, the Docker
	// engine pipe, the database port of the stored database URL (TCP, 500 ms), who listens on the port and the last
	// exit of the Hub this desktop started. The recorded exit belongs to the latest spawn: after a Start, Exited is
	// false until that new process ends (the Service relies on it to see a respawned Hub's own exit). The secret flags
	// may be left false: the Service sets them from SecretSlots. DatabaseStartable is the probe's (ADR-0036 section 8:
	// compose file next to the exe, Docker CLI at its fixed place, loopback database host); DatabaseStartFailed is
	// left false: the Service sets it from its memory. Facts never carry a secret value.
	Facts(ctx context.Context, t values.Target) (values.Facts, error)
}

// DatabaseStarter starts the Hub's database container (ADR-0036 section 8). Implemented by the hubprocess adapter.
type DatabaseStarter interface {
	// StartDatabase runs the compose start of the postgres service and returns when the command ends: nil when it
	// succeeded. The Service calls it only while the Docker engine runs and bounds it by ctx; the app never starts
	// Docker Desktop itself.
	StartDatabase(ctx context.Context) error
}

// HubLauncher starts and stops the local hub.exe and issues access keys with it (ADR-0036 section 5). Implemented by
// the hubprocess adapter, which reads the secrets from the store it is given in app.
type HubLauncher interface {
	// Start spawns hub.exe listening on t.ListenAddr with settings and the stored secrets in its environment, and
	// returns once the process runs; its exit is reported by HubProbe.Facts. A successful Start forgets the exit of
	// the previous process.
	Start(ctx context.Context, t values.Target, settings values.ServerSettings) error

	// Stop ends the hub.exe that listens on t, only if its image is ours (TASK-390).
	Stop(ctx context.Context, t values.Target) error

	// IssueKey creates a Hub access key and stores it in the desktop's key slot, never returning it (TASK-390).
	IssueKey(ctx context.Context) error
}

// SecretSlots is the write-only store of the Hub secrets (ADR-0036 section 3). Implemented by the wincred adapter.
type SecretSlots interface {
	// IsSet reports whether the slot holds a value.
	IsSet(ctx context.Context, name values.SecretName) (bool, error)
	// Set stores value in the slot, replacing the old one.
	Set(ctx context.Context, name values.SecretName, value string) error
	// Generate stores a fresh random value (the seal keys: "1:" + 32 random bytes as hex).
	Generate(ctx context.Context, name values.SecretName) error
}

// SettingsFile keeps the non-secret Hub settings in <DataDir>/hub/hub-server.json. Implemented by the hubserverconf
// adapter.
type SettingsFile interface {
	// Load returns the saved settings; the zero ServerSettings (every Hub default) when nothing is saved.
	Load(ctx context.Context) (values.ServerSettings, error)
	// Save replaces the saved settings.
	Save(ctx context.Context, s values.ServerSettings) error
}

// HubURL reads the desktop's «Адрес сервера» (a bridge over the desktop settings, built in app).
type HubURL interface {
	// Current returns the server address as the owner set it.
	Current(ctx context.Context) (string, error)
}

// Clock is the time source of the readiness wait (ARCH-24). Implemented by the clock adapter, a fake in tests.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error.
	Sleep(ctx context.Context, d time.Duration) error
}
