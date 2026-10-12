package testenv

import (
	"testing"
	"time"
)

// eventuallyLimit bounds the real time a test waits for code running on other goroutines.
const eventuallyLimit = 15 * time.Second

// Eventually polls cond in real time until it holds, running between (if not nil) before each new poll, for example
// to advance a fake clock. It fails the test after a generous limit.
func Eventually(t testing.TB, what string, cond func() bool, between func()) {
	t.Helper()
	deadline := time.Now().Add(eventuallyLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		if between != nil {
			between()
		}
		time.Sleep(time.Millisecond)
	}
}
