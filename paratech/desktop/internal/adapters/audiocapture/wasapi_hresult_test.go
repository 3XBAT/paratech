package audiocapture

import (
	"errors"
	"fmt"
	"testing"
)

func TestHRESULTText(t *testing.T) {
	tests := []struct {
		hr     hresult
		failed bool
		text   string
	}{
		{hrOK, false, "0x00000000 (S_OK)"},
		{hrFalse, false, "0x00000001 (S_FALSE)"},
		{hrBufferEmpty, false, "0x08890001 (AUDCLNT_S_BUFFER_EMPTY)"},
		{hrNotImpl, true, "0x80004001 (E_NOTIMPL)"},
		{hrNoInterface, true, "0x80004002 (E_NOINTERFACE)"},
		{hrPointer, true, "0x80004003 (E_POINTER)"},
		{hrFail, true, "0x80004005 (E_FAIL)"},
		{hrIllegalMethodCall, true, "0x8000000E (E_ILLEGAL_METHOD_CALL)"},
		{hrAccessDenied, true, "0x80070005 (E_ACCESSDENIED)"},
		{hrInvalidArg, true, "0x80070057 (E_INVALIDARG)"},
		{hrNotFound, true, "0x80070490 (ERROR_NOT_FOUND)"},
		{hrClassNotReg, true, "0x80040154 (REGDB_E_CLASSNOTREG)"},
		{hrChangedMode, true, "0x80010106 (RPC_E_CHANGED_MODE)"},
		{hrNotInitialized, true, "0x88890001 (AUDCLNT_E_NOT_INITIALIZED)"},
		{hrAlreadyInitialized, true, "0x88890002 (AUDCLNT_E_ALREADY_INITIALIZED)"},
		{hrWrongEndpointType, true, "0x88890003 (AUDCLNT_E_WRONG_ENDPOINT_TYPE)"},
		{hrDeviceInvalidated, true, "0x88890004 (AUDCLNT_E_DEVICE_INVALIDATED)"},
		{hrUnsupportedFormat, true, "0x88890008 (AUDCLNT_E_UNSUPPORTED_FORMAT)"},
		{hrDeviceInUse, true, "0x8889000A (AUDCLNT_E_DEVICE_IN_USE)"},
		{hrServiceNotRunning, true, "0x88890010 (AUDCLNT_E_SERVICE_NOT_RUNNING)"},
		{hrInvalidStreamFlag, true, "0x88890021 (AUDCLNT_E_INVALID_STREAM_FLAG)"},
		{hrResourcesInvalid, true, "0x88890026 (AUDCLNT_E_RESOURCES_INVALIDATED)"},
		{0x8889FFFF, true, "0x8889FFFF"},
	}
	for _, tt := range tests {
		if tt.hr.failed() != tt.failed || tt.hr.String() != tt.text {
			t.Errorf("%08X: failed=%v String=%q, want %v %q", uint32(tt.hr), tt.hr.failed(), tt.hr.String(), tt.failed, tt.text)
		}
	}
}

// Raw HRESULTs become typed errors (ARCH-32); the raw code stays readable for the diagnostics.
func TestHRESULTMapsToTypedErrors(t *testing.T) {
	tests := []struct {
		hr   hresult
		want error
	}{
		{hrDeviceInvalidated, ErrDeviceLost},
		{hrResourcesInvalid, ErrDeviceLost},
		{hrServiceNotRunning, ErrDeviceLost},
		{hrNotFound, ErrNoDevice},
		{hrAccessDenied, ErrAccessDenied},
		{hrUnsupportedFormat, ErrFormatRejected},
		{hrNotImpl, ErrNotSupported},
		{hrNoInterface, ErrNotSupported},
		{hrClassNotReg, ErrNotSupported},
		{hrIllegalMethodCall, ErrNotSupported},
		{hrFail, ErrDevice},
		{hrDeviceInUse, ErrDevice},
	}
	for _, tt := range tests {
		t.Run(tt.hr.String(), func(t *testing.T) {
			err := fmt.Errorf("outer: %w", hrError("IAudioClient::Initialize", tt.hr))
			if !errors.Is(err, tt.want) || !errors.Is(err, ErrDevice) {
				t.Fatalf("%v: want %v and ErrDevice", err, tt.want)
			}
			if got := hresultText(err); got != tt.hr.hex() {
				t.Fatalf("hresultText = %q, want %q", got, tt.hr.hex())
			}
			if isDeviceLost(err) != (tt.want == ErrDeviceLost) {
				t.Fatalf("isDeviceLost = %v", isDeviceLost(err))
			}
		})
	}

	if hresultText(errors.New("plain")) != "" || isDeviceLost(errors.New("plain")) {
		t.Fatal("a plain error has no HRESULT")
	}
	if err := hrError("IAudioClient::Start", hrDeviceInvalidated); err.Error() !=
		"audiocapture: IAudioClient::Start failed: 0x88890004 (AUDCLNT_E_DEVICE_INVALIDATED)" {
		t.Fatalf("text = %q", err)
	}
	if errors.Is(ErrActivationLimit, ErrDevice) {
		t.Fatal("the activation limit is not a device error")
	}
}
