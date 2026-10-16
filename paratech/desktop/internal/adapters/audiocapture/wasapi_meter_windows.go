//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows side of the diagnostics (TASK-058): the friendly name of an output device (IPropertyStore) and the
// audio meters the healthy-but-mute detector reads (IAudioMeterInformation). Everything runs on the capture thread and
// releases every interface it gets.

// iidIAudioMeterInformation is from the Windows SDK endpointvolume.h; mingw-w64 only declares the interface, so the
// value is not checked against a header here (docs/refs/audio.md). A wrong value would only make every QueryInterface
// fail with E_NOINTERFACE: the meters then never show sound and the detector never switches.
func iidIAudioMeterInformation() windows.GUID {
	return windows.GUID{Data1: 0xc02216f6, Data2: 0x8c67, Data3: 0x4b5b, Data4: [8]byte{0x9d, 0x00, 0xd0, 0x08, 0xe7, 0x3e, 0x00, 0x64}}
}

// iidIPropertyStore is from the mingw-w64 propsys.h.
func iidIPropertyStore() windows.GUID {
	return windows.GUID{Data1: 0x886d8eeb, Data2: 0x8cf2, Data3: 0x4446, Data4: [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99}}
}

// pkeyDeviceFriendlyName is PKEY_Device_FriendlyName from the mingw-w64 functiondiscoverykeys_devpkey.h.
func pkeyDeviceFriendlyName() propertyKey {
	return propertyKey{
		fmtid: windows.GUID{Data1: 0xa45c254e, Data2: 0xdf1c, Data3: 0x4efd, Data4: [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0}},
		pid:   14,
	}
}

// Constants and vtable slots from the mingw-w64 headers (objbase.h, wtypes.h, mmdeviceapi.h, propsys.h) and, for
// IAudioMeterInformation, the Windows SDK endpointvolume.h.
const (
	stgmRead  = 0  // STGM_READ
	vtLPWStr  = 31 // VT_LPWSTR
	maxNameCh = 512

	slotDeviceOpenPropertyStore = 4 // IMMDevice: Activate, OpenPropertyStore, GetId, GetState
	slotStoreGetValue           = 5 // IPropertyStore: GetCount, GetAt, GetValue, SetValue, Commit
	slotMeterGetPeakValue       = 3 // IAudioMeterInformation: GetPeakValue, GetMeteringChannelCount, ...
)

// propertyKey mirrors PROPERTYKEY (wtypes.h): a GUID and a DWORD, 20 bytes.
type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// propVariantValue mirrors a PROPVARIANT read back from Windows: vt, three reserved WORDs and the 16-byte value union,
// 24 bytes on 64-bit. For VT_LPWSTR the first union word is the CoTaskMem string; uintptr keeps it from the GC.
type propVariantValue struct {
	vt                              uint16
	reserved1, reserved2, reserved3 uint16
	val                             uintptr
	val2                            uintptr
}

// deviceName is the friendly name of an endpoint ("Speakers (Realtek(R) Audio)"), "" when it cannot be read. It is a
// name of the user's hardware, fine to log.
func (p *comProcs) deviceName(dev unsafe.Pointer) string {
	if err := p.procPropVariantClear.Find(); err != nil {
		return ""
	}
	var store unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceOpenPropertyStore), uintptr(dev), stgmRead,
		uintptr(unsafe.Pointer(&store))) //nolint:gosec // G103: COM out-parameter
	store, err := checkOut("IMMDevice::OpenPropertyStore", hr(r), store)
	if err != nil {
		return ""
	}
	defer release(store)

	key := pkeyDeviceFriendlyName()
	var pv propVariantValue
	r, _, _ = syscall.SyscallN(method(store, slotStoreGetValue), uintptr(store),
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&pv))) //nolint:gosec // G103: COM parameters
	if hr(r).failed() {
		return ""
	}
	defer func() { _, _, _ = syscall.SyscallN(p.procPropVariantClear.Addr(), uintptr(unsafe.Pointer(&pv))) }() //nolint:gosec // G103: frees the value Windows allocated
	if pv.vt != vtLPWStr || pv.val == 0 {
		return ""
	}
	name, ok := utf16At(fromUintptr(pv.val), maxNameCh)
	if !ok {
		return ""
	}
	return shortText(name)
}

// utf16At reads a NUL-terminated UTF-16 string of at most limit characters; ok is false when it is longer.
func utf16At(p unsafe.Pointer, limit int) (string, bool) {
	n := 0
	for n < limit && *(*uint16)(unsafe.Add(p, n*2)) != 0 { //nolint:gosec // G103: NUL-terminated string from Windows
		n++
	}
	if n == limit {
		return "", false
	}
	return windows.UTF16ToString(unsafe.Slice((*uint16)(p), n)), true //nolint:gosec // G103: the n characters counted above
}

// defaultDeviceName is the friendly name of the default output device, "" when it cannot be read.
func (p *comProcs) defaultDeviceName(enum unsafe.Pointer) string {
	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return ""
	}
	defer release(dev)
	return p.deviceName(dev)
}

// peakOf reads IAudioMeterInformation::GetPeakValue of meter: 0..1, the peak of the last processing period.
func peakOf(meter unsafe.Pointer) (float32, bool) {
	var peak float32
	r, _, _ := syscall.SyscallN(method(meter, slotMeterGetPeakValue), uintptr(meter), uintptr(unsafe.Pointer(&peak))) //nolint:gosec // G103: COM out-parameter
	return peak, !hr(r).failed()
}

// sessionPeak reads the peak meter of an audio session through its control (QueryInterface, as NAudio does).
func sessionPeak(ctl unsafe.Pointer) (float32, bool) {
	meter, err := queryInterface(ctl, iidIAudioMeterInformation())
	if err != nil {
		return 0, false
	}
	defer release(meter)
	return peakOf(meter)
}

// endpointPlaying reports a peak above zero on the default output device.
func endpointPlaying(enum unsafe.Pointer) bool {
	dev, err := defaultRenderEndpoint(enum)
	if err != nil {
		return false
	}
	defer release(dev)

	iid := iidIAudioMeterInformation()
	var meter unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceActivate), uintptr(dev),
		uintptr(unsafe.Pointer(&iid)), clsctxAll, 0, uintptr(unsafe.Pointer(&meter))) //nolint:gosec // G103: COM parameters
	if meter, err = checkOut("IMMDevice::Activate(IAudioMeterInformation)", hr(r), meter); err != nil {
		return false
	}
	defer release(meter)
	peak, ok := peakOf(meter)
	return ok && peak > 0
}

// targetMeter tells whether the target of a process loopback capture plays sound (playing of the endpoint seam).
type targetMeter struct {
	enum   unsafe.Pointer // IMMDeviceEnumerator; released by the endpoint
	target Target
}

// playing reads the meters once. INCLUDE: an audible session (active, peak above zero, not muted or at volume 0 in
// the mixer) whose process is in the target's tree, on any active output device, as process loopback captures every
// device. EXCLUDE: the default output device has a peak; nothing of ours renders while process loopback runs (the
// keep-alive exists only in classic loopback).
func (m *targetMeter) playing() bool {
	if m.target.Mode == ModeExclude {
		return endpointPlaying(m.enum)
	}
	sessions, err := renderSessions(m.enum)
	if err != nil || !anyAudible(sessions) {
		return false // the process snapshot is taken only when some session plays
	}
	procs, err := processes()
	if err != nil {
		return false
	}
	return audibleInTree(newTree(procs), m.target.PID, sessions)
}
