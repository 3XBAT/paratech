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
		name       string
		facts      values.Facts
		status     values.Status
		reason     values.Reason
		exitCode   int
		canStart   bool
		canStartDB bool
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

		// The database container (ADR-0036 section 8).
		{name: "database down, startable", facts: with(func(f *values.Facts) { f.DatabasePort, f.DatabaseStartable = false, true }),
			status: values.StatusNotRunning, reason: values.ReasonDatabaseDown, canStartDB: true},
		{name: "database down, startable, start failed", facts: with(func(f *values.Facts) {
			f.DatabasePort, f.DatabaseStartable, f.DatabaseStartFailed = false, true, true
		}), status: values.StatusNotRunning, reason: values.ReasonDatabaseStartFailed},
		{name: "degraded, startable", facts: with(func(f *values.Facts) { f.Healthz, f.DatabasePort, f.DatabaseStartable = true, false, true }),
			status: values.StatusDegraded, reason: values.ReasonDatabaseDown, canStartDB: true},
		{name: "degraded, startable, start failed", facts: with(func(f *values.Facts) {
			f.Healthz, f.DatabasePort, f.DatabaseStartable, f.DatabaseStartFailed = true, false, true, true
		}), status: values.StatusDegraded, reason: values.ReasonDatabaseStartFailed},
		{name: "degraded, port answers: nothing to start", facts: with(func(f *values.Facts) { f.Healthz, f.DatabaseStartable = true, true }),
			status: values.StatusDegraded, reason: values.ReasonDatabaseDown},
		{name: "docker off, startable: nothing to start", facts: with(func(f *values.Facts) {
			f.DockerEngine, f.DatabasePort, f.DatabaseStartable = false, false, true
		}), status: values.StatusNotRunning, reason: values.ReasonDockerOff},
		{name: "degraded, docker off, startable: nothing to start", facts: values.Facts{Healthz: true, DatabaseStartable: true},
			status: values.StatusDegraded, reason: values.ReasonDockerOff},
		{name: "docker off wins over a failed start", facts: with(func(f *values.Facts) {
			f.DockerEngine, f.DatabasePort, f.DatabaseStartable, f.DatabaseStartFailed = false, false, true, true
		}), status: values.StatusNotRunning, reason: values.ReasonDockerOff},
		{name: "precondition wins over a startable database", facts: with(func(f *values.Facts) {
			f.ProviderKeySet, f.DatabasePort, f.DatabaseStartable = false, false, true
		}), status: values.StatusNotRunning, reason: values.ReasonNoProviderKey},
		{name: "database answers: a failed start is past", facts: with(func(f *values.Facts) { f.DatabaseStartable, f.DatabaseStartFailed = true, true }),
			status: values.StatusNotRunning, canStart: true},
		{name: "startable database down wins over an exit", facts: with(func(f *values.Facts) {
			f.DatabasePort, f.DatabaseStartable, f.Exited, f.ExitCode = false, true, true, 1
		}), status: values.StatusNotRunning, reason: values.ReasonDatabaseDown, canStartDB: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Diagnose(tt.facts)
			if d.Status() != tt.status || d.Reason() != tt.reason || d.ExitCode() != tt.exitCode || d.CanStart() != tt.canStart ||
				d.CanStartDatabase() != tt.canStartDB {
				t.Fatalf("Diagnose = %s/%s/%d canStart=%v canStartDB=%v, want %s/%s/%d canStart=%v canStartDB=%v",
					d.Status(), d.Reason(), d.ExitCode(), d.CanStart(), d.CanStartDatabase(),
					tt.status, tt.reason, tt.exitCode, tt.canStart, tt.canStartDB)
			}
			if d.Reason() != values.ReasonNone && d.NextStep() == values.NextNone {
				t.Fatalf("reason %s has no next step", d.Reason())
			}
		})
	}
}
