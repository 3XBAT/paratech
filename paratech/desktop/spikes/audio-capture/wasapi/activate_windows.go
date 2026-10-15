//go:build windows && (amd64 || arm64)

package wasapi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	activationTimeout = 5 * time.Second
	waitStepMS        = 100 // every wait is this short, so a stop request is seen within 100 ms
)

var (
	// ErrActivationTimeout means ActivateAudioInterfaceAsync did not complete in time.
	ErrActivationTimeout = errors.New("wasapi: process loopback activation timed out")

	errStopped = errors.New("wasapi: stopped")
)

// handlerVtbl is the vtable of IActivateAudioInterfaceCompletionHandler: IUnknown, then ActivateCompleted.
type handlerVtbl struct {
	queryInterface    uintptr
	addRef            uintptr
	release           uintptr
	activateCompleted uintptr
}

// handlerVtable is built once per process: syscall.NewCallback slots are never freed and limited (at least 1024
// are guaranteed, 2000 on Windows today), so one vtable is shared by every handler. Read-only after creation.
var handlerVtable = sync.OnceValue(func() *handlerVtbl {
	return &handlerVtbl{
		queryInterface:    windows.NewCallback(handlerQueryInterface),
		addRef:            windows.NewCallback(handlerAddRef),
		release:           windows.NewCallback(handlerRelease),
		activateCompleted: windows.NewCallback(handlerActivateCompleted),
	}
})

// completionHandler is a COM object implemented in Go. It also answers IAgileObject: ActivateAudioInterfaceAsync
// fails with E_ILLEGAL_METHOD_CALL on older builds for a handler that is not agile, and the callback arrives on an
// MTA worker thread. The object lives in Go memory; the opener keeps every handler reachable for its whole life,
// because Windows may call Release after we stopped waiting.
type completionHandler struct {
	vtbl *handlerVtbl // first field: COM reads the vtable pointer at offset 0
	refs atomic.Int32
	done windows.Handle // manual-reset event set by ActivateCompleted
}

// fromUintptr turns an address received from Windows back into a pointer without the uintptr-to-Pointer
// conversion vet rejects; the memory is either Windows-owned or a handler kept reachable by the opener.
func fromUintptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } //nolint:gosec // G103: see above

func handlerOf(this uintptr) *completionHandler { return (*completionHandler)(fromUintptr(this)) }

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 || riid == 0 {
		return uintptr(hrPointer)
	}
	// Store through *uintptr: a typed pointer store into Windows memory would run the GC write barrier on it.
	out := (*uintptr)(fromUintptr(ppv))
	switch *(*windows.GUID)(fromUintptr(riid)) {
	case iidIUnknown, iidIAgileObject, iidICompletionHandler:
		handlerOf(this).refs.Add(1)
		*out = this
		return uintptr(hrOK)
	}
	*out = 0
	return uintptr(hrNoInterface)
}

func handlerAddRef(this uintptr) uintptr { return uintptr(handlerOf(this).refs.Add(1)) } //nolint:gosec // count is small and positive

func handlerRelease(this uintptr) uintptr {
	n := handlerOf(this).refs.Add(-1)
	if n < 0 {
		n = 0
	}
	return uintptr(n)
}

func handlerActivateCompleted(this, _ uintptr) uintptr {
	_ = windows.SetEvent(handlerOf(this).done)
	return uintptr(hrOK)
}

// activateProcessLoopback activates an IAudioClient for process loopback of spec and waits for completion,
// checking stop every waitStepMS.
func (o *opener) activateProcessLoopback(spec Spec, stop <-chan struct{}) (unsafe.Pointer, error) {
	mode, ok := spec.Mode.processLoopbackMode()
	if !ok {
		return nil, fmt.Errorf("%w: %s is not process loopback", ErrInvalidSpec, spec.Mode)
	}

	done, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("wasapi: CreateEvent: %w", err)
	}
	h := &completionHandler{vtbl: handlerVtable(), done: done}
	h.refs.Store(1)
	o.keep(h)

	params := &activationParams{activationType: activationTypeProcessLoopback, targetProcessID: spec.PID, processLoopbackMode: mode}
	pv := &propVariant{vt: vtBlob, blobSize: uint32(unsafe.Sizeof(*params)), blobData: params}
	path, err := windows.UTF16PtrFromString(virtualAudioDeviceProcessLoopback)
	if err != nil {
		_ = windows.CloseHandle(done)
		return nil, err
	}

	op, err := activateAudioInterfaceAsync(path, pv, h)
	if err != nil {
		_ = windows.CloseHandle(done)
		return nil, err
	}
	defer release(op)
	o.keep(params, pv) // Windows may read the parameters asynchronously

	deadline := time.Now().Add(activationTimeout)
	for {
		ev, err := windows.WaitForSingleObject(done, waitStepMS)
		if err != nil {
			return nil, fmt.Errorf("wasapi: wait for activation: %w", err)
		}
		if ev == windows.WAIT_OBJECT_0 {
			break
		}
		// On timeout or stop the event stays open: ActivateCompleted may still run and set it.
		select {
		case <-stop:
			return nil, errStopped
		default:
		}
		if time.Now().After(deadline) {
			return nil, ErrActivationTimeout
		}
	}
	_ = windows.CloseHandle(done)

	unk, err := getActivateResult(op)
	if err != nil {
		return nil, err
	}
	defer release(unk)
	return queryInterface(unk, &iidIAudioClient)
}
