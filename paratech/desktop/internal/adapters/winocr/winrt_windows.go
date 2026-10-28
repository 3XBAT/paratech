//go:build windows

package winocr

// Minimal WinRT plumbing: HSTRINGs, activation factories and raw vtable calls. x/sys/windows has no WinRT bindings,
// so the combase.dll functions are loaded lazily and interfaces are called through their vtables. IIDs and slots come
// from the Windows metadata (docs/refs/windows-ocr.md). unsafe is confined to this file.
//
// Pointer rules: every Go pointer handed to Windows is converted to uintptr directly in the argument list of
// syscall.SyscallN, so the compiler keeps it alive and in place for the call; Windows copies what it keeps.

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modCombase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize              = modCombase.NewProc("RoInitialize")
	procRoUninitialize            = modCombase.NewProc("RoUninitialize")
	procRoGetActivationFactory    = modCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = modCombase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = modCombase.NewProc("WindowsGetStringRawBuffer")
)

// roInitMultithreaded is RO_INIT_MULTITHREADED of the RO_INIT_TYPE enum (roapi.h).
const roInitMultithreaded = 1

// IUnknown slots, the same for every COM and WinRT interface; IInspectable takes 3 to 5, so WinRT methods start at 6.
const (
	slotQueryInterface = 0
	slotRelease        = 2
)

// maxSlots bounds the vtable view; the largest slot used here is 10.
const maxSlots = 16

// hresult keeps the 32 bits of an HRESULT returned in a register.
func hresult(r uintptr) uint32 { return uint32(r) } //nolint:gosec // an HRESULT is the low 32 bits

func failed(r uintptr) bool { return hresult(r)&0x80000000 != 0 }

// hrErr returns nil for a success code and an *HResultError for a failure.
func hrErr(op string, r uintptr) error {
	if !failed(r) {
		return nil
	}
	return &HResultError{Op: op, Code: hresult(r)}
}

// findProcs resolves the combase procs up front: LazyProc.Addr panics on a missing proc.
func findProcs() error {
	for _, p := range []*windows.LazyProc{procRoInitialize, procRoUninitialize, procRoGetActivationFactory,
		procWindowsCreateString, procWindowsDeleteString, procWindowsGetStringRawBuffer} {
		if p.Find() != nil {
			return fmt.Errorf("%w: combase.dll has no %s", ErrUnsupported, p.Name)
		}
	}
	return nil
}

func roInitialize() uintptr {
	hr, _, _ := syscall.SyscallN(procRoInitialize.Addr(), roInitMultithreaded)
	return hr
}

func roUninitialize() { _, _, _ = syscall.SyscallN(procRoUninitialize.Addr()) }

// hstring is an HSTRING handle; 0 is the empty string.
type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, fmt.Errorf("%w: string with NUL", ErrEngine)
	}
	var h hstring
	hr, _, _ := syscall.SyscallN(procWindowsCreateString.Addr(), uintptr(unsafe.Pointer(&u[0])), //nolint:gosec // G103: UTF-16 source, copied by WindowsCreateString
		uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))) //nolint:gosec // G103: HSTRING out-parameter
	if err := hrErr("WindowsCreateString", hr); err != nil {
		return 0, err
	}
	return h, nil
}

func (h hstring) free() {
	if h != 0 {
		_, _, _ = syscall.SyscallN(procWindowsDeleteString.Addr(), uintptr(h))
	}
}

// String copies the characters out; the handle stays owned by the caller.
func (h hstring) String() string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := syscall.SyscallN(procWindowsGetStringRawBuffer.Addr(), uintptr(h), uintptr(unsafe.Pointer(&n))) //nolint:gosec // G103: length out-parameter
	if p == 0 || n == 0 {
		return ""
	}
	// The buffer is owned by the HSTRING (not Go memory); reinterpreting the register value avoids a uintptr
	// conversion that vet's unsafeptr check cannot prove valid.
	buf := *(*unsafe.Pointer)(unsafe.Pointer(&p))                 //nolint:gosec // G103: pointer returned by WindowsGetStringRawBuffer
	return windows.UTF16ToString(unsafe.Slice((*uint16)(buf), n)) //nolint:gosec // G103: n characters, as WindowsGetStringRawBuffer reported
}

// takeString reads and frees an HSTRING received from an out-parameter.
func takeString(h hstring) string {
	defer h.free()
	return h.String()
}

// comObject is a WinRT interface pointer; the vtable lives in the runtime's memory.
type comObject struct {
	vtbl *[maxSlots]uintptr
}

// release drops one reference; nil-safe so every error path can release whatever it obtained.
func (o *comObject) release() {
	if o == nil {
		return
	}
	_, _, _ = syscall.SyscallN(o.vtbl[slotRelease], uintptr(unsafe.Pointer(o))) //nolint:gosec // G103: COM this
}

func (o *comObject) queryInterface(op string, iid *windows.GUID) (*comObject, error) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slotQueryInterface], uintptr(unsafe.Pointer(o)), //nolint:gosec // G103: COM this
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM in and out parameters
	if err := hrErr(op, hr); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, &HResultError{Op: op, Code: uint32(windows.E_NOINTERFACE)}
	}
	return out, nil
}

// call invokes a method whose arguments hold no Go pointers.
func (o *comObject) call(op string, slot int, args ...uintptr) error {
	this := uintptr(unsafe.Pointer(o)) //nolint:gosec // G103: COM this, memory owned by WinRT
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{this}, args...)...)
	return hrErr(op, hr)
}

// getObject calls a getter (T** out) returning an interface pointer the caller owns. A null result is returned as
// nil without error; callers that need an object check it.
func (o *comObject) getObject(op string, slot int) (*comObject, error) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	if err := hrErr(op, hr); err != nil {
		return nil, err
	}
	return out, nil
}

// getObjectWord calls a method (word, T** out), as GetAt(index) or CreateLanguage(HSTRING); the word holds no Go
// pointer. A null result is nil without error: TryCreateFromLanguage reports "no such recognizer" that way.
func (o *comObject) getObjectWord(op string, slot int, word uintptr) (*comObject, error) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), word, uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	if err := hrErr(op, hr); err != nil {
		return nil, err
	}
	return out, nil
}

// getObjectArg is getObjectWord with an interface-pointer argument (WinRT memory, not Go memory).
func (o *comObject) getObjectArg(op string, slot int, arg *comObject) (*comObject, error) {
	return o.getObjectWord(op, slot, uintptr(unsafe.Pointer(arg))) //nolint:gosec // G103: COM in-parameter
}

func (o *comObject) getUint32(op string, slot int) (uint32, error) {
	var out uint32
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return out, hrErr(op, hr)
}

func (o *comObject) getInt32(op string, slot int) (int32, error) {
	var out int32
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return out, hrErr(op, hr)
}

func (o *comObject) getString(op string, slot int) (string, error) {
	var h hstring
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&h))) //nolint:gosec // G103: HSTRING out-parameter
	if err := hrErr(op, hr); err != nil {
		return "", err
	}
	return takeString(h), nil
}

// getRect calls a getter returning Windows.Foundation.Rect through an out-pointer.
func (o *comObject) getRect(op string, slot int) (rectF, error) {
	var out rectF
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return out, hrErr(op, hr)
}

// getObjectWithHString calls a method (HSTRING, T** out).
func (o *comObject) getObjectWithHString(op string, slot int, s string) (*comObject, error) {
	h, err := newHString(s)
	if err != nil {
		return nil, err
	}
	defer h.free()
	return o.getObjectWord(op, slot, uintptr(h))
}

// createFromBytes calls ICryptographicBufferStatics.CreateFromByteArray(UINT32, BYTE*, IBuffer**): a WinRT in-array
// is passed as its length and a pointer; the runtime copies the bytes into the new buffer.
func (o *comObject) createFromBytes(op string, slot int, b []byte) (*comObject, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty pixel buffer", ErrBadImage)
	}
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(len(b)), //nolint:gosec // G103: COM this
		uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: array in, buffer out
	if err := hrErr(op, hr); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, &HResultError{Op: op, Code: uint32(windows.E_POINTER)}
	}
	return out, nil
}

// createCopyFromBuffer calls ISoftwareBitmapStatics.CreateCopyFromBuffer(IBuffer*, BitmapPixelFormat, INT32 width,
// INT32 height, SoftwareBitmap** out); enums are 32-bit integers at the ABI.
func (o *comObject) createCopyFromBuffer(op string, slot int, buf *comObject, format, w, h int) (*comObject, error) {
	var out *comObject
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(buf)), //nolint:gosec // G103: COM this and buffer
		uintptr(format), uintptr(w), uintptr(h), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: bitmap out-parameter; sizes are positive
	if err := hrErr(op, hr); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, &HResultError{Op: op, Code: uint32(windows.E_POINTER)}
	}
	return out, nil
}

// activationFactory returns the factory or statics interface iid of a runtime class. A class missing from this
// Windows (REGDB_E_CLASSNOTREG) is ErrUnsupported.
func activationFactory(class string, iid *windows.GUID) (*comObject, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()

	var out *comObject
	hr, _, _ := syscall.SyscallN(procRoGetActivationFactory.Addr(), uintptr(h), //nolint:gosec // G103: class name handle
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: IID in, factory out
	if hresult(hr) == uint32(windows.REGDB_E_CLASSNOTREG) {
		return nil, fmt.Errorf("%w: %s is not registered", ErrUnsupported, class)
	}
	if err := hrErr("RoGetActivationFactory "+class, hr); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, &HResultError{Op: "RoGetActivationFactory " + class, Code: uint32(windows.E_POINTER)}
	}
	return out, nil
}
