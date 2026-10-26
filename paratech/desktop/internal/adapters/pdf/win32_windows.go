//go:build windows

package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	// runtimeClientKey is the EdgeUpdate state of the stable WebView2 Runtime; EBWebView names its folder,
	// whose last element is the version.
	runtimeClientKey   = `Software\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	runtimeFolderValue = "EBWebView"
	runtimeClientDLL   = "EmbeddedBrowserWebView.dll"

	// minRuntimeVersion is the first runtime with PrintToPdf and CreatePrintSettings.
	minRuntimeVersion = "98.0.1108.43"

	pollInterval = 20 * time.Millisecond

	// settleTimeout bounds the extra wait after cancellation for a pending environment or controller.
	settleTimeout = 5 * time.Second
)

// errRuntimeTooOld separates «installed but too old» from «not installed» in the log; both are ErrUnsupported.
var errRuntimeTooOld = errors.New("webview2 runtime is older than " + minRuntimeVersion)

// webviewRuntime is an installed WebView2 Runtime usable for printing.
type webviewRuntime struct {
	dll     string
	version string
}

// findRuntime looks up the stable runtime per machine, then per user, as the official loader does.
func findRuntime() (webviewRuntime, error) {
	minVersion, _ := parseVersion(minRuntimeVersion)
	notFound := error(ErrUnsupported)

	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		folder, ok := runtimeFolder(root)
		if !ok {
			continue
		}

		version := filepath.Base(folder)
		parsed, ok := parseVersion(version)
		if !ok {
			continue
		}
		if compareVersions(parsed, minVersion) < 0 {
			notFound = errRuntimeTooOld
			continue
		}

		dll, ok := clientDLL(folder)
		if !ok {
			continue
		}
		return webviewRuntime{dll: dll, version: version}, nil
	}

	if errors.Is(notFound, errRuntimeTooOld) {
		return webviewRuntime{}, &opError{op: opFindRuntime, err: ErrUnsupported, detail: errRuntimeTooOld.Error()}
	}
	return webviewRuntime{}, &opError{op: opFindRuntime, err: ErrUnsupported, detail: "webview2 runtime not found"}
}

func runtimeFolder(root registry.Key) (string, bool) {
	k, err := registry.OpenKey(root, runtimeClientKey, registry.QUERY_VALUE|registry.WOW64_32KEY)
	if err != nil {
		return "", false
	}
	defer func() { _ = k.Close() }()

	folder, _, err := k.GetStringValue(runtimeFolderValue)
	if err != nil || folder == "" || !filepath.IsAbs(folder) {
		return "", false
	}
	return folder, true
}

func clientDLL(folder string) (string, bool) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "x86"
	case "arm64":
		arch = "arm64"
	default:
		return "", false
	}

	dll := filepath.Join(folder, "EBWebView", arch, runtimeClientDLL)
	if st, err := os.Stat(dll); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return dll, true
}

// parseVersion reads «a.b.c.d»; anything else is not a runtime version.
func parseVersion(s string) ([4]int, bool) {
	var v [4]int
	parts := strings.Split(s, ".")
	if len(parts) != len(v) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [4]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procPeekMessageW          = user32.NewProc("PeekMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procMsgWaitForMultipleObj = user32.NewProc("MsgWaitForMultipleObjectsEx")
)

const (
	wsPopup            = 0x80000000
	pmRemove           = 0x0001
	qsAllInput         = 0x04FF
	mwmoInputAvailable = 0x0004

	// The host window is never shown; its size is a plain A4 sheet at 96 dpi so the page has a sane viewport.
	hostWidth  = 794
	hostHeight = 1123
)

type winPoint struct{ x, y int32 }

// winMsg mirrors the Win32 MSG structure.
type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       winPoint
	lPrivate uint32
}

func loadUser32() error {
	for _, p := range []*windows.LazyProc{procCreateWindowExW, procDestroyWindow, procPeekMessageW,
		procTranslateMessage, procDispatchMessageW, procMsgWaitForMultipleObj} {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
}

// createHostWindow makes a hidden top-level window for the controller. The system STATIC class needs no
// registration and no window procedure of ours; ShowWindow is never called.
func createHostWindow() (uintptr, error) {
	class, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		return 0, err
	}
	hwnd, _, callErr := syscall.SyscallN(procCreateWindowExW.Addr(), 0, uintptr(unsafe.Pointer(class)), 0, //nolint:gosec // G103: class name string
		wsPopup, 0, 0, hostWidth, hostHeight, 0, 0, 0, 0)
	if hwnd == 0 {
		return 0, callErr
	}
	return hwnd, nil
}

func destroyWindow(hwnd uintptr) {
	_, _, _ = syscall.SyscallN(procDestroyWindow.Addr(), hwnd)
}

// pumpMessages dispatches what is queued for this thread: WebView2 delivers every callback through it.
func pumpMessages() {
	var msg winMsg
	for {
		r, _, _ := syscall.SyscallN(procPeekMessageW.Addr(), uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove) //nolint:gosec // G103: MSG out-parameter
		if r == 0 {
			return
		}
		_, _, _ = syscall.SyscallN(procTranslateMessage.Addr(), uintptr(unsafe.Pointer(&msg))) //nolint:gosec // G103: MSG in-parameter
		_, _, _ = syscall.SyscallN(procDispatchMessageW.Addr(), uintptr(unsafe.Pointer(&msg))) //nolint:gosec // G103: MSG in-parameter
	}
}

// pumpUntil runs the message loop until done reports true or ctx ends. ctx is polled, so cancellation is
// noticed within pollInterval without a second thread posting to this one.
func pumpUntil(ctx context.Context, done func() bool) error {
	wait := uintptr(pollInterval / time.Millisecond)
	for {
		pumpMessages()
		if done() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _, _ = syscall.SyscallN(procMsgWaitForMultipleObj.Addr(), 0, 0, wait, qsAllInput, mwmoInputAvailable)
	}
}
