//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The raw state for the source picker (TASK-059): processes (Toolhelp32), visible top-level windows (EnumWindows,
// as OBS lists them) and the audio sessions of the active output devices (IAudioSessionManager2). The logic that
// turns it into apps is in apps.go.

func iidIAudioSessionManager2() windows.GUID {
	return windows.GUID{Data1: 0x77aa99a0, Data2: 0x1bd6, Data3: 0x484f, Data4: [8]byte{0x8b, 0xc7, 0x2c, 0x65, 0x4c, 0x9a, 0x9b, 0x6f}}
}

func iidIAudioSessionControl2() windows.GUID {
	return windows.GUID{Data1: 0xbfb7ff88, Data2: 0x7239, Data3: 0x4fc9, Data4: [8]byte{0x8f, 0xa2, 0x07, 0xc9, 0x50, 0xbe, 0x9c, 0x6d}}
}

func iidISimpleAudioVolume() windows.GUID {
	return windows.GUID{Data1: 0x87ce5498, Data2: 0x68d6, Data3: 0x44e5, Data4: [8]byte{0x92, 0x15, 0x6d, 0xa4, 0x7e, 0xf8, 0x83, 0xd8}}
}

// Constants from the mingw-w64 headers (mmdeviceapi.h, winuser.h).
const (
	deviceStateActive = 0x1 // DEVICE_STATE_ACTIVE
	gwlStyle          = -16 // GWL_STYLE
	gwlExStyle        = -20 // GWL_EXSTYLE

	maxDevices     = 16
	maxTitleChars  = 512
	imageChars     = 1024  // the first try; most paths fit
	maxImageChars  = 32768 // the longest Windows path
	uwpFrameClass  = "ApplicationFrameWindow"
	classNameChars = 64
)

// Vtable slots of the session interfaces (mingw-w64 audiopolicy.h, audioclient.h, mmdeviceapi.h).
const (
	slotEnumEnumAudioEndpoints = 3 // IMMDeviceEnumerator

	slotCollectionGetCount = 3 // IMMDeviceCollection: GetCount, Item
	slotCollectionItem     = 4

	slotManagerGetSessionEnumerator = 5 // IAudioSessionManager2 (after GetAudioSessionControl, GetSimpleAudioVolume)

	slotSessionsGetCount   = 3 // IAudioSessionEnumerator: GetCount, GetSession
	slotSessionsGetSession = 4

	slotControlGetState              = 3  // IAudioSessionControl
	slotControlGetProcessID          = 14 // IAudioSessionControl2
	slotControlIsSystemSoundsSession = 15

	slotVolumeGetMasterVolume = 4 // ISimpleAudioVolume: SetMasterVolume, GetMasterVolume, SetMute, GetMute
	slotVolumeGetMute         = 6
)

func platformApps() appPlatform {
	user32 := windows.NewLazySystemDLL("user32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	return &winApps{
		com:                newComProcs(),
		enumWindowsCB:      windows.NewCallback(enumWindowsProc),
		enumChildCB:        windows.NewCallback(enumChildProc),
		getWindowText:      user32.NewProc("InternalGetWindowText"),
		getWindowLongPtrW:  user32.NewProc("GetWindowLongPtrW"),
		getIconInfo:        user32.NewProc("GetIconInfo"),
		destroyIcon:        user32.NewProc("DestroyIcon"),
		extractIconExW:     windows.NewLazySystemDLL("shell32.dll").NewProc("ExtractIconExW"),
		getObjectW:         gdi32.NewProc("GetObjectW"),
		getDIBits:          gdi32.NewProc("GetDIBits"),
		createCompatibleDC: gdi32.NewProc("CreateCompatibleDC"),
		deleteDC:           gdi32.NewProc("DeleteDC"),
		deleteObject:       gdi32.NewProc("DeleteObject"),
	}
}

// winApps reads the raw state from Windows. Its calls are serialized by mu: the enumeration state is passed to the
// window callbacks through a field (a heap object, so its address stays valid while Windows holds it).
type winApps struct {
	com                          *comProcs
	enumWindowsCB, enumChildCB   uintptr
	getWindowText                *windows.LazyProc
	getWindowLongPtrW            *windows.LazyProc
	getIconInfo, destroyIcon     *windows.LazyProc
	extractIconExW               *windows.LazyProc
	getObjectW, getDIBits        *windows.LazyProc
	createCompatibleDC, deleteDC *windows.LazyProc
	deleteObject                 *windows.LazyProc

	mu    sync.Mutex
	enum  *windowEnum
	child *childEnum
}

func (w *winApps) snapshot(sessions bool) (snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var s snapshot
	s.self = windows.GetCurrentProcessId()
	if err := windows.ProcessIdToSessionId(s.self, &s.selfSession); err != nil {
		return snapshot{}, fmt.Errorf("own session: %w", err)
	}
	procs, err := processes()
	if err != nil {
		return snapshot{}, err
	}
	s.procs = procs
	if s.windows, err = w.windows(s.self); err != nil {
		return snapshot{}, err
	}
	if sessions {
		// COM objects are created, used and released on one locked thread in the MTA, like the capture.
		if cerr := comThread(func() { s.sessions, s.sessionsErr = w.sessions() }); cerr != nil {
			s.sessionsErr = cerr
		}
	}
	return s, nil
}

// processes is a Toolhelp32 snapshot with the session and the creation time of each process that can be opened.
func processes() ([]procInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var (
		out []procInfo
		e   windows.ProcessEntry32
	)
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil && len(out) < maxProcesses; err = windows.Process32Next(snap, &e) {
		p := procInfo{pid: e.ProcessID, parent: e.ParentProcessID, exe: windows.UTF16ToString(e.ExeFile[:])}
		p.sessionOK = windows.ProcessIdToSessionId(p.pid, &p.session) == nil
		p.created = creationTime(p.pid)
		out = append(out, p)
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("Process32Next: %w", err)
	}
	return out, nil
}

// creationTime is the creation time of pid, 0 when it cannot be opened.
func creationTime(pid uint32) uint64 {
	if pid == 0 {
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return processTimes(h)
}

func processTimes(h windows.Handle) uint64 {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return 0
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
}

// inspect opens pid for a limited query (allowed for most processes of the user, elevated ones included).
func (w *winApps) inspect(pid uint32) (procIdentity, error) {
	if pid == 0 {
		return procIdentity{}, nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return procIdentity{}, nil // no such process
	case err != nil:
		return procIdentity{}, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return procIdentity{}, fmt.Errorf("exit code of process %d: %w", pid, err)
	}
	id := procIdentity{alive: code == stillActive, created: processTimes(h)}
	if !id.alive {
		return id, nil // an exited process may have no image name any more
	}
	image, err := imageName(func(buf *uint16, n *uint32) error { return windows.QueryFullProcessImageName(h, 0, buf, n) })
	if err != nil {
		return procIdentity{}, fmt.Errorf("image of process %d: %w", pid, err)
	}
	id.image = image
	if err := windows.ProcessIdToSessionId(pid, &id.session); err != nil {
		return procIdentity{}, fmt.Errorf("session of process %d: %w", pid, err)
	}
	return id, nil
}

// imageName calls query with a buffer of imageChars characters, and with larger ones up to maxImageChars while
// Windows reports it too small.
func imageName(query func(buf *uint16, n *uint32) error) (string, error) {
	for size := uint32(imageChars); ; size = min(size*4, maxImageChars) {
		buf := make([]uint16, size)
		n := size
		err := query(&buf[0], &n)
		switch {
		case err == nil && n <= size:
			return windows.UTF16ToString(buf[:n]), nil
		case err == nil:
			return "", errors.New("the image name is longer than its buffer")
		case !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || size >= maxImageChars:
			return "", err
		}
	}
}

// windowEnum collects the windows of one EnumWindows run.
type windowEnum struct {
	w    *winApps
	self uint32 // this process: its windows are skipped before they are read
	out  []windowInfo
	full bool
}

// childEnum looks for the window of the UWP app inside a frame: a child owned by another process.
type childEnum struct {
	framePID uint32
	found    uint32
}

// enumWindowsProc is the EnumWindows callback (one function for every run, so one callback slot).
func enumWindowsProc(hwnd, param uintptr) uintptr {
	return (*windowEnum)(fromUintptr(param)).add(windows.HWND(hwnd))
}

func enumChildProc(hwnd, param uintptr) uintptr {
	c := (*childEnum)(fromUintptr(param))
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err == nil && pid != 0 && pid != c.framePID {
		c.found = pid
		return 0
	}
	return 1
}

func (w *winApps) windows(self uint32) ([]windowInfo, error) {
	e := &windowEnum{w: w, self: self}
	w.enum = e
	defer func() { w.enum = nil }()
	if err := windows.EnumWindows(w.enumWindowsCB, unsafe.Pointer(e)); err != nil && !e.full { //nolint:gosec // G103: the callback state, kept reachable by w.enum
		return nil, fmt.Errorf("EnumWindows: %w", err)
	}
	return e.out, nil
}

// add records one visible top-level window. Invisible ones are skipped here, so they do not use up the bound.
func (e *windowEnum) add(hwnd windows.HWND) uintptr {
	if len(e.out) >= maxWindows {
		e.full = true
		return 0
	}
	if !windows.IsWindowVisible(hwnd) {
		return 1
	}
	info := windowInfo{
		visible: true,
		style:   e.w.windowLong(hwnd, gwlStyle),
		exStyle: e.w.windowLong(hwnd, gwlExStyle),
	}
	if info.style&wsChild != 0 || info.exStyle&wsExToolWindow != 0 {
		return 1
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return 1
	}
	if className(hwnd) == uwpFrameClass {
		pid = e.w.uwpApp(hwnd, pid)
	}
	if pid == e.self { // never listed; and a busy UI thread of ours must not stall the enumeration
		return 1
	}
	info.owner, info.title = pid, e.w.windowText(hwnd)
	e.out = append(e.out, info)
	return 1
}

// uwpApp returns the PID of the app inside a UWP frame, or the frame's own PID (applicationframehost.exe, skipped
// later) when there is none (a suspended app).
func (w *winApps) uwpApp(frame windows.HWND, framePID uint32) uint32 {
	c := &childEnum{framePID: framePID}
	w.child = c
	defer func() { w.child = nil }()
	windows.EnumChildWindows(frame, w.enumChildCB, unsafe.Pointer(c)) //nolint:gosec // G103: the callback state, kept reachable by w.child
	if c.found != 0 {
		return c.found
	}
	return framePID
}

func (w *winApps) windowLong(hwnd windows.HWND, index int32) uint32 {
	idx := uintptr(index) //nolint:gosec // a negative GWL_ index goes sign-extended, as in C
	r, _, _ := w.getWindowLongPtrW.Call(uintptr(hwnd), idx)
	return uint32(r) //nolint:gosec // the styles are the low 32 bits
}

// windowText reads the stored title with InternalGetWindowText, which sends the window no message (GetWindowText
// sends WM_GETTEXT to windows of this process), so a hung window cannot block this.
func (w *winApps) windowText(hwnd windows.HWND) string {
	buf := make([]uint16, maxTitleChars)
	n, _, _ := w.getWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) //nolint:gosec // G103: output buffer
	if n == 0 || n > uintptr(len(buf)) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func className(hwnd windows.HWND) string {
	buf := make([]uint16, classNameChars)
	n, err := windows.GetClassName(hwnd, &buf[0], classNameChars)
	if err != nil || n <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// sessions reads the audio sessions of every active output device; one that cannot be read is skipped. It runs on
// a COM thread.
func (w *winApps) sessions() ([]sessionInfo, error) {
	enum, err := w.com.createDeviceEnumerator()
	if err != nil {
		return nil, err
	}
	defer release(enum)

	var coll unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(enum, slotEnumEnumAudioEndpoints), uintptr(enum), eRender, deviceStateActive,
		uintptr(unsafe.Pointer(&coll))) //nolint:gosec // G103: COM out-parameter
	if coll, err = checkOut("IMMDeviceEnumerator::EnumAudioEndpoints", hr(r), coll); err != nil {
		return nil, err
	}
	defer release(coll)

	n, err := uint32Out("IMMDeviceCollection::GetCount", coll, slotCollectionGetCount)
	if err != nil {
		return nil, err
	}
	var out []sessionInfo
	for i := range min(n, maxDevices) {
		out = deviceSessions(coll, i, out)
	}
	return out, nil
}

// deviceSessions appends the sessions of device i of coll, up to maxSessions in all.
func deviceSessions(coll unsafe.Pointer, i uint32, out []sessionInfo) []sessionInfo {
	var dev unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(coll, slotCollectionItem), uintptr(coll), uintptr(i), uintptr(unsafe.Pointer(&dev))) //nolint:gosec // G103: COM out-parameter
	dev, err := checkOut("IMMDeviceCollection::Item", hr(r), dev)
	if err != nil {
		return out
	}
	defer release(dev)

	iid := iidIAudioSessionManager2()
	var mgr unsafe.Pointer
	r, _, _ = syscall.SyscallN(method(dev, slotDeviceActivate), uintptr(dev),
		uintptr(unsafe.Pointer(&iid)), clsctxAll, 0, uintptr(unsafe.Pointer(&mgr))) //nolint:gosec // G103: COM parameters
	if mgr, err = checkOut("IMMDevice::Activate(IAudioSessionManager2)", hr(r), mgr); err != nil {
		return out
	}
	defer release(mgr)

	var list unsafe.Pointer
	r, _, _ = syscall.SyscallN(method(mgr, slotManagerGetSessionEnumerator), uintptr(mgr), uintptr(unsafe.Pointer(&list))) //nolint:gosec // G103: COM out-parameter
	if list, err = checkOut("IAudioSessionManager2::GetSessionEnumerator", hr(r), list); err != nil {
		return out
	}
	defer release(list)

	count, err := uint32Out("IAudioSessionEnumerator::GetCount", list, slotSessionsGetCount)
	if err != nil {
		return out
	}
	for j := range count {
		if len(out) >= maxSessions {
			break
		}
		var ctl unsafe.Pointer
		r, _, _ := syscall.SyscallN(method(list, slotSessionsGetSession), uintptr(list), uintptr(j), uintptr(unsafe.Pointer(&ctl))) //nolint:gosec // G103: COM out-parameter
		if ctl, err := checkOut("IAudioSessionEnumerator::GetSession", hr(r), ctl); err == nil {
			if s, ok := sessionOf(ctl); ok {
				out = append(out, s)
			}
			release(ctl)
		}
	}
	return out
}

// sessionOf reads one session; ok is false when it cannot be read.
func sessionOf(ctl unsafe.Pointer) (sessionInfo, bool) {
	ctl2, err := queryInterface(ctl, iidIAudioSessionControl2())
	if err != nil {
		return sessionInfo{}, false
	}
	defer release(ctl2)

	var s sessionInfo
	if s.state, err = uint32Out("IAudioSessionControl::GetState", ctl2, slotControlGetState); err != nil {
		return sessionInfo{}, false
	}
	r, _, _ := syscall.SyscallN(method(ctl2, slotControlIsSystemSoundsSession), uintptr(ctl2))
	s.system = hr(r) == hrOK // S_FALSE: not the system sounds
	// AUDCLNT_S_NO_SINGLE_PROCESS (a success code) still gives the creating process.
	if s.pid, err = uint32Out("IAudioSessionControl2::GetProcessId", ctl2, slotControlGetProcessID); err != nil {
		return sessionInfo{}, false
	}

	if vol, err := queryInterface(ctl, iidISimpleAudioVolume()); err == nil {
		if muted, err := uint32Out("ISimpleAudioVolume::GetMute", vol, slotVolumeGetMute); err == nil {
			s.muted = muted != 0
		}
		var level float32
		r, _, _ := syscall.SyscallN(method(vol, slotVolumeGetMasterVolume), uintptr(vol), uintptr(unsafe.Pointer(&level))) //nolint:gosec // G103: COM out-parameter
		if !hr(r).failed() {
			s.volume, s.volumeOK = level, true
		}
		release(vol)
	}
	return s, true
}
