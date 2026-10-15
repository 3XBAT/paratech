package audiocapture

import (
	"errors"
	"fmt"
)

// Typed errors of the WASAPI source (ARCH-32): every failed COM call unwraps to ErrDevice and to the class of its
// HRESULT; the raw code stays in the text and in Attempt.HResult.
var (
	// ErrDevice is any failure of the audio device or of the Windows audio API.
	ErrDevice = errors.New("audiocapture: audio device error")

	// ErrDeviceLost means the stream must be reopened: the device went away or the audio service restarted.
	ErrDeviceLost = fmt.Errorf("%w: the device was lost", ErrDevice)

	// ErrNoDevice means there is no output device to capture.
	ErrNoDevice = fmt.Errorf("%w: no output device", ErrDevice)

	// ErrAccessDenied means Windows refused the capture (for example the privacy settings).
	ErrAccessDenied = fmt.Errorf("%w: access denied", ErrDevice)

	// ErrFormatRejected means the device format cannot be used or a requested format was refused.
	ErrFormatRejected = fmt.Errorf("%w: format rejected", ErrDevice)

	// ErrNotSupported means this Windows build lacks the API (process loopback before build 19041, for example).
	ErrNotSupported = fmt.Errorf("%w: not supported by this Windows", ErrDevice)

	// ErrActivationTimeout means the process loopback activation did not complete in time.
	ErrActivationTimeout = fmt.Errorf("%w: process loopback activation timed out", ErrDevice)

	// ErrActivationLimit means the recorder process already used its process loopback activation.
	ErrActivationLimit = errors.New("audiocapture: process loopback was already activated in this process")
)

// hresult is a COM status code; negative values (high bit set) are failures.
type hresult uint32

// HRESULT values used by the capture code, from the mingw-w64 headers (winerror.h; audioclient.h:
// AUDCLNT_ERR(n) = MAKE_HRESULT(1, FACILITY_AUDCLNT = 0x889, n)); E_ILLEGAL_METHOD_CALL is documented by Microsoft.
const (
	hrOK                 hresult = 0x00000000
	hrFalse              hresult = 0x00000001 // S_FALSE
	hrNotImpl            hresult = 0x80004001 // E_NOTIMPL
	hrNoInterface        hresult = 0x80004002 // E_NOINTERFACE
	hrPointer            hresult = 0x80004003 // E_POINTER
	hrFail               hresult = 0x80004005 // E_FAIL
	hrIllegalMethodCall  hresult = 0x8000000E // E_ILLEGAL_METHOD_CALL
	hrAccessDenied       hresult = 0x80070005 // E_ACCESSDENIED
	hrInvalidArg         hresult = 0x80070057 // E_INVALIDARG
	hrNotFound           hresult = 0x80070490 // HRESULT_FROM_WIN32(ERROR_NOT_FOUND): no default endpoint
	hrClassNotReg        hresult = 0x80040154 // REGDB_E_CLASSNOTREG
	hrChangedMode        hresult = 0x80010106 // RPC_E_CHANGED_MODE
	hrNotInitialized     hresult = 0x88890001 // AUDCLNT_E_NOT_INITIALIZED
	hrAlreadyInitialized hresult = 0x88890002 // AUDCLNT_E_ALREADY_INITIALIZED
	hrWrongEndpointType  hresult = 0x88890003 // AUDCLNT_E_WRONG_ENDPOINT_TYPE
	hrDeviceInvalidated  hresult = 0x88890004 // AUDCLNT_E_DEVICE_INVALIDATED
	hrUnsupportedFormat  hresult = 0x88890008 // AUDCLNT_E_UNSUPPORTED_FORMAT
	hrDeviceInUse        hresult = 0x8889000A // AUDCLNT_E_DEVICE_IN_USE
	hrServiceNotRunning  hresult = 0x88890010 // AUDCLNT_E_SERVICE_NOT_RUNNING
	hrInvalidStreamFlag  hresult = 0x88890021 // AUDCLNT_E_INVALID_STREAM_FLAG
	hrResourcesInvalid   hresult = 0x88890026 // AUDCLNT_E_RESOURCES_INVALIDATED
	hrBufferEmpty        hresult = 0x08890001 // AUDCLNT_S_BUFFER_EMPTY (success)
)

// failed reports whether h is a failure code.
func (h hresult) failed() bool { return int32(h) < 0 } //nolint:gosec // reinterpreting the sign bit is the definition of FAILED()

func (h hresult) hex() string { return fmt.Sprintf("0x%08X", uint32(h)) }

func (h hresult) String() string {
	if name := h.name(); name != "" {
		return h.hex() + " (" + name + ")"
	}
	return h.hex()
}

func (h hresult) name() string {
	switch h {
	case hrOK:
		return "S_OK"
	case hrFalse:
		return "S_FALSE"
	case hrNotImpl:
		return "E_NOTIMPL"
	case hrNoInterface:
		return "E_NOINTERFACE"
	case hrPointer:
		return "E_POINTER"
	case hrFail:
		return "E_FAIL"
	case hrIllegalMethodCall:
		return "E_ILLEGAL_METHOD_CALL"
	case hrAccessDenied:
		return "E_ACCESSDENIED"
	case hrInvalidArg:
		return "E_INVALIDARG"
	case hrNotFound:
		return "ERROR_NOT_FOUND"
	case hrClassNotReg:
		return "REGDB_E_CLASSNOTREG"
	case hrChangedMode:
		return "RPC_E_CHANGED_MODE"
	case hrNotInitialized:
		return "AUDCLNT_E_NOT_INITIALIZED"
	case hrAlreadyInitialized:
		return "AUDCLNT_E_ALREADY_INITIALIZED"
	case hrWrongEndpointType:
		return "AUDCLNT_E_WRONG_ENDPOINT_TYPE"
	case hrDeviceInvalidated:
		return "AUDCLNT_E_DEVICE_INVALIDATED"
	case hrUnsupportedFormat:
		return "AUDCLNT_E_UNSUPPORTED_FORMAT"
	case hrDeviceInUse:
		return "AUDCLNT_E_DEVICE_IN_USE"
	case hrServiceNotRunning:
		return "AUDCLNT_E_SERVICE_NOT_RUNNING"
	case hrInvalidStreamFlag:
		return "AUDCLNT_E_INVALID_STREAM_FLAG"
	case hrResourcesInvalid:
		return "AUDCLNT_E_RESOURCES_INVALIDATED"
	case hrBufferEmpty:
		return "AUDCLNT_S_BUFFER_EMPTY"
	}
	return ""
}

// class maps a failure code to its typed error.
func (h hresult) class() error {
	switch h {
	case hrDeviceInvalidated, hrResourcesInvalid, hrServiceNotRunning:
		return ErrDeviceLost
	case hrNotFound:
		return ErrNoDevice
	case hrAccessDenied:
		return ErrAccessDenied
	case hrUnsupportedFormat:
		return ErrFormatRejected
	case hrNotImpl, hrNoInterface, hrClassNotReg, hrIllegalMethodCall:
		return ErrNotSupported
	}
	return ErrDevice
}

// comError is a failed COM call: op names the call, hr keeps the raw code.
type comError struct {
	op string
	hr hresult
}

func (e *comError) Error() string { return fmt.Sprintf("audiocapture: %s failed: %s", e.op, e.hr) }

// Unwrap returns the typed class, so callers use errors.Is with the exported errors.
func (e *comError) Unwrap() error { return e.hr.class() }

func hrError(op string, hr hresult) error { return &comError{op: op, hr: hr} }

// hresultText returns the HRESULT of the first COM error in err's chain as hex, or "" when there is none.
func hresultText(err error) string {
	var e *comError
	if errors.As(err, &e) {
		return e.hr.hex()
	}
	return ""
}

// isDeviceLost reports whether err means that the stream must be reopened.
func isDeviceLost(err error) bool { return errors.Is(err, ErrDeviceLost) }
