//go:build windows

package pdf

// Minimal COM plumbing for the few WebView2 calls of this adapter. WebView2 is a COM API, x/sys/windows has no
// bindings for it and Wails keeps its own in an internal package, so interfaces are called through raw vtables.
// unsafe is confined to this file and win32_windows.go; the session code works with typed helpers.
//
// Pointer rules: every Go pointer handed to Windows is converted to uintptr directly in the argument list of
// syscall.SyscallN (an assembly function), so the compiler keeps it alive and in place for the call. Handler
// objects outlive the call and are kept reachable in liveHandlers until COM releases its last reference.

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HRESULT values used here.
const (
	hrOK          = 0
	hrNoInterface = 0x80004002
	hrPointer     = 0x80004003
)

// IUnknown slots, the same for every COM interface.
const (
	slotQueryInterface = 0
	slotAddRef         = 1
	slotRelease        = 2
)

// maxSlots bounds the vtable view; the largest slot used here is ICoreWebView2_7::PrintToPdf (80).
const maxSlots = 128

var iidIUnknown = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
	Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}

// hresult keeps the 32 bits of an HRESULT returned in a register.
func hresult(r uintptr) uint32 { return uint32(r) } //nolint:gosec // an HRESULT is the low 32 bits

func failed(r uintptr) bool { return hresult(r)&0x80000000 != 0 }

// comObject is an interface pointer owned by WebView2; the vtable lives in the runtime's memory.
type comObject struct {
	vtbl *[maxSlots]uintptr
}

// release drops one reference; nil-safe so teardown can release whatever was obtained.
func (o *comObject) release() {
	if o == nil {
		return
	}
	_, _, _ = syscall.SyscallN(o.vtbl[slotRelease], uintptr(unsafe.Pointer(o))) //nolint:gosec // G103: COM this
}

func (o *comObject) addRef() {
	_, _, _ = syscall.SyscallN(o.vtbl[slotAddRef], uintptr(unsafe.Pointer(o))) //nolint:gosec // G103: COM this
}

func (o *comObject) queryInterface(iid *windows.GUID) (*comObject, uintptr) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slotQueryInterface], uintptr(unsafe.Pointer(o)), //nolint:gosec // G103: COM this
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM in and out parameters
	if failed(hr) {
		return nil, hr
	}
	return out, hr
}

// call invokes a method whose arguments hold no Go pointers.
func (o *comObject) call(slot int, args ...uintptr) uintptr {
	this := uintptr(unsafe.Pointer(o)) //nolint:gosec // G103: COM this, memory owned by WebView2
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{this}, args...)...)
	return hr
}

// getObject calls a property getter that returns an interface pointer (the caller owns the reference).
func (o *comObject) getObject(slot int) (*comObject, uintptr) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	if failed(hr) || out == nil {
		return nil, hr
	}
	return out, hr
}

func (o *comObject) getBool(slot int) (bool, uintptr) {
	var out int32
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return out != 0, hr
}

func (o *comObject) getUint64(slot int) (uint64, uintptr) {
	var out uint64
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return out, hr
}

func (o *comObject) putBool(slot int, v bool) uintptr {
	var b uintptr
	if v {
		b = 1
	}
	return o.call(slot, b)
}

// putDouble passes a by-value C double. amd64: the runtime mirrors the first four call words into XMM0-3, where
// the x64 ABI reads doubles. 386: stdcall takes the double as two stack words. arm64: Go cannot fill d0-d7
// (golang.org/issue/62583), so the call is skipped and ok is false.
func (o *comObject) putDouble(slot int, v float64) (hr uintptr, ok bool) {
	bits := math.Float64bits(v)
	switch runtime.GOARCH {
	case "amd64":
		return o.call(slot, uintptr(bits)), true
	case "386":
		lo, hi := uintptr(bits&math.MaxUint32), uintptr(bits>>32)
		return o.call(slot, lo, hi), true
	default:
		return 0, false
	}
}

// putString passes an LPCWSTR argument.
func (o *comObject) putString(slot int, s *uint16) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(s))) //nolint:gosec // G103: COM in-parameter
	return hr
}

// addHandler subscribes h to an event (add_* methods); the registration token is not needed because the
// controller is closed at the end of every print, which drops all subscriptions.
func (o *comObject) addHandler(slot int, h *comHandler) uintptr {
	var token int64
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), //nolint:gosec // G103: COM this
		uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token))) //nolint:gosec // G103: handler in, token out
	return hr
}

// callWithHandler calls a method taking (word, handler), as CreateCoreWebView2Controller(HWND, handler).
func (o *comObject) callWithHandler(slot int, word uintptr, h *comHandler) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), word, uintptr(unsafe.Pointer(h))) //nolint:gosec // G103: COM this and handler
	return hr
}

// printToPdf calls ICoreWebView2_7::PrintToPdf(path, settings, handler).
func (o *comObject) printToPdf(slot int, path *uint16, settings *comObject, h *comHandler) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), //nolint:gosec // G103: COM this
		uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(settings)), uintptr(unsafe.Pointer(h))) //nolint:gosec // G103: COM in-parameters
	return hr
}

// createEnvironment calls the runtime's CreateWebViewEnvironmentWithOptionsInternal(flag, runtime type, user
// data folder, options, handler) with no options object.
func createEnvironment(proc *windows.Proc, flag, runtimeType uintptr, folder *uint16, h *comHandler) uintptr {
	hr, _, _ := syscall.SyscallN(proc.Addr(), flag, runtimeType, uintptr(unsafe.Pointer(folder)), 0, //nolint:gosec // G103: folder string
		uintptr(unsafe.Pointer(h))) //nolint:gosec // G103: completion handler
	return hr
}

// comHandler is a COM object implemented in Go: a completion handler or an event handler of WebView2. All three
// shapes used here take two arguments after "this", so one struct with three vtables covers them.
type comHandler struct {
	vtbl *handlerVtbl // first field: COM reads the vtable pointer at offset 0
	iid  windows.GUID
	refs atomic.Int32

	onResult func(hr uintptr, obj *comObject) // Invoke(HRESULT, Interface*)
	onEvent  func(args *comObject)            // Invoke(ICoreWebView2* sender, Args*)
	onPrint  func(hr uintptr, ok bool)        // Invoke(HRESULT, BOOL)
}

type handlerVtbl struct {
	queryInterface, addRef, release, invoke uintptr
}

var (
	vtblOnce                         sync.Once
	resultVtbl, eventVtbl, printVtbl handlerVtbl

	liveMu       sync.Mutex
	liveHandlers = map[*comHandler]struct{}{}
)

// initVtables creates the callbacks once: NewCallback slots are never freed and limited per process.
func initVtables() {
	vtblOnce.Do(func() {
		qi := windows.NewCallback(handlerQueryInterface)
		add := windows.NewCallback(handlerAddRef)
		rel := windows.NewCallback(handlerRelease)
		resultVtbl = handlerVtbl{qi, add, rel, windows.NewCallback(handlerInvokeResult)}
		eventVtbl = handlerVtbl{qi, add, rel, windows.NewCallback(handlerInvokeEvent)}
		printVtbl = handlerVtbl{qi, add, rel, windows.NewCallback(handlerInvokePrint)}
	})
}

func newHandler(vtbl *handlerVtbl, iid windows.GUID) *comHandler {
	h := &comHandler{vtbl: vtbl, iid: iid}
	h.refs.Store(1)
	liveMu.Lock()
	liveHandlers[h] = struct{}{}
	liveMu.Unlock()
	return h
}

func newResultHandler(iid windows.GUID, fn func(hr uintptr, obj *comObject)) *comHandler {
	initVtables()
	h := newHandler(&resultVtbl, iid)
	h.onResult = fn
	return h
}

func newEventHandler(iid windows.GUID, fn func(args *comObject)) *comHandler {
	initVtables()
	h := newHandler(&eventVtbl, iid)
	h.onEvent = fn
	return h
}

func newPrintHandler(iid windows.GUID, fn func(hr uintptr, ok bool)) *comHandler {
	initVtables()
	h := newHandler(&printVtbl, iid)
	h.onPrint = fn
	return h
}

// handlerQueryInterface answers only IUnknown and the handler's own interface. out is stored through *uintptr:
// a typed pointer store into Windows memory would run the GC write barrier on it.
func handlerQueryInterface(this *comHandler, iid *windows.GUID, out *uintptr) uintptr {
	if out == nil || iid == nil {
		return hrPointer
	}
	if *iid != iidIUnknown && *iid != this.iid {
		*out = 0
		return hrNoInterface
	}
	*out = uintptr(unsafe.Pointer(this)) //nolint:gosec // G103: the handler, kept reachable by liveHandlers
	this.refs.Add(1)
	return hrOK
}

func handlerAddRef(this *comHandler) uintptr {
	return uintptr(this.refs.Add(1)) //nolint:gosec // the count is small and positive
}

// handlerRelease forgets the handler when the last reference (ours or the runtime's) is gone.
func handlerRelease(this *comHandler) uintptr {
	n := this.refs.Add(-1)
	if n == 0 {
		liveMu.Lock()
		delete(liveHandlers, this)
		liveMu.Unlock()
	}
	return uintptr(max(n, 0)) //nolint:gosec // not negative after max
}

func handlerInvokeResult(this *comHandler, hr uintptr, obj *comObject) uintptr {
	if this.onResult != nil {
		this.onResult(hr, obj)
	}
	return hrOK
}

func handlerInvokeEvent(this *comHandler, _ *comObject, args *comObject) uintptr {
	if this.onEvent != nil && args != nil {
		this.onEvent(args)
	}
	return hrOK
}

// handlerInvokePrint receives (HRESULT, BOOL); a BOOL is the low 32 bits of its register.
func handlerInvokePrint(this *comHandler, hr uintptr, ok uintptr) uintptr {
	if this.onPrint != nil {
		this.onPrint(hr, ok&math.MaxUint32 != 0)
	}
	return hrOK
}
