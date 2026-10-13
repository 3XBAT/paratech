//go:build !windows

package whispercli

import "os/exec"

// procGroup is a no-op off Windows: the product targets Windows, and whisper-cli starts no children, so killing
// the process itself (exec.Cmd.Cancel) is enough here.
type procGroup struct{}

func newProcGroup() *procGroup { return &procGroup{} }

func (g *procGroup) attach(int) error { return nil }

func (g *procGroup) kill() {}

func (g *procGroup) close() {}

func setProcAttrs(*exec.Cmd) {}

// checkArgEncoding accepts every string: POSIX passes argv bytes unchanged.
func checkArgEncoding(string) error { return nil }

func binaryNameOK(string) bool { return true }
