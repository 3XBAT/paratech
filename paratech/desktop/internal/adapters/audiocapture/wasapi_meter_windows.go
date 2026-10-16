//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows side of the diagnostics (TASK-058): the friendly name, form factor and enumerator of an output device
// (IPropertyStore) and the audio meters the healthy-but-mute detector reads (IAudioMeterInformation). Everything runs
// on a COM thread and releases every interface it gets.

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
	vtUI4     = 19 // VT_UI4
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

// pkeyAudioEndpointFormFactor is PKEY_AudioEndpoint_FormFactor from the mingw-w64 mmdeviceapi.h: a VT_UI4
// EndpointFormFactor.
func pkeyAudioEndpointFormFactor() propertyKey {
	return propertyKey{
		fmtid: windows.GUID{Data1: 0x1da5d803, Data2: 0xd492, Data3: 0x4edd, Data4: [8]byte{0x8c, 0x23, 0xe0, 0xc0, 0xff, 0xee, 0x7f, 0x0e}},
		pid:   0,
	}
}

// pkeyDeviceEnumeratorName is PKEY_Device_EnumeratorName from the mingw-w64 functiondiscoverykeys_devpkey.h: VT_LPWSTR.
func pkeyDeviceEnumeratorName() propertyKey {
	return propertyKey{
		fmtid: windows.GUID{Data1: 0xa45c254e, Data2: 0xdf1c, Data3: 0x4efd, Data4: [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0}},
		pid:   24,
	}
}

// deviceName is the friendly name of an endpoint ("Speakers (Realtek(R) Audio)"), "" when it cannot be read. It is a
// name of the user's hardware, fine to log.
func (p *comProcs) deviceName(dev unsafe.Pointer) string {
	var name string
	p.withStore(dev, func(store unsafe.Pointer) { name = p.stringProp(store, pkeyDeviceFriendlyName()) })
	return name
}

// endpointProps are the properties of an endpoint the sound check reports.
type endpointProps struct {
	name         string
	enumerator   string // "BTHENUM", "BTHHFENUM", "USB", "HDAUDIO"...; "" when unknown
	formFactor   uint32
	formFactorOK bool
}

func (p *comProcs) endpointProps(dev unsafe.Pointer) endpointProps {
	var out endpointProps
	p.withStore(dev, func(store unsafe.Pointer) {
		out.name = p.stringProp(store, pkeyDeviceFriendlyName())
		out.enumerator = p.stringProp(store, pkeyDeviceEnumeratorName())
		out.formFactor, out.formFactorOK = p.uint32Prop(store, pkeyAudioEndpointFormFactor())
	})
	return out
}

// withStore runs f with the read-only property store of dev; nothing runs when it cannot be opened.
func (p *comProcs) withStore(dev unsafe.Pointer, f func(store unsafe.Pointer)) {
	if err := p.procPropVariantClear.Find(); err != nil {
		return
	}
	var store unsafe.Pointer
	r, _, _ := syscall.SyscallN(method(dev, slotDeviceOpenPropertyStore), uintptr(dev), stgmRead,
		uintptr(unsafe.Pointer(&store))) //nolint:gosec // G103: COM out-parameter
	store, err := checkOut("IMMDevice::OpenPropertyStore", hr(r), store)
	if err != nil {
		return
	}
	defer release(store)
	f(store)
}

// property reads key into pv and returns the cleanup that frees what Windows allocated; ok is false on a failure.
func (p *comProcs) property(store unsafe.Pointer, key propertyKey, pv *propVariantValue) (clear func(), ok bool) {
	r, _, _ := syscall.SyscallN(method(store, slotStoreGetValue), uintptr(store),
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(pv))) //nolint:gosec // G103: COM parameters
	if hr(r).failed() {
		return func() {}, false
	}
	return func() { _, _, _ = syscall.SyscallN(p.procPropVariantClear.Addr(), uintptr(unsafe.Pointer(pv))) }, true //nolint:gosec // G103: frees the value Windows allocated
}

// stringProp reads a VT_LPWSTR property, bounded and without control characters; "" when it is missing or another type.
func (p *comProcs) stringProp(store unsafe.Pointer, key propertyKey) string {
	var pv propVariantValue
	clear, ok := p.property(store, key, &pv)
	defer clear()
	if !ok || pv.vt != vtLPWStr || pv.val == 0 {
		return ""
	}
	s, ok := utf16At(fromUintptr(pv.val), maxNameCh)
	if !ok {
		return ""
	}
	return shortText(s)
}

// uint32Prop reads a VT_UI4 property: the first four bytes of the value union.
func (p *comProcs) uint32Prop(store unsafe.Pointer, key propertyKey) (uint32, bool) {
	var pv propVariantValue
	clear, ok := p.property(store, key, &pv)
	defer clear()
	if !ok || pv.vt != vtUI4 {
		return 0, false
	}
	return uint32(pv.val), true //nolint:gosec // ulVal is the low 32 bits of the union on little-endian Windows
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

// targetMeter tells whether the target of a process loopback capture plays sound (playing of the endpoint seam).
type targetMeter struct {
	enum   unsafe.Pointer // IMMDeviceEnumerator; released by the endpoint
	target Target
}

// playing reads the meters once: an audible session (active, peak above the floor, not muted or at volume 0 in the
// mixer) that the mode captures, on any active output device, as process loopback captures every device. INCLUDE:
// a session of the target's tree; EXCLUDE: one outside our own tree (audioExpected).
func (m *targetMeter) playing() bool {
	sessions, err := renderSessions(m.enum)
	if err != nil || !anyAudible(sessions) {
		return false // the process snapshot is taken only when some session plays
	}
	procs, err := processes()
	if err != nil {
		return false
	}
	return audioExpected(m.target, newTree(procs), sessions)
}
