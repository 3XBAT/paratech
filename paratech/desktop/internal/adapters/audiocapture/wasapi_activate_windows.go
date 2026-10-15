//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// activationTimeout bounds a process loopback activation (ADR-0002 Decision 3).
const activationTimeout = 5 * time.Second

// handlerVtbl is the vtable of IActivateAudioInterfaceCompletionHandler: IUnknown, then ActivateCompleted.
type handlerVtbl struct {
	queryInterface    uintptr
	addRef            uintptr
	release           uintptr
	activateCompleted uintptr
}

// newHandlerVtbl builds the vtable. syscall.NewCallback slots are never freed, but the runtime registers each function
// once and returns the same slot afterwards, so building it again uses no new slot.
func newHandlerVtbl() *handlerVtbl {
	return &handlerVtbl{
		queryInterface:    windows.NewCallback(handlerQueryInterface),
		addRef:            windows.NewCallback(handlerAddRef),
		release:           windows.NewCallback(handlerRelease),
		activateCompleted: windows.NewCallback(handlerActivateCompleted),
	}
}

// completionHandler is a COM object implemented in Go, part of an activation. It also answers IAgileObject:
// ActivateAudioInterfaceAsync fails with E_ILLEGAL_METHOD_CALL on older builds for a handler that is not agile, and the
// callback arrives on an MTA worker thread.
type completionHandler struct {
	vtbl *handlerVtbl // first field: COM reads the vtable pointer at offset 0
	refs atomic.Int32
	done windows.Handle // manual-reset event set by ActivateCompleted
}

// fromUintptr turns an address received from Windows back into a pointer without the uintptr-to-Pointer conversion
// vet rejects; the memory is either Windows-owned or an activation kept reachable by the source.
func fromUintptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } //nolint:gosec // G103: see above

func handlerOf(this uintptr) *completionHandler { return (*completionHandler)(fromUintptr(this)) }

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 || riid == 0 {
		return uintptr(hrPointer)
	}
	// Store through *uintptr: a typed pointer store into Windows memory would run the GC write barrier on it.
	out := (*uintptr)(fromUintptr(ppv))
	switch *(*windows.GUID)(fromUintptr(riid)) {
	case iidIUnknown(), iidIAgileObject(), iidICompletionHandler():
		handlerOf(this).refs.Add(1)
		*out = this
		return uintptr(hrOK)
	}
	*out = 0
	return uintptr(hrNoInterface)
}

func handlerAddRef(this uintptr) uintptr { return uintptr(handlerOf(this).refs.Add(1)) } //nolint:gosec // the count is small and positive

func handlerRelease(this uintptr) uintptr {
	n := max(handlerOf(this).refs.Add(-1), 0)
	return uintptr(n)
}

func handlerActivateCompleted(this, _ uintptr) uintptr {
	_ = windows.SetEvent(handlerOf(this).done)
	return uintptr(hrOK)
}

// activation is everything ActivateAudioInterfaceAsync gets a pointer to. It lives on the heap, which the GC does not
// move, and the source keeps it reachable until the process ends: Windows may read the parameters and call the handler
// after we stopped waiting.
type activation struct {
	handler completionHandler
	params  activationParams
	pv      propVariant
	iid     windows.GUID
	path    []uint16
	op      unsafe.Pointer // out-parameter: the IActivateAudioInterfaceAsyncOperation
}

// activateProcessLoopback activates an IAudioClient for process loopback of target and waits up to
// activationTimeout, checking cancel every waitStep. It uses the activation budget of the process: the activation
// and, after a timeout, its event stay alive until the process ends.
func (s *wasapiSource) activateProcessLoopback(target Target, cancel <-chan struct{}) (unsafe.Pointer, error) {
	mode := uint32(processLoopbackModeInclude)
	if target.Mode == ModeExclude {
		mode = processLoopbackModeExclude
	}
	if err := s.budget.take(); err != nil {
		return nil, err
	}

	path, err := windows.UTF16FromString(virtualAudioDeviceProcessLoopback)
	if err != nil {
		return nil, err
	}
	done, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: CreateEvent: %v", ErrDevice, err)
	}
	// No AUDCLNT_STREAMOPTIONS_POST_VOLUME_LOOPBACK anywhere: the default pre-volume tap keeps the Windows volume
	// and speaker mute out of the recording (ADR-0002 Known limits).
	a := &activation{
		handler: completionHandler{vtbl: newHandlerVtbl(), done: done},
		params:  activationParams{activationType: activationTypeProcessLoopback, targetProcessID: target.PID, processLoopbackMode: mode},
		iid:     iidIAudioClient(),
		path:    path,
	}
	a.handler.refs.Store(1)
	a.pv = propVariant{vt: vtBlob, blobSize: uint32(unsafe.Sizeof(a.params)), blobData: &a.params}
	s.keep(a)

	op, err := s.procs.activateAudioInterfaceAsync(a)
	if err != nil {
		_ = windows.CloseHandle(done) // the call failed: no completion will come
		return nil, err
	}
	defer release(op)

	deadline := time.Now().Add(activationTimeout)
	for {
		// On a stop, a timeout or a failed wait the event stays open: ActivateCompleted may still run and set it.
		ev, err := windows.WaitForSingleObject(done, uint32(waitStep/time.Millisecond))
		if err != nil {
			return nil, fmt.Errorf("%w: wait for the activation: %v", ErrDevice, err)
		}
		if ev == windows.WAIT_OBJECT_0 {
			break
		}
		select {
		case <-cancel:
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
	return queryInterface(unk, iidIAudioClient())
}
