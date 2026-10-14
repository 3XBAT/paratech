package tests

import (
	"os"
	"testing"

	"paratech/desktop/testenv"
)

// TestMain lets the test binary double as the fake whisper-cli of the STT scenarios (transcribe_sqlite_test.go): the
// whispercli adapter starts os.Executable() with whisper-cli arguments.
func TestMain(m *testing.M) {
	if code, ok := testenv.RunFakeWhisper(os.Args[1:]); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
