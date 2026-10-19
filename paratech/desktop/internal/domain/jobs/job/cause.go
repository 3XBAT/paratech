package job

// Cause says why a job waits or retries when the status alone does not (TASK-083): WAITING_FOR_NETWORK is also used
// for a cloud service that is not set up, so that the job parks without using attempts. Mirrored by the CHECK of
// jobs.cause; a test keeps both in sync.
type Cause string

// Causes of a wait or a retry.
const (
	CauseNone                 Cause = ""                       // the status says it all
	CauseServiceNotConfigured Cause = "service_not_configured" // the cloud service has no address or key
	CauseServiceUnavailable   Cause = "service_unavailable"    // the cloud service did not serve the call for now
)

// Causes returns every cause, CauseNone first.
func Causes() []Cause { return []Cause{CauseNone, CauseServiceNotConfigured, CauseServiceUnavailable} }

// Valid reports whether c is one of Causes.
func (c Cause) Valid() bool {
	for _, k := range Causes() {
		if c == k {
			return true
		}
	}
	return false
}

// keepsCause reports whether a job in the status may carry a cause: only the statuses an unsuccessful attempt ends in.
func (s Status) keepsCause() bool {
	return s == StatusRetryWait || s == StatusFailed || s.IsWaiting()
}
