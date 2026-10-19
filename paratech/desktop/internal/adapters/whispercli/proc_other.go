//go:build !windows

package whispercli

import (
	"os/exec"
	"strings"
)

// procGroup is a no-op off Windows: the product targets Windows, and whisper-cli starts no children, so killing
// the process itself (exec.Cmd.Cancel) is enough here. There is no CPU cap either: the load modes only set threads.
type procGroup struct{}

func newProcGroup(int) (*procGroup, error) { return &procGroup{}, nil }

func (g *procGroup) attach(int) error { return nil }

func (g *procGroup) kill() {}

func (g *procGroup) close() {}

// setProcAttrs ignores the priority off Windows.
func setProcAttrs(*exec.Cmd, bool) {}

// checkArgEncoding accepts every string: POSIX passes argv bytes unchanged.
func checkArgEncoding(string) error { return nil }

func binaryNameOK(string) bool { return true }

// processDone is unknown here; Process.Kill reports a reaped process as os.ErrProcessDone.
func processDone(int) bool { return false }

// decodeStderr keeps the bytes: the CLI prints the argv bytes it got, which are UTF-8 here.
func decodeStderr(b []byte) string { return strings.ToValidUTF8(string(b), "?") }

// nativeForm has nothing to add: paths are echoed as given.
func nativeForm(string) (string, bool) { return "", false }
