//go:build windows

package whispercli

import (
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// wcNoBestFitChars makes WideCharToMultiByte report characters without an exact ANSI equivalent.
	wcNoBestFitChars = 0x00000400

	// mbErrInvalidChars makes MultiByteToWideChar fail on bytes that are not valid in the code page.
	mbErrInvalidChars = 0x00000008

	cpUTF8 = 65001

	exeExt = ".exe"
)

// x/sys/windows has no wrapper for WideCharToMultiByte.
var procWideCharToMultiByte = windows.NewLazySystemDLL("kernel32.dll").NewProc("WideCharToMultiByte")

// procGroup holds a job object: killing it ends the whole process tree, and closing the last handle (also when
// this app crashes) kills whatever is still running in it.
type procGroup struct {
	job windows.Handle
}

// newProcGroup returns a group, or nil when the job cannot be created; nil degrades to killing the process only.
func newProcGroup() *procGroup {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// DIE_ON_UNHANDLED_EXCEPTION: a crash ends the process instead of waiting on an error dialog.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION

	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))) //nolint:gosec // the API takes the struct address
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}

	return &procGroup{job: job}
}

// attach puts the started process into the job. The PID cannot be reused yet: exec.Cmd holds a process handle
// until Wait returns.
func (g *procGroup) attach(pid int) error {
	if g == nil {
		return errors.New("no job object")
	}
	if pid <= 0 || pid > math.MaxUint32 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	return windows.AssignProcessToJobObject(g.job, h)
}

func (g *procGroup) kill() {
	if g != nil {
		_ = windows.TerminateJobObject(g.job, 1)
	}
}

func (g *procGroup) close() {
	if g != nil {
		_ = windows.CloseHandle(g.job)
	}
}

// processDone reports whether the started, not yet waited process has exited. The PID is not reused before Wait.
func processDone(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}

	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == windows.WAIT_OBJECT_0
}

// setProcAttrs keeps the console process from opening a window next to the GUI app.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func binaryNameOK(bin string) bool {
	return strings.EqualFold(filepath.Ext(bin), exeExt)
}

// checkArgEncoding rejects arguments that whisper-cli would not receive unchanged. It reads its command line in
// the ANSI code page (main, no UTF-8 manifest), where "best fit" may turn a character into a quote or a
// backslash and split or join arguments; a double-byte trail byte can also look like a backslash.
func checkArgEncoding(s string) error {
	acp := windows.GetACP()
	if acp == cpUTF8 {
		return nil
	}
	return checkCodePage(s, acp)
}

func checkCodePage(s string, cp uint32) error {
	if isASCII(s) {
		return nil
	}

	bad := fmt.Errorf("%w: path is not representable in the ANSI code page %d of this system", ErrInvalidInput, cp)

	wide := utf16.Encode([]rune(s))
	ansi, err := toANSI(wide, cp)
	if err != nil {
		return bad
	}

	back, err := fromANSI(ansi, cp, mbErrInvalidChars)
	if err != nil || !slices.Equal(back, wide) {
		return bad
	}

	if strings.Count(string(ansi), `\`) != strings.Count(s, `\`) || strings.Count(string(ansi), `"`) != strings.Count(s, `"`) {
		return bad
	}

	return nil
}

func toANSI(wide []uint16, cp uint32) ([]byte, error) {
	if len(wide) == 0 || len(wide) > math.MaxInt32 {
		return nil, errors.New("bad length")
	}

	call := func(buf []byte) (int, bool, error) {
		var usedDefault int32
		var out, size uintptr
		if len(buf) > 0 {
			out, size = uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)) //nolint:gosec // the API takes the buffer address
		}
		n, _, err := procWideCharToMultiByte.Call(uintptr(cp), wcNoBestFitChars,
			uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)), out, size, 0, //nolint:gosec // the API takes the buffer address
			uintptr(unsafe.Pointer(&usedDefault))) //nolint:gosec // the API takes the flag address
		if n == 0 {
			return 0, false, err
		}
		return int(n), usedDefault != 0, nil //nolint:gosec // n is bounded by the buffer length
	}

	n, lossy, err := call(nil)
	if err != nil || lossy {
		return nil, errors.New("not representable")
	}

	buf := make([]byte, n)
	if _, lossy, err = call(buf); err != nil || lossy {
		return nil, errors.New("not representable")
	}
	return buf, nil
}

// fromANSI converts from the code page; flags 0 replaces invalid bytes instead of failing.
func fromANSI(ansi []byte, cp, flags uint32) ([]uint16, error) {
	if len(ansi) == 0 || len(ansi) > math.MaxInt32 {
		return nil, errors.New("bad length")
	}

	n, err := windows.MultiByteToWideChar(cp, flags, &ansi[0], int32(len(ansi)), nil, 0) //nolint:gosec // bounded above
	if err != nil {
		return nil, err
	}

	wide := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cp, flags, &ansi[0], int32(len(ansi)), &wide[0], n); err != nil { //nolint:gosec // bounded above
		return nil, err
	}
	return wide, nil
}

// decodeStderr converts the CLI's stderr to UTF-8: it prints paths and messages in the ANSI code page.
func decodeStderr(b []byte) string {
	return decodeANSI(b, windows.GetACP())
}

func decodeANSI(b []byte, cp uint32) string {
	if cp == cpUTF8 || isASCII(string(b)) {
		return strings.ToValidUTF8(string(b), "?")
	}

	wide, err := fromANSI(b, cp, 0)
	if err != nil {
		// Lossy, but nothing non-ASCII survives to leak.
		return strings.ToValidUTF8(string(b), "?")
	}
	return string(utf16.Decode(wide))
}

// nativeForm returns s in the ANSI code page, as the CLI echoes it; ok is false when that is s itself or impossible.
func nativeForm(s string) (string, bool) {
	return nativeFormCP(s, windows.GetACP())
}

func nativeFormCP(s string, cp uint32) (string, bool) {
	if cp == cpUTF8 || isASCII(s) {
		return "", false
	}

	b, err := toANSI(utf16.Encode([]rune(s)), cp)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
