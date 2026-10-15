package tests

import (
	"os"
	"testing"

	"paratech/desktop/testenv"
)

// TestMain lets the test binary double as the fake whisper-cli of the STT scenarios (transcribe_sqlite_test.go): the
// whispercli adapter starts os.Executable() with whisper-cli arguments. It also doubles as the recorder child of the
// recording scenario with the real recorder (recording_capture_test.go).
func TestMain(m *testing.M) {
	if code, ok := testenv.RunFakeWhisper(os.Args[1:]); ok {
		os.Exit(code)
	}
	if code, ok := testenv.RunFakeRecorderChild(os.Args[1:], os.Getenv); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
