//go:build !windows

package audiocapture

import "fmt"

// PlatformProcesses returns the process checker of this OS: none outside Windows, where no INCLUDE target exists.
func PlatformProcesses() ProcessChecker { return noProcesses{} }

type noProcesses struct{}

func (noProcesses) Alive(uint32) (bool, error) {
	return false, fmt.Errorf("%w: %w", ErrProcessCheck, ErrUnavailable)
}
