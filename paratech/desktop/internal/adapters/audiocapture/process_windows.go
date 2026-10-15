//go:build windows

package audiocapture

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// stillActive is STILL_ACTIVE (= STATUS_PENDING, minwinbase.h): the exit code of a process that runs.
const stillActive = 259

// PlatformProcesses returns the process checker of this OS: it asks Windows whether the process runs.
func PlatformProcesses() ProcessChecker { return windowsProcesses{} }

type windowsProcesses struct{}

// Alive opens the process for a limited query: a process of another user or a protected one (access denied) runs;
// an unknown PID (invalid parameter) does not. A process that exited with the code 259 looks alive; no app does that.
func (windowsProcesses) Alive(pid uint32) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true, nil
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%w: open process %d: %v", ErrProcessCheck, pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false, fmt.Errorf("%w: exit code of process %d: %v", ErrProcessCheck, pid, err)
	}
	return code == stillActive, nil
}
