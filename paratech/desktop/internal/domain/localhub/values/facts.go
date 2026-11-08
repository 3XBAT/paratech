package values

// Listener is who listens on the Target port, as far as the probe can tell.
type Listener int

// Listeners. ListenerNone also covers a probe that cannot tell.
const (
	ListenerNone    Listener = iota // nobody, or unknown
	ListenerOurs                    // our hub.exe (the image next to the desktop exe)
	ListenerForeign                 // another program
)

// Facts is what a probe of the local Hub found (ADR-0036 sections 2 and 4). It is plain data: flags only, never a
// secret value, so it may be logged and shown.
type Facts struct {
	// HubExe: hub.exe is a regular file next to the desktop executable.
	HubExe bool
	// ProviderKeySet and DatabaseURLSet: the secret slots hold a value.
	ProviderKeySet bool
	DatabaseURLSet bool
	// Healthz: GET /healthz answered ok; Readyz: GET /readyz answered ok (the Hub reaches its database).
	Healthz bool
	Readyz  bool
	// DockerEngine: the Docker engine pipe exists (existence only).
	DockerEngine bool
	// DatabasePort: the host and port of the database URL accept TCP.
	DatabasePort bool
	// Listener: who listens on the Target port.
	Listener Listener
	// Exited and ExitCode: the Hub this desktop started last has ended, with that code.
	Exited   bool
	ExitCode int
}
