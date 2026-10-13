package whispercli

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Typed errors of the adapter (ARCH-32). Resolver errors (for example shared.ErrNotFound) and artifact store errors
// pass through unchanged; a cancelled context yields an error wrapping ctx.Err().
var (
	// ErrConfig means the configuration given to New is unusable.
	ErrConfig = errors.New("whispercli: invalid configuration")

	// ErrInvalidInput means a reference or a resolved path cannot be passed to the process safely.
	ErrInvalidInput = errors.New("whispercli: invalid input")

	// ErrStart means the process could not be started (missing or unusable binary).
	ErrStart = errors.New("whispercli: cannot start whisper-cli")

	// ErrExit means the process ended with a failure; the *RunError carries the exit code and the stderr tail.
	ErrExit = errors.New("whispercli: whisper-cli failed")

	// ErrNoOutput means the process exited with code 0 but wrote no result file. whisper-cli exits with 0 on
	// argument errors (unknown flag or language), so this is a failure, not an empty transcript.
	ErrNoOutput = errors.New("whispercli: whisper-cli wrote no result")

	// ErrOutput means the result file is malformed or violates a limit.
	ErrOutput = errors.New("whispercli: invalid result")

	// ErrOutputTooLarge means the result file exceeds the size limit.
	ErrOutputTooLarge = fmt.Errorf("%w: too large", ErrOutput)

	// ErrTimeout means the process ran longer than Config.Timeout and was killed.
	ErrTimeout = errors.New("whispercli: time limit exceeded")

	// ErrWorkDir means the private run directory could not be created or read.
	ErrWorkDir = fmt.Errorf("whispercli: work directory: %w", shared.ErrStorage)
)

// exitMeanings are the exit codes of whisper-cli v1.9.5 (examples/cli/cli.cpp, see docs/refs/whisper.md).
var exitMeanings = map[int]string{
	1:  "invalid arguments",
	2:  "input audio not found",
	3:  "model could not be loaded",
	4:  "grammar could not be parsed",
	10: "recognition failed",
	11: "audio could not be read or result could not be written",
}

// RunError is a failed run: Err is ErrExit or ErrNoOutput, Code the exit code (-1 if the process was killed by
// the OS) and Stderr the bounded tail of its standard error with the data root and the binary directory redacted.
type RunError struct {
	Err    error
	Code   int
	Stderr string
}

func (e *RunError) Error() string {
	msg := fmt.Sprintf("%v: exit code %d", e.Err, e.Code)
	if m, ok := exitMeanings[e.Code]; ok {
		msg += " (" + m + ")"
	}
	if e.Stderr != "" {
		msg += ": stderr: " + e.Stderr
	}
	return msg
}

func (e *RunError) Unwrap() error { return e.Err }
