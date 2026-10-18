//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"context"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"paratech/desktop/internal/domain/recording/values"
)

// PlatformSource returns the WASAPI capture source (ADR-0002): process loopback of the target with classic loopback
// as the one-way fallback, after an activation failure, a failed stream or a healthy-but-mute stream. It opens nothing
// until Open; the recorder child builds one for its whole life.
func PlatformSource(cfg SourceConfig) Source {
	return &wasapiSource{procs: newComProcs(), budget: activationBudget{max: maxProcessActivations}, muteAfter: cfg.muteAfter()}
}

// Supported reports whether this build captures audio.
func Supported() bool { return true }

// wasapiSource opens WASAPI streams. It keeps every object Windows may still reference (completion handlers and
// activation parameters) reachable for its own lifetime, the lifetime of the recorder process.
type wasapiSource struct {
	procs     *comProcs
	budget    activationBudget
	muteAfter time.Duration

	mu   sync.Mutex
	kept []any
}

func (s *wasapiSource) keep(objs ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kept = append(s.kept, objs...)
}

// Open starts the capture of target on its own COM thread.
func (s *wasapiSource) Open(ctx context.Context, target Target, obs Observer) (Stream, error) {
	return openStream(ctx, streamDeps{
		open: s.openEndpoint, thread: comThread, windows: windowsVersion(), now: time.Now, sleep: sleepOrCancel,
		muteAfter: s.muteAfter,
	}, target, obs)
}

// comThread runs body on a new goroutine locked to its own OS thread in the COM MTA and waits for it; COM objects
// are created, used and released there only, and CoUninitialize runs after the last release. Never the caller's
// thread: in the app the caller may be the main thread, which the UI library initialized as an STA, and
// CoInitializeEx there fails with RPC_E_CHANGED_MODE.
func comThread(body func()) error {
	type result struct {
		err      error
		panicked any
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		uninit, err := coInitialize()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer uninit()
		// A panic in body goes back to the caller, whose recover (e.g. the app list) handled it before.
		defer func() {
			if r := recover(); r != nil {
				done <- result{panicked: r}
			}
		}()
		body()
		done <- result{}
	}()
	r := <-done
	if r.panicked != nil {
		panic(r.panicked)
	}
	return r.err
}

// currentVersionKey holds the product texts of Windows; it is only read.
const currentVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// windowsVersion is the Windows of this machine: the numbers from RtlGetVersion (no compatibility shims of
// GetVersionEx), the product texts and the update build revision from the registry. A value that cannot be read
// stays empty; that never fails a capture.
func windowsVersion() values.WindowsVersion {
	v := windows.RtlGetVersion()
	out := values.WindowsVersion{Major: v.MajorVersion, Minor: v.MinorVersion, Build: v.BuildNumber}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, currentVersionKey, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer func() { _ = k.Close() }()
	if s, _, err := k.GetStringValue("ProductName"); err == nil {
		out.Product = shortText(s)
	}
	if s, _, err := k.GetStringValue("DisplayVersion"); err == nil {
		out.DisplayVersion = shortText(s)
	}
	if n, _, err := k.GetIntegerValue("UBR"); err == nil && n <= 1<<32-1 {
		out.UBR = uint32(n)
	}
	return out
}
