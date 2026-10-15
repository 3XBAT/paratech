package audiocapture_test

import (
	"os"
	"testing"

	"paratech/desktop/testenv"
)

// TestMain lets the test binary double as the recorder child: the Recorder tests launch it with the production
// command line (--recorder --data-dir <dir>) and a fake tone source chosen by testenv.EnvFakeRecorder. It is the only
// TestMain of this directory (internal and external tests share a binary).
func TestMain(m *testing.M) {
	if code, ok := testenv.RunFakeRecorderChild(os.Args[1:], os.Getenv); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
