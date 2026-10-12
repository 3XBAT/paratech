package testenv

import (
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

// NoGoroutineLeaks fails the test if goroutines started during it are still running when it ends. Call it first, so
// its cleanup runs last. Do not combine it with t.Parallel: it counts all goroutines of the process.
func NoGoroutineLeaks(t testing.TB) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > before {
			if time.Now().After(deadline) {
				var dump strings.Builder
				_ = pprof.Lookup("goroutine").WriteTo(&dump, 1)
				t.Errorf("goroutine leak: %d goroutines before the test, %d after\n%s", before, runtime.NumGoroutine(), dump.String())
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}
