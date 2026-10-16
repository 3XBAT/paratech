//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Go mirrors of the C structures passed to Windows have the 64-bit ABI layout.
func TestCOMStructLayout(t *testing.T) {
	var (
		pv propVariant
		ap activationParams
		a  activation
	)
	tests := []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(PROPVARIANT)", unsafe.Sizeof(pv), 24},
		{"PROPVARIANT.vt", unsafe.Offsetof(pv.vt), 0},
		{"PROPVARIANT.blob.cbSize", unsafe.Offsetof(pv.blobSize), 8},
		{"PROPVARIANT.blob.pBlobData", unsafe.Offsetof(pv.blobData), 16},
		{"sizeof(AUDIOCLIENT_ACTIVATION_PARAMS)", unsafe.Sizeof(ap), 12},
		{"ActivationType", unsafe.Offsetof(ap.activationType), 0},
		{"TargetProcessId", unsafe.Offsetof(ap.targetProcessID), 4},
		{"ProcessLoopbackMode", unsafe.Offsetof(ap.processLoopbackMode), 8},
		{"sizeof(GUID)", unsafe.Sizeof(windows.GUID{}), 16},
		{"handler vtable pointer", unsafe.Offsetof(a.handler.vtbl), 0},
		{"handler vtable: IUnknown + ActivateCompleted", unsafe.Sizeof(handlerVtbl{}), 4 * unsafe.Sizeof(uintptr(0))},
		{"sizeof(PROPERTYKEY)", unsafe.Sizeof(propertyKey{}), 20},
		{"PROPERTYKEY.pid", unsafe.Offsetof(propertyKey{}.pid), 16},
		{"sizeof(PROPVARIANT) read back", unsafe.Sizeof(propVariantValue{}), 24},
		{"PROPVARIANT.pwszVal", unsafe.Offsetof(propVariantValue{}.val), 8},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if activationTypeProcessLoopback != 1 || processLoopbackModeInclude != 0 || processLoopbackModeExclude != 1 || vtBlob != 65 {
		t.Error("activation constants differ from the documented values")
	}
}

// The GUIDs match their registry form (docs/refs/audio.md).
func TestCOMGUIDs(t *testing.T) {
	tests := []struct {
		name string
		got  windows.GUID
		want string
	}{
		{"CLSID_MMDeviceEnumerator", clsidMMDeviceEnumerator(), "{BCDE0395-E52F-467C-8E3D-C4579291692E}"},
		{"IID_IMMDeviceEnumerator", iidIMMDeviceEnumerator(), "{A95664D2-9614-4F35-A746-DE8DB63617E6}"},
		{"IID_IAudioClient", iidIAudioClient(), "{1CB9AD4C-DBFA-4C32-B178-C2F568A703B2}"},
		{"IID_IAudioCaptureClient", iidIAudioCaptureClient(), "{C8ADBD64-E71E-48A0-A4DE-185C395CD317}"},
		{"IID_IAudioRenderClient", iidIAudioRenderClient(), "{F294ACFC-3146-4483-A7BF-ADDCA7C260E2}"},
		{"IID_IUnknown", iidIUnknown(), "{00000000-0000-0000-C000-000000000046}"},
		{"IID_IAgileObject", iidIAgileObject(), "{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}"},
		{"IID_IActivateAudioInterfaceCompletionHandler", iidICompletionHandler(), "{41D949AB-9862-444A-80F6-C261334DA5EB}"},
		{"IID_IAudioMeterInformation", iidIAudioMeterInformation(), "{C02216F6-8C67-4B5B-9D00-D008E73E0064}"},
		{"IID_IPropertyStore", iidIPropertyStore(), "{886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}"},
		{"PKEY_Device_FriendlyName", pkeyDeviceFriendlyName().fmtid, "{A45C254E-DF1C-4EFD-8020-67D146A850E0}"},
	}
	if pkeyDeviceFriendlyName().pid != 14 {
		t.Error("PKEY_Device_FriendlyName pid differs from functiondiscoverykeys_devpkey.h")
	}
	for _, tt := range tests {
		if got := strings.ToUpper(tt.got.String()); got != tt.want {
			t.Errorf("%s = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// The completion handler, called through its vtable as Windows calls it, answers the interfaces it implements and
// counts its references; nothing is activated. The arguments live in a heap activation, as in production: the Go
// callback may move the goroutine stack.
func TestCompletionHandler(t *testing.T) {
	done, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(done) }()
	src := PlatformSource(DefaultSourceConfig()).(*wasapiSource)
	a := &activation{handler: completionHandler{vtbl: newHandlerVtbl(), done: done}}
	a.handler.refs.Store(1)
	src.keep(a)
	this := uintptr(unsafe.Pointer(&a.handler))
	vt := a.handler.vtbl

	queryInterface := func(iid windows.GUID) (hresult, uintptr) {
		a.iid, a.op = iid, unsafe.Pointer(&a.params) // any non-nil value: it must be overwritten
		r, _, _ := syscall.SyscallN(vt.queryInterface, this, uintptr(unsafe.Pointer(&a.iid)), uintptr(unsafe.Pointer(&a.op)))
		return hr(r), uintptr(a.op)
	}
	for _, iid := range []windows.GUID{iidIUnknown(), iidIAgileObject(), iidICompletionHandler()} {
		if got, out := queryInterface(iid); got != hrOK || out != this {
			t.Fatalf("QueryInterface(%s) = %s, %x", iid, got, out)
		}
	}
	if got, out := queryInterface(iidIAudioClient()); got != hrNoInterface || out != 0 {
		t.Fatalf("QueryInterface(IAudioClient) = %s, %x", got, out)
	}
	if r, _, _ := syscall.SyscallN(vt.queryInterface, this, 0, 0); hr(r) != hrPointer {
		t.Fatalf("QueryInterface(NULL) = %s", hr(r))
	}

	if n, _, _ := syscall.SyscallN(vt.addRef, this); n != 5 {
		t.Fatalf("AddRef = %d, want 5", n)
	}
	for range 10 {
		_, _, _ = syscall.SyscallN(vt.release, this)
	}
	if n, _, _ := syscall.SyscallN(vt.release, this); n != 0 || a.handler.refs.Load() > 0 {
		t.Fatalf("Release left %d references", a.handler.refs.Load())
	}

	_, _, _ = syscall.SyscallN(vt.activateCompleted, this, 0)
	if ev, err := windows.WaitForSingleObject(done, 0); err != nil || ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("ActivateCompleted did not set the event: %d, %v", ev, err)
	}
}

// The version of this machine is read without a capture: RtlGetVersion and the registry, nothing is written.
func TestWindowsVersion(t *testing.T) {
	v := windowsVersion()
	if v.Major != 10 || v.Build < 10240 || !strings.HasPrefix(v.BuildString(), "10.0.") || !strings.HasPrefix(v.Name(), "Windows") {
		t.Fatalf("windowsVersion = %+v (%s), want Windows 10 or 11", v, v)
	}
	if v.IsWindows11() && strings.HasPrefix(v.Product, "Windows 10") && !strings.HasPrefix(v.Name(), "Windows 11") {
		t.Fatalf("Windows 11 detection: %+v, %q", v, v.Name())
	}
	t.Logf("windows %s", v)
}

// The second process loopback activation of a process is refused before Windows is called.
func TestSourceRefusesASecondActivation(t *testing.T) {
	s := PlatformSource(DefaultSourceConfig()).(*wasapiSource)
	s.budget.used.Store(maxProcessActivations) // as if the first activation had run
	if _, err := s.activateProcessLoopback(Target{Mode: ModeExclude, PID: 1}, nil); !errors.Is(err, ErrActivationLimit) {
		t.Fatalf("activation = %v, want ErrActivationLimit", err)
	}
	if len(s.kept) != 0 {
		t.Fatal("a refused activation kept objects")
	}
}
