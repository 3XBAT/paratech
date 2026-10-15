//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// PlatformSource returns the WASAPI capture source (ADR-0002): process loopback of the target with classic loopback
// as the one-way fallback. It opens nothing until Open; the recorder child builds one for its whole life.
func PlatformSource() Source {
	return &wasapiSource{procs: newComProcs(), budget: activationBudget{max: maxProcessActivations}}
}

// Supported reports whether this build captures audio.
func Supported() bool { return true }

// wasapiSource opens WASAPI streams. It keeps every object Windows may still reference (completion handlers and
// activation parameters) reachable for its own lifetime, the lifetime of the recorder process.
type wasapiSource struct {
	procs  *comProcs
	budget activationBudget

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
		open: s.openEndpoint, thread: comThread, build: windowsBuild(), now: time.Now, sleep: sleepOrCancel,
	}, target, obs)
}

// comThread runs body on a locked OS thread in the COM MTA; COM objects are created, used and released there only,
// and CoUninitialize runs after the last release.
func comThread(body func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninit, err := coInitialize()
	if err != nil {
		return err
	}
	defer uninit()
	body()
	return nil
}

// windowsBuild is the Windows version as major.minor.build, without the compatibility shims of GetVersionEx.
func windowsBuild() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
