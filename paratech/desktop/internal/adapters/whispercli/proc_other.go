//go:build !windows

package whispercli

import (
	"os/exec"
	"strings"
)

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

// processDone is unknown here; Process.Kill reports a reaped process as os.ErrProcessDone.
func processDone(int) bool { return false }

// decodeStderr keeps the bytes: the CLI prints the argv bytes it got, which are UTF-8 here.
func decodeStderr(b []byte) string { return strings.ToValidUTF8(string(b), "?") }

// nativeForm has nothing to add: paths are echoed as given.
func nativeForm(string) (string, bool) { return "", false }
