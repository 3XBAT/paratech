package values

// Status is the state of the local Hub the owner sees (ADR-0036 section 7). A closed list.
type Status string

// Statuses.
const (
	StatusRemote     Status = "remote"      // «Адрес сервера» is not local: nothing to start
	StatusRunning    Status = "running"     // /healthz and /readyz answer
	StatusStarting   Status = "starting"    // started, not answering yet
	StatusDegraded   Status = "degraded"    // /healthz answers, the database does not
	StatusNotRunning Status = "not_running" // nothing answers
)

// Reason says why the Hub is not running or degraded. A closed list (ADR-0036 section 7).
type Reason string

// Reasons.
const (
	ReasonNone          Reason = ""
	ReasonNoProviderKey Reason = "no_provider_key"
	ReasonNoDatabaseURL Reason = "no_database_url"
	ReasonNoHubExe      Reason = "no_hub_exe"
	ReasonDockerOff     Reason = "docker_off"
	ReasonDatabaseDown  Reason = "database_down"
	ReasonPortBusy      Reason = "port_busy"
	ReasonExited        Reason = "exited"
	ReasonConfigError   Reason = "config_error"
)

// Reasons returns every reason except ReasonNone, in a fixed order.
func Reasons() []Reason {
	return []Reason{
		ReasonNoProviderKey, ReasonNoDatabaseURL, ReasonNoHubExe, ReasonDockerOff, ReasonDatabaseDown,
		ReasonPortBusy, ReasonExited, ReasonConfigError,
	}
}

// NextStep is what the owner does about a reason: a stable code the UI turns into text and a link or action
// (ADR-0036 section 7: every reason has a next step).
type NextStep string

// Next steps.
const (
	NextNone             NextStep = ""
	NextEnterProviderKey NextStep = "enter_provider_key" // «Подключения → Hub на этом компьютере (бета)»
	NextEnterDatabaseURL NextStep = "enter_database_url" // same block
	NextRebuildBeta      NextStep = "rebuild_beta"       // hub.exe is built next to the exe by the beta build
	NextStartDocker      NextStep = "start_docker"       // the owner starts Docker Desktop; the app never does
	NextStartDatabase    NextStep = "start_database"     // the owner starts the postgres container
	NextFreePort         NextStep = "free_port"          // another program holds the port
	NextOpenLogs         NextStep = "open_logs"          // the Hub ended: details in the log
	NextCheckSettings    NextStep = "check_settings"     // the Hub refused its settings (exit 2): settings and log
)

// NextStep returns the next step of r; NextNone for ReasonNone and an unknown reason.
func (r Reason) NextStep() NextStep {
	switch r {
	case ReasonNoProviderKey:
		return NextEnterProviderKey
	case ReasonNoDatabaseURL:
		return NextEnterDatabaseURL
	case ReasonNoHubExe:
		return NextRebuildBeta
	case ReasonDockerOff:
		return NextStartDocker
	case ReasonDatabaseDown:
		return NextStartDatabase
	case ReasonPortBusy:
		return NextFreePort
	case ReasonExited:
		return NextOpenLogs
	case ReasonConfigError:
		return NextCheckSettings
	}
	return NextNone
}

// ExitConfigError is the exit code of a Hub that refused its configuration (config.MustLoad).
const ExitConfigError = 2

// ExitNotStarted is the exit code reported when the Hub process could not be started at all (no process, so no
// real code): the UI says «не удалось запустить» for it, never «код -1».
const ExitNotStarted = -1

// Diagnosis is a Status with its reason and, for an ended Hub, its exit code. The zero value is not a diagnosis.
type Diagnosis struct {
	status   Status
	reason   Reason
	exitCode int
}

// Remote is the diagnosis of a desktop that uses a remote Hub.
func Remote() Diagnosis { return Diagnosis{status: StatusRemote} }

// Running is the diagnosis of a Hub that answers /healthz and /readyz.
func Running() Diagnosis { return Diagnosis{status: StatusRunning} }

// Starting is the diagnosis of a Hub that was started and does not answer yet.
func Starting() Diagnosis { return Diagnosis{status: StatusStarting} }

// Degraded is the diagnosis of a Hub that answers /healthz but not /readyz, for reason r.
func Degraded(r Reason) Diagnosis { return Diagnosis{status: StatusDegraded, reason: r} }

// NotRunning is the diagnosis of a Hub that does not answer, for reason r.
func NotRunning(r Reason) Diagnosis { return Diagnosis{status: StatusNotRunning, reason: r} }

// Exited is the diagnosis of a Hub that ended with code: ReasonConfigError for ExitConfigError, else ReasonExited.
func Exited(code int) Diagnosis {
	r := ReasonExited
	if code == ExitConfigError {
		r = ReasonConfigError
	}
	return Diagnosis{status: StatusNotRunning, reason: r, exitCode: code}
}

// ReadyToStart is the diagnosis of a Hub that does not answer while every precondition of a start holds.
func ReadyToStart() Diagnosis { return Diagnosis{status: StatusNotRunning} }

// Status returns the status.
func (d Diagnosis) Status() Status { return d.status }

// Reason returns the reason; ReasonNone for running, starting, remote and ReadyToStart.
func (d Diagnosis) Reason() Reason { return d.reason }

// NextStep returns the next step of the reason.
func (d Diagnosis) NextStep() NextStep { return d.reason.NextStep() }

// ExitCode returns the exit code of an ended Hub (ReasonExited, ReasonConfigError); 0 otherwise.
func (d Diagnosis) ExitCode() int { return d.exitCode }

// CanStart reports whether the Hub may be started: nothing answers and nothing stands in the way.
func (d Diagnosis) CanStart() bool { return d.status == StatusNotRunning && d.reason == ReasonNone }

// IsZero reports whether d is not a diagnosis.
func (d Diagnosis) IsZero() bool { return d.status == "" }
