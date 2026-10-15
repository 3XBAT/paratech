//go:build windows && (amd64 || arm64)

// The COM code is 64-bit only: IAudioClient::Initialize takes two REFERENCE_TIME (int64) arguments, which a 32-bit
// call would have to split into two stack slots.

package audiocapture

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GUIDs from the mingw-w64 headers (mmdeviceapi.h, audioclient.h, unknwnbase.h, objidlbase.h); functions instead of
// package variables, the caller takes the address of its copy.
func clsidMMDeviceEnumerator() windows.GUID {
	return windows.GUID{Data1: 0xbcde0395, Data2: 0xe52f, Data3: 0x467c, Data4: [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e}}
}

func iidIMMDeviceEnumerator() windows.GUID {
	return windows.GUID{Data1: 0xa95664d2, Data2: 0x9614, Data3: 0x4f35, Data4: [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6}}
}

func iidIAudioClient() windows.GUID {
	return windows.GUID{Data1: 0x1cb9ad4c, Data2: 0xdbfa, Data3: 0x4c32, Data4: [8]byte{0xb1, 0x78, 0xc2, 0xf5, 0x68, 0xa7, 0x03, 0xb2}}
}

func iidIAudioCaptureClient() windows.GUID {
	return windows.GUID{Data1: 0xc8adbd64, Data2: 0xe71e, Data3: 0x48a0, Data4: [8]byte{0xa4, 0xde, 0x18, 0x5c, 0x39, 0x5c, 0xd3, 0x17}}
}

func iidIAudioRenderClient() windows.GUID {
	return windows.GUID{Data1: 0xf294acfc, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xa7, 0xbf, 0xad, 0xdc, 0xa7, 0xc2, 0x60, 0xe2}}
}

func iidIUnknown() windows.GUID {
	return windows.GUID{Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
}

func iidIAgileObject() windows.GUID {
	return windows.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
}

func iidICompletionHandler() windows.GUID {
	return windows.GUID{Data1: 0x41d949ab, Data2: 0x9862, Data3: 0x444a, Data4: [8]byte{0x80, 0xf6, 0xc2, 0x61, 0x33, 0x4d, 0xa5, 0xeb}}
}

// Constants from the mingw-w64 headers unless marked otherwise.
const (
	clsctxAll = 0x1 | 0x2 | 0x4 | 0x10 // CLSCTX_ALL (combaseapi.h)

	eRender  = 0 // EDataFlow
	eConsole = 0 // ERole

	shareModeShared = 0 // AUDCLNT_SHAREMODE_SHARED

	streamFlagsLoopback          = 0x00020000 // AUDCLNT_STREAMFLAGS_LOOPBACK
	streamFlagsEventCallback     = 0x00040000 // AUDCLNT_STREAMFLAGS_EVENTCALLBACK
	streamFlagsSrcDefaultQuality = 0x08000000 // AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
	streamFlagsAutoConvertPCM    = 0x80000000 // AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM

	classicFlags = streamFlagsLoopback | streamFlagsEventCallback
	processFlags = streamFlagsLoopback | streamFlagsEventCallback | streamFlagsAutoConvertPCM | streamFlagsSrcDefaultQuality

	bufferFlagsDiscontinuity = 0x1 // AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY
	bufferFlagsSilent        = 0x2 // AUDCLNT_BUFFERFLAGS_SILENT

	vtBlob = 65 // VT_BLOB (wtypes.h)

	// virtualAudioDeviceProcessLoopback is VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK (audioclientactivationparams.h,
	// documented by Microsoft; the header is not in mingw-w64).
	virtualAudioDeviceProcessLoopback = `VAD\Process_Loopback`

	// AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK and PROCESS_LOOPBACK_MODE (documented by Microsoft).
	activationTypeProcessLoopback = 1
	processLoopbackModeInclude    = 0 // PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE
	processLoopbackModeExclude    = 1 // PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE

	// bufferDuration is 200 ms in REFERENCE_TIME units (100 ns).
	bufferDuration = 2_000_000
)

// Vtable slots: IUnknown takes 0-2, then the methods in header order.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotEnumGetDefaultAudioEndpoint = 4 // IMMDeviceEnumerator: EnumAudioEndpoints, GetDefaultAudioEndpoint, ...

	slotDeviceActivate = 3 // IMMDevice: Activate, OpenPropertyStore, GetId, GetState
	slotDeviceGetID    = 5

	slotClientInitialize        = 3 // IAudioClient: Initialize, GetBufferSize, GetStreamLatency, GetCurrentPadding,
	slotClientGetBufferSize     = 4 // IsFormatSupported, GetMixFormat, GetDevicePeriod, Start, Stop, Reset,
	slotClientGetCurrentPadding = 6 // SetEventHandle, GetService
	slotClientGetMixFormat      = 8
	slotClientStart             = 10
	slotClientStop              = 11
	slotClientSetEventHandle    = 13
	slotClientGetService        = 14

	slotCaptureGetBuffer         = 3 // IAudioCaptureClient: GetBuffer, ReleaseBuffer, GetNextPacketSize
	slotCaptureReleaseBuffer     = 4
	slotCaptureGetNextPacketSize = 5

	slotRenderGetBuffer     = 3 // IAudioRenderClient: GetBuffer, ReleaseBuffer
	slotRenderReleaseBuffer = 4

	slotOpGetActivateResult = 3 // IActivateAudioInterfaceAsyncOperation: GetActivateResult
)

// propVariant mirrors a PROPVARIANT holding a BLOB (propidl.h): vt, three reserved WORDs, then
// BLOB { ULONG cbSize; BYTE *pBlobData; }. 24 bytes on 64-bit, as in C (checked by a test).
type propVariant struct {
	vt        uint16
	reserved1 uint16
	reserved2 uint16
	reserved3 uint16
	blobSize  uint32
	blobData  *activationParams
}

// activationParams mirrors AUDIOCLIENT_ACTIVATION_PARAMS with its AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS union member
// (documented layout; not in mingw-w64): three DWORDs.
type activationParams struct {
	activationType      uint32
	targetProcessID     uint32
	processLoopbackMode uint32
}

// comProcs are the entry points x/sys/windows does not wrap; resolved on first use.
type comProcs struct {
	procCoCreateInstance            *windows.LazyProc
	procActivateAudioInterfaceAsync *windows.LazyProc
}

func newComProcs() *comProcs {
	return &comProcs{
		procCoCreateInstance:            windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance"),
		procActivateAudioInterfaceAsync: windows.NewLazySystemDLL("mmdevapi.dll").NewProc("ActivateAudioInterfaceAsync"),
	}
}

// hr takes the HRESULT from the low 32 bits of the return register.
func hr(r uintptr) hresult { return hresult(uint32(r)) } //nolint:gosec // an HRESULT is a 32-bit value

// method reads the function pointer of slot from the vtable of a COM object.
func method(obj unsafe.Pointer, slot uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
}

// call invokes a method whose arguments are plain values (no Go pointers).
func call(op string, obj unsafe.Pointer, slot uintptr, args ...uintptr) error {
	r, _, _ := syscall.SyscallN(method(obj, slot), append([]uintptr{uintptr(obj)}, args...)...)
	if h := hr(r); h.failed() {
		return hrError(op, h)
	}
	return nil
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		syscall.SyscallN(method(obj, slotRelease), uintptr(obj)) //nolint:errcheck // Release returns a count
	}
}

// The wrappers below pass the addresses of Go out-variables inline in the SyscallN call, the pattern x/sys/windows
// uses, so the variables stay live and in place for the call. A nil out-pointer after S_OK is E_POINTER.

func queryInterface(obj unsafe.Pointer, iid windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(obj, slotQueryInterface), uintptr(obj),
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	return checkOut("IUnknown::QueryInterface", hr(r), out)
}

func checkOut(op string, h hresult, out unsafe.Pointer) (unsafe.Pointer, error) {
	if h.failed() {
		return nil, hrError(op, h)
	}
	if out == nil {
		return nil, hrError(op, hrPointer)
	}
	return out, nil
}

func (p *comProcs) createDeviceEnumerator() (unsafe.Pointer, error) {
	if err := p.procCoCreateInstance.Find(); err != nil {
		return nil, fmt.Errorf("%w: CoCreateInstance: %v", ErrNotSupported, err)
	}
	clsid, iid := clsidMMDeviceEnumerator(), iidIMMDeviceEnumerator()
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(p.procCoCreateInstance.Addr(),
		uintptr(unsafe.Pointer(&clsid)), 0, clsctxAll, //nolint:gosec // G103: COM in-parameter
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	return checkOut("CoCreateInstance(MMDeviceEnumerator)", hr(r), out)
}

func defaultRenderEndpoint(enum unsafe.Pointer) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(enum, slotEnumGetDefaultAudioEndpoint), uintptr(enum),
		eRender, eConsole, uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	return checkOut("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hr(r), out)
}

// maxDeviceIDLen bounds the endpoint id string read from Windows (ids are about 55 characters).
const maxDeviceIDLen = 1024

// deviceID returns the endpoint id string; the CoTaskMem copy from GetId is freed here.
func deviceID(dev unsafe.Pointer) (string, error) {
	var p *uint16
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceGetID), uintptr(dev), uintptr(unsafe.Pointer(&p))) //nolint:gosec // G103: COM out-parameter
	if h := hr(r); h.failed() {
		return "", hrError("IMMDevice::GetId", h)
	}
	if p == nil {
		return "", hrError("IMMDevice::GetId", hrPointer)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p)) //nolint:gosec // G103: freeing the CoTaskMem string

	n := 0
	for n < maxDeviceIDLen && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 { //nolint:gosec // G103: NUL-terminated string from GetId
		n++
	}
	if n == maxDeviceIDLen {
		return "", fmt.Errorf("%w: endpoint id longer than %d characters", ErrDevice, maxDeviceIDLen)
	}
	return windows.UTF16ToString(unsafe.Slice(p, n)), nil //nolint:gosec // G103: the n characters counted above
}

// defaultDeviceID returns the id of the current default render endpoint.
func defaultDeviceID(enum unsafe.Pointer) (string, error) {
	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return "", err
	}
	defer release(dev)
	return deviceID(dev)
}

func activateAudioClient(dev unsafe.Pointer) (unsafe.Pointer, error) {
	iid := iidIAudioClient()
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceActivate), uintptr(dev),
		uintptr(unsafe.Pointer(&iid)), clsctxAll, 0, uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	return checkOut("IMMDevice::Activate(IAudioClient)", hr(r), out)
}

// mixFormat returns the CoTaskMem WAVEFORMATEX of the endpoint (the caller frees it) and its validated form. Only the
// bytes Windows allocated are read: 18, then up to the WAVEFORMATEXTENSIBLE size within 18 + cbSize.
func mixFormat(client unsafe.Pointer) (unsafe.Pointer, Format, error) {
	var p unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(client, slotClientGetMixFormat), uintptr(client), uintptr(unsafe.Pointer(&p))) //nolint:gosec // G103: COM out-parameter
	if h := hr(r); h.failed() {
		return nil, Format{}, hrError("IAudioClient::GetMixFormat", h)
	}
	if p == nil {
		return nil, Format{}, hrError("IAudioClient::GetMixFormat", hrPointer)
	}
	head := unsafe.Slice((*byte)(p), waveFormatExSize)                           //nolint:gosec // G103: fixed WAVEFORMATEX part
	f, err := parseWaveFormat(unsafe.Slice((*byte)(p), waveFormatReadLen(head))) //nolint:gosec // G103: within 18 + cbSize bytes
	if err != nil {
		windows.CoTaskMemFree(p)
		return nil, Format{}, fmt.Errorf("mix format: %w", err)
	}
	return p, f, nil
}

// initialize opens a shared-mode stream; wfx points to a WAVEFORMATEX (CoTaskMem or a Go byte slice).
func initialize(client unsafe.Pointer, flags uint32, wfx unsafe.Pointer) error {
	r, _, _ := syscall.SyscallN(method(client, slotClientInitialize), uintptr(client),
		shareModeShared, uintptr(flags), bufferDuration, 0, uintptr(wfx), 0)
	if h := hr(r); h.failed() {
		return hrError("IAudioClient::Initialize", h)
	}
	return nil
}

func uint32Out(op string, obj unsafe.Pointer, slot uintptr) (uint32, error) {
	var n uint32
	r, _, _ := syscall.SyscallN(method(obj, slot), uintptr(obj), uintptr(unsafe.Pointer(&n))) //nolint:gosec // G103: COM out-parameter
	if h := hr(r); h.failed() {
		return 0, hrError(op, h)
	}
	return n, nil
}

func getService(client unsafe.Pointer, iid windows.GUID, name string) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(client, slotClientGetService), uintptr(client),
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	return checkOut("IAudioClient::GetService("+name+")", hr(r), out)
}

// captureGetBuffer returns the next packet; the device and QPC positions are not requested (both may be NULL).
func captureGetBuffer(capture unsafe.Pointer) (data unsafe.Pointer, frames, flags uint32, h hresult) {
	r, _, _ := syscall.SyscallN(method(capture, slotCaptureGetBuffer), uintptr(capture),
		uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0) //nolint:gosec // G103: COM out-parameters
	return data, frames, flags, hr(r)
}

func renderGetBuffer(render unsafe.Pointer, frames uint32) error {
	var data unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(render, slotRenderGetBuffer), uintptr(render),
		uintptr(frames), uintptr(unsafe.Pointer(&data))) //nolint:gosec // G103: COM out-parameter
	if h := hr(r); h.failed() {
		return hrError("IAudioRenderClient::GetBuffer", h)
	}
	return nil
}

// activateAudioInterfaceAsync starts the activation of a. Every argument points into a (heap memory kept by the
// source), never into the goroutine stack: Windows may call the Go completion handler during this call, and a Go
// callback can grow and move the stack while Windows still holds the addresses (syscall.SyscallN does not pin them).
func (p *comProcs) activateAudioInterfaceAsync(a *activation) (unsafe.Pointer, error) {
	if err := p.procActivateAudioInterfaceAsync.Find(); err != nil {
		return nil, fmt.Errorf("%w: ActivateAudioInterfaceAsync: %v", ErrNotSupported, err)
	}
	r, _, _ := syscall.SyscallN(p.procActivateAudioInterfaceAsync.Addr(),
		uintptr(unsafe.Pointer(&a.path[0])), uintptr(unsafe.Pointer(&a.iid)), //nolint:gosec // G103: COM in-parameters
		uintptr(unsafe.Pointer(&a.pv)), uintptr(unsafe.Pointer(&a.handler)), uintptr(unsafe.Pointer(&a.op))) //nolint:gosec // G103: COM parameters
	return checkOut("ActivateAudioInterfaceAsync", hr(r), a.op)
}

// getActivateResult returns the activated interface, or the activation HRESULT as the error.
func getActivateResult(op unsafe.Pointer) (unsafe.Pointer, error) {
	var (
		activateHR uint32
		unk        unsafe.Pointer
	)
	r, _, _ := syscall.SyscallN(method(op, slotOpGetActivateResult), uintptr(op),
		uintptr(unsafe.Pointer(&activateHR)), uintptr(unsafe.Pointer(&unk))) //nolint:gosec // G103: COM out-parameters
	if h := hr(r); h.failed() {
		return nil, hrError("IActivateAudioInterfaceAsyncOperation::GetActivateResult", h)
	}
	if h := hresult(activateHR); h.failed() {
		release(unk)
		return nil, hrError("process loopback activation", h)
	}
	return checkOut("process loopback activation", hrOK, unk)
}

// coInitialize joins the MTA on the locked thread. S_FALSE (already initialized) still needs CoUninitialize.
func coInitialize() (func(), error) {
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	var errno syscall.Errno
	switch {
	case err == nil:
	case errors.As(err, &errno) && hr(uintptr(errno)) == hrFalse:
	case errors.As(err, &errno):
		return nil, hrError("CoInitializeEx", hr(uintptr(errno)))
	default:
		return nil, fmt.Errorf("%w: CoInitializeEx: %v", ErrDevice, err)
	}
	return windows.CoUninitialize, nil
}
