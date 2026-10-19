package recordingsession

import "errors"

// Errors of the recording session (ARCH-18). A refused start, or a refused Recover, because a recording is in progress
// is limits.ErrRecordingInProgress.
var (
	// ErrInvalidConfig is returned by New for missing ports or unusable settings.
	ErrInvalidConfig = errors.New("invalid recording session configuration")

	// ErrClosed is returned by Start and Recover after Close.
	ErrClosed = errors.New("recording service is closed")

	// ErrRecovering is returned by Recover while another Recover runs, by Start while Recover marks the interrupted
	// recordings, and by Delete of a recording that the running Recover has not finished yet.
	ErrRecovering = errors.New("interrupted recordings are being recovered")

	// ErrStartFailed means the capture could not start; the recording is FAILED.
	ErrStartFailed = errors.New("recording could not start")

	// ErrStartAborted means a stop, a cancelled request or a shutdown ended the start before the capture ran; the
	// recording is FAILED.
	ErrStartAborted = errors.New("recording start was aborted")

	// ErrNotRunning means Stop found an unfinished recording that no session of this process captures: it was
	// interrupted and waits for recovery.
	ErrNotRunning = errors.New("recording is not running in this session")

	// ErrSwitchRefused means the source of the recording cannot be switched now: it is not being captured any more
	// (stopping or finished), or the capture refused (TASK-082). The recording goes on as it was.
	ErrSwitchRefused = errors.New("the source of the recording cannot be switched now")

	// ErrAbandoned means the session ended before the recording was finished: a forced shutdown, or the recording
	// was changed elsewhere. The stored state is left for the recovery after a restart.
	ErrAbandoned = errors.New("recording session ended before the recording was finished")
)

// Reasons a context of the session is cancelled; they are context causes, never returned to callers.
var (
	errStopRequested   = errors.New("stop requested")
	errShutdown        = errors.New("recording service is shutting down")
	errDetachedTimeout = errors.New("detached call timed out")
)
