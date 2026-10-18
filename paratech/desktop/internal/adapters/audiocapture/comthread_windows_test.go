//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// The app calls into audio from its main thread, which the UI library initialized as an STA; comThread must not
// reuse the caller's thread (it failed there with RPC_E_CHANGED_MODE).
func TestComThreadWorksFromAnSTAThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		t.Fatalf("CoInitializeEx STA: %v", err)
	}
	defer windows.CoUninitialize()

	ran := false
	if err := comThread(func() { ran = true }); err != nil {
		t.Fatalf("comThread from an STA thread = %v", err)
	}
	if !ran {
		t.Fatal("body did not run")
	}
}

func TestComThreadPanicReachesTheCaller(t *testing.T) {
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want boom", r)
		}
	}()
	_ = comThread(func() { panic("boom") })
	t.Fatal("comThread returned after a panic")
}
