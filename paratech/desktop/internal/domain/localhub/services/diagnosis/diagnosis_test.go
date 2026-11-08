package diagnosis

import (
	"testing"

	"paratech/desktop/internal/domain/localhub/values"
)

// ready holds every precondition of a start; nothing answers and nothing listens.
func ready() values.Facts {
	return values.Facts{HubExe: true, ProviderKeySet: true, DatabaseURLSet: true, DockerEngine: true, DatabasePort: true}
}

func TestDiagnose(t *testing.T) {
	with := func(change func(*values.Facts)) values.Facts {
		f := ready()
		change(&f)
		return f
	}
	tests := []struct {
		name     string
		facts    values.Facts
		status   values.Status
		reason   values.Reason
		exitCode int
		canStart bool
	}{
		{name: "healthz and readyz ok", facts: with(func(f *values.Facts) { f.Healthz, f.Readyz = true, true }), status: values.StatusRunning},
		{name: "a hand-run hub counts with nothing set", facts: values.Facts{Healthz: true, Readyz: true}, status: values.StatusRunning},
		{name: "healthz without readyz, docker off", facts: values.Facts{Healthz: true}, status: values.StatusDegraded, reason: values.ReasonDockerOff},
		{name: "healthz without readyz, docker on", facts: with(func(f *values.Facts) { f.Healthz, f.DatabasePort = true, false }), status: values.StatusDegraded, reason: values.ReasonDatabaseDown},
		{name: "running wins over an old exit", facts: with(func(f *values.Facts) { f.Healthz, f.Readyz, f.Exited, f.ExitCode = true, true, true, 1 }), status: values.StatusRunning},

		{name: "no hub.exe first", facts: values.Facts{}, status: values.StatusNotRunning, reason: values.ReasonNoHubExe},
		{name: "no provider key", facts: values.Facts{HubExe: true}, status: values.StatusNotRunning, reason: values.ReasonNoProviderKey},
		{name: "no database url", facts: values.Facts{HubExe: true, ProviderKeySet: true}, status: values.StatusNotRunning, reason: values.ReasonNoDatabaseURL},
		{name: "docker off", facts: with(func(f *values.Facts) { f.DockerEngine, f.DatabasePort = false, false }), status: values.StatusNotRunning, reason: values.ReasonDockerOff},
		{name: "database down", facts: with(func(f *values.Facts) { f.DatabasePort = false }), status: values.StatusNotRunning, reason: values.ReasonDatabaseDown},
		{name: "precondition wins over port busy", facts: with(func(f *values.Facts) { f.HubExe, f.Listener = false, values.ListenerForeign }), status: values.StatusNotRunning, reason: values.ReasonNoHubExe},
		{name: "port busy", facts: with(func(f *values.Facts) { f.Listener = values.ListenerForeign }), status: values.StatusNotRunning, reason: values.ReasonPortBusy},
		{name: "ours listens, not answering yet", facts: with(func(f *values.Facts) { f.Listener = values.ListenerOurs }), status: values.StatusStarting},
		{name: "config error", facts: with(func(f *values.Facts) { f.Exited, f.ExitCode = true, 2 }), status: values.StatusNotRunning, reason: values.ReasonConfigError, exitCode: 2},
		{name: "exited", facts: with(func(f *values.Facts) { f.Exited, f.ExitCode = true, 1 }), status: values.StatusNotRunning, reason: values.ReasonExited, exitCode: 1},
		{name: "exited with 0", facts: with(func(f *values.Facts) { f.Exited = true }), status: values.StatusNotRunning, reason: values.ReasonExited},
		{name: "database down wins over the exit it caused", facts: with(func(f *values.Facts) { f.DatabasePort, f.Exited, f.ExitCode = false, true, 1 }), status: values.StatusNotRunning, reason: values.ReasonDatabaseDown},
		{name: "ready to start", facts: ready(), status: values.StatusNotRunning, canStart: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Diagnose(tt.facts)
			if d.Status() != tt.status || d.Reason() != tt.reason || d.ExitCode() != tt.exitCode || d.CanStart() != tt.canStart {
				t.Fatalf("Diagnose = %s/%s/%d canStart=%v, want %s/%s/%d canStart=%v",
					d.Status(), d.Reason(), d.ExitCode(), d.CanStart(), tt.status, tt.reason, tt.exitCode, tt.canStart)
			}
			if d.Reason() != values.ReasonNone && d.NextStep() == values.NextNone {
				t.Fatalf("reason %s has no next step", d.Reason())
			}
		})
	}
}
