//go:build !windows

package audiocapture

import "os/exec"

// hideWindow has nothing to hide outside Windows.
func hideWindow(*exec.Cmd) {}
