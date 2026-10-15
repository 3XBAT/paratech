//go:build windows && (amd64 || arm64)

// The COM code is 64-bit only: IAudioClient::Initialize takes two REFERENCE_TIME (int64) arguments, which a
// 32-bit call would have to split into two stack slots.

package wasapi

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GUIDs, copied from the mingw-w64 headers (mmdeviceapi.h, audioclient.h, unknwnbase.h, objidlbase.h).
// Package variables only because syscalls need their addresses; they are never written.
var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xbcde0395, Data2: 0xe52f, Data3: 0x467c, Data4: [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xa95664d2, Data2: 0x9614, Data3: 0x4f35, Data4: [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1cb9ad4c, Data2: 0xdbfa, Data3: 0x4c32, Data4: [8]byte{0xb1, 0x78, 0xc2, 0xf5, 0x68, 0xa7, 0x03, 0xb2}}
	iidIAudioCaptureClient  = windows.GUID{Data1: 0xc8adbd64, Data2: 0xe71e, Data3: 0x48a0, Data4: [8]byte{0xa4, 0xde, 0x18, 0x5c, 0x39, 0x5c, 0xd3, 0x17}}
	iidIAudioRenderClient   = windows.GUID{Data1: 0xf294acfc, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xa7, 0xbf, 0xad, 0xdc, 0xa7, 0xc2, 0x60, 0xe2}}
	iidIUnknown             = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIAgileObject         = windows.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	iidICompletionHandler   = windows.GUID{Data1: 0x41d949ab, Data2: 0x9862, Data3: 0x444a, Data4: [8]byte{0x80, 0xf6, 0xc2, 0x61, 0x33, 0x4d, 0xa5, 0xeb}}
)

// x/sys/windows has neither CoCreateInstance nor ActivateAudioInterfaceAsync (read-only after init).
var (
	procCoCreateInstance            = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
	procActivateAudioInterfaceAsync = windows.NewLazySystemDLL("mmdevapi.dll").NewProc("ActivateAudioInterfaceAsync")
)

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

	bufferFlagsDiscontinuity = 0x1 // AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY
	bufferFlagsSilent        = 0x2 // AUDCLNT_BUFFERFLAGS_SILENT

	vtBlob = 65 // VT_BLOB (wtypes.h)

	// virtualAudioDeviceProcessLoopback is VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK (audioclientactivationparams.h,
	// documented by Microsoft; the header is not in mingw-w64).
	virtualAudioDeviceProcessLoopback = `VAD\Process_Loopback`

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

// propVariant mirrors PROPVARIANT holding a BLOB (propidl.h, winsock2.h): vt, three reserved WORDs, then
// BLOB { ULONG cbSize; BYTE *pBlobData; }. 24 bytes on 64-bit, as in C.
type propVariant struct {
	vt        uint16
	reserved1 uint16
	reserved2 uint16
	reserved3 uint16
	blobSize  uint32
	blobData  *activationParams
}

// activationParams mirrors AUDIOCLIENT_ACTIVATION_PARAMS with its AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS union
// member (documented layout; not in mingw-w64): three DWORDs.
type activationParams struct {
	activationType      uint32
	targetProcessID     uint32
	processLoopbackMode uint32
}

// hresult takes the HRESULT from the low 32 bits of the return register.
func hresult(r uintptr) HRESULT { return HRESULT(uint32(r)) } //nolint:gosec // HRESULT is a 32-bit value

// method reads the function pointer of slot from the vtable of a COM object.
func method(obj unsafe.Pointer, slot uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
}

// call invokes a method whose arguments are plain values (no Go pointers).
func call(op string, obj unsafe.Pointer, slot uintptr, args ...uintptr) error {
	r, _, _ := syscall.SyscallN(method(obj, slot), append([]uintptr{uintptr(obj)}, args...)...)
	if hr := hresult(r); hr.Failed() {
		return hrError(op, hr)
	}
	return nil
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		syscall.SyscallN(method(obj, slotRelease), uintptr(obj)) //nolint:errcheck // Release returns a count
	}
}

// The wrappers below pass the addresses of Go out-variables inline in the SyscallN call, the pattern
// x/sys/windows uses, so the variables stay live and in place for the call.

func queryInterface(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(obj, slotQueryInterface), uintptr(obj),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("IUnknown::QueryInterface", hr)
	}
	return out, nil
}

func createDeviceEnumerator() (unsafe.Pointer, error) {
	if err := procCoCreateInstance.Find(); err != nil {
		return nil, fmt.Errorf("wasapi: CoCreateInstance: %w", err)
	}
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(procCoCreateInstance.Addr(),
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, //nolint:gosec // G103: COM in-parameter
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("CoCreateInstance(MMDeviceEnumerator)", hr)
	}
	return out, nil
}

func defaultRenderEndpoint(enum unsafe.Pointer) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(enum, slotEnumGetDefaultAudioEndpoint), uintptr(enum),
		eRender, eConsole, uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("IMMDeviceEnumerator::GetDefaultAudioEndpoint", hr)
	}
	return out, nil
}

// deviceID returns the endpoint id string; the CoTaskMem copy from GetId is freed here.
func deviceID(dev unsafe.Pointer) (string, error) {
	var p *uint16
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceGetID), uintptr(dev), uintptr(unsafe.Pointer(&p))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return "", hrError("IMMDevice::GetId", hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p)) //nolint:gosec // G103: freeing the CoTaskMem string
	return windows.UTF16PtrToString(p), nil
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
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceActivate), uintptr(dev),
		uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("IMMDevice::Activate(IAudioClient)", hr)
	}
	return out, nil
}

// mixFormat returns the CoTaskMem WAVEFORMATEX of the endpoint (the caller frees it) and its decoded form.
func mixFormat(client unsafe.Pointer) (unsafe.Pointer, Format, error) {
	var p unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(client, slotClientGetMixFormat), uintptr(client), uintptr(unsafe.Pointer(&p))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return nil, Format{}, hrError("IAudioClient::GetMixFormat", hr)
	}
	head := unsafe.Slice((*byte)(p), waveFormatExSize)                       //nolint:gosec // G103: fixed WAVEFORMATEX part
	f, err := parseWaveFormat(unsafe.Slice((*byte)(p), waveFormatLen(head))) //nolint:gosec // G103: 18 + cbSize bytes, as allocated by GetMixFormat
	if err != nil {
		windows.CoTaskMemFree(p)
		return nil, Format{}, err
	}
	return p, f, nil
}

// initialize opens a shared-mode stream; wfx points to a WAVEFORMATEX (CoTaskMem or a Go byte slice).
func initialize(client unsafe.Pointer, flags uint32, wfx unsafe.Pointer) error {
	r, _, _ := syscall.SyscallN(method(client, slotClientInitialize), uintptr(client),
		shareModeShared, uintptr(flags), bufferDuration, 0, uintptr(wfx), 0)
	if hr := hresult(r); hr.Failed() {
		return hrError("IAudioClient::Initialize", hr)
	}
	return nil
}

func uint32Out(op string, obj unsafe.Pointer, slot uintptr) (uint32, error) {
	var n uint32
	r, _, _ := syscall.SyscallN(method(obj, slot), uintptr(obj), uintptr(unsafe.Pointer(&n))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return 0, hrError(op, hr)
	}
	return n, nil
}

func getService(client unsafe.Pointer, iid *windows.GUID, name string) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(client, slotClientGetService), uintptr(client),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))) //nolint:gosec // G103: COM parameters
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("IAudioClient::GetService("+name+")", hr)
	}
	return out, nil
}

// captureGetBuffer returns the next packet; the device and QPC positions are not requested (both may be NULL).
func captureGetBuffer(capture unsafe.Pointer) (data unsafe.Pointer, frames, flags uint32, hr HRESULT) {
	r, _, _ := syscall.SyscallN(method(capture, slotCaptureGetBuffer), uintptr(capture),
		uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0) //nolint:gosec // G103: COM out-parameters
	return data, frames, flags, hresult(r)
}

func renderGetBuffer(render unsafe.Pointer, frames uint32) error {
	var data unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(render, slotRenderGetBuffer), uintptr(render),
		uintptr(frames), uintptr(unsafe.Pointer(&data))) //nolint:gosec // G103: COM out-parameter
	if hr := hresult(r); hr.Failed() {
		return hrError("IAudioRenderClient::GetBuffer", hr)
	}
	return nil
}

func activateAudioInterfaceAsync(path *uint16, params *propVariant, handler *completionHandler) (unsafe.Pointer, error) {
	if err := procActivateAudioInterfaceAsync.Find(); err != nil {
		return nil, fmt.Errorf("wasapi: ActivateAudioInterfaceAsync: %w", err)
	}
	var op unsafe.Pointer
	r, _, _ := syscall.SyscallN(procActivateAudioInterfaceAsync.Addr(),
		uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&iidIAudioClient)), //nolint:gosec // G103: COM in-parameters
		uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(&op))) //nolint:gosec // G103: COM parameters
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("ActivateAudioInterfaceAsync", hr)
	}
	return op, nil
}

// getActivateResult returns the activated interface, or the activation HRESULT as the error.
func getActivateResult(op unsafe.Pointer) (unsafe.Pointer, error) {
	var (
		activateHR uint32
		unk        unsafe.Pointer
	)
	r, _, _ := syscall.SyscallN(method(op, slotOpGetActivateResult), uintptr(op),
		uintptr(unsafe.Pointer(&activateHR)), uintptr(unsafe.Pointer(&unk))) //nolint:gosec // G103: COM out-parameters
	if hr := hresult(r); hr.Failed() {
		return nil, hrError("IActivateAudioInterfaceAsyncOperation::GetActivateResult", hr)
	}
	if hr := HRESULT(activateHR); hr.Failed() {
		release(unk)
		return nil, hrError("process loopback activation", hr)
	}
	if unk == nil {
		return nil, hrError("process loopback activation", hrPointer)
	}
	return unk, nil
}
