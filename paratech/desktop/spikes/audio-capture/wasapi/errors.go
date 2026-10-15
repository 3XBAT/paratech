package wasapi

import (
	"errors"
	"fmt"
)

// HRESULT is a COM status code; negative values (high bit set) are failures.
type HRESULT uint32

// HRESULT values used by the capture code; all but E_ILLEGAL_METHOD_CALL confirmed in the mingw-w64 headers (winerror.h, audioclient.h:
// AUDCLNT_ERR(n) = MAKE_HRESULT(1, FACILITY_AUDCLNT = 2185 = 0x889, n)).
const (
	hrOK                 HRESULT = 0x00000000
	hrFalse              HRESULT = 0x00000001 // S_FALSE
	hrNotImpl            HRESULT = 0x80004001 // E_NOTIMPL
	hrNoInterface        HRESULT = 0x80004002 // E_NOINTERFACE
	hrPointer            HRESULT = 0x80004003 // E_POINTER
	hrFail               HRESULT = 0x80004005 // E_FAIL
	hrAccessDenied       HRESULT = 0x80070005 // E_ACCESSDENIED
	hrInvalidArg         HRESULT = 0x80070057 // E_INVALIDARG
	hrNotFound           HRESULT = 0x80070490 // HRESULT_FROM_WIN32(ERROR_NOT_FOUND), no default endpoint
	hrClassNotReg        HRESULT = 0x80040154 // REGDB_E_CLASSNOTREG
	hrChangedMode        HRESULT = 0x80010106 // RPC_E_CHANGED_MODE
	hrNotInitialized     HRESULT = 0x88890001 // AUDCLNT_E_NOT_INITIALIZED
	hrAlreadyInitialized HRESULT = 0x88890002 // AUDCLNT_E_ALREADY_INITIALIZED
	hrWrongEndpointType  HRESULT = 0x88890003 // AUDCLNT_E_WRONG_ENDPOINT_TYPE
	hrDeviceInvalidated  HRESULT = 0x88890004 // AUDCLNT_E_DEVICE_INVALIDATED
	hrUnsupportedFormat  HRESULT = 0x88890008 // AUDCLNT_E_UNSUPPORTED_FORMAT
	hrDeviceInUse        HRESULT = 0x8889000A // AUDCLNT_E_DEVICE_IN_USE
	hrServiceNotRunning  HRESULT = 0x88890010 // AUDCLNT_E_SERVICE_NOT_RUNNING
	hrInvalidStreamFlag  HRESULT = 0x88890021 // AUDCLNT_E_INVALID_STREAM_FLAG
	hrResourcesInvalid   HRESULT = 0x88890026 // AUDCLNT_E_RESOURCES_INVALIDATED
	hrBufferEmpty        HRESULT = 0x08890001 // AUDCLNT_S_BUFFER_EMPTY (success)
	hrIllegalMethodCall  HRESULT = 0x8000000E // E_ILLEGAL_METHOD_CALL (not in mingw winerror.h; documented by Microsoft)
)

// Failed reports whether h is a failure code.
func (h HRESULT) Failed() bool { return int32(h) < 0 } //nolint:gosec // reinterpreting the sign bit is the definition of FAILED()

func (h HRESULT) String() string {
	if name := h.name(); name != "" {
		return fmt.Sprintf("0x%08X (%s)", uint32(h), name)
	}
	return fmt.Sprintf("0x%08X", uint32(h))
}

func (h HRESULT) name() string {
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
	case hrIllegalMethodCall:
		return "E_ILLEGAL_METHOD_CALL"
	}
	return ""
}

// Error is a failed COM call. Op names the call ("IAudioClient::Initialize"), HR keeps the raw code.
type Error struct {
	Op string
	HR HRESULT
}

func (e *Error) Error() string { return fmt.Sprintf("wasapi: %s failed: %s", e.Op, e.HR) }

func hrError(op string, hr HRESULT) error { return &Error{Op: op, HR: hr} }

// HRESULTOf returns the HRESULT of the first *Error in err's chain.
func HRESULTOf(err error) (HRESULT, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.HR, true
	}
	return 0, false
}

// isDeviceLost reports whether the stream must be reopened: the endpoint went away or the audio service restarted.
func isDeviceLost(err error) bool {
	hr, ok := HRESULTOf(err)
	return ok && (hr == hrDeviceInvalidated || hr == hrResourcesInvalid || hr == hrServiceNotRunning)
}
