package diagnosis

import "paratech/desktop/internal/domain/localhub/values"

// Diagnose reads f in this order and stops at the first match (ADR-0036 sections 4 and 8):
//  1. /healthz ok: running when /readyz is ok, else degraded (docker_off without the engine, else the database rule
//     below when its port does not answer, else database_down). Whoever started that Hub, the owner by hand included.
//  2. The preconditions of a start, in order: hub.exe, provider key, database URL, Docker engine, database port.
//  3. Another program listens on the port: port_busy.
//  4. Our hub.exe listens but does not answer yet: starting.
//  5. The Hub this desktop started ended: config_error for exit 2, else exited. It is not started again by itself.
//  6. Otherwise values.ReadyToStart: the Hub may be started.
//
// The database rule (engine on, port silent): database_start_failed after a failed start of the container; else
// database_down, which the app may resolve itself (CanStartDatabase) when the container is startable.
func Diagnose(f values.Facts) values.Diagnosis {
	if f.Healthz {
		switch {
		case f.Readyz:
			return values.Running()
		case !f.DockerEngine:
			return values.Degraded(values.ReasonDockerOff)
		case !f.DatabasePort:
			return databaseDown(f, true)
		default:
			return values.Degraded(values.ReasonDatabaseDown)
		}
	}

	switch {
	case !f.HubExe:
		return values.NotRunning(values.ReasonNoHubExe)
	case !f.ProviderKeySet:
		return values.NotRunning(values.ReasonNoProviderKey)
	case !f.DatabaseURLSet:
		return values.NotRunning(values.ReasonNoDatabaseURL)
	case !f.DockerEngine:
		return values.NotRunning(values.ReasonDockerOff)
	case !f.DatabasePort:
		return databaseDown(f, false)
	case f.Listener == values.ListenerForeign:
		return values.NotRunning(values.ReasonPortBusy)
	case f.Listener == values.ListenerOurs:
		return values.Starting()
	case f.Exited:
		return values.Exited(f.ExitCode)
	}
	return values.ReadyToStart()
}

// databaseDown is the database rule for a running engine and a silent database port.
func databaseDown(f values.Facts, hubAnswers bool) values.Diagnosis {
	if f.DatabaseStartFailed {
		if hubAnswers {
			return values.Degraded(values.ReasonDatabaseStartFailed)
		}
		return values.NotRunning(values.ReasonDatabaseStartFailed)
	}
	return values.DatabaseDown(hubAnswers, f.DatabaseStartable)
}
