package app

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/testenv"
)

// TestMain lets the test binary double as the fake whisper-cli: the wired whispercli adapter starts os.Executable()
// with whisper-cli arguments. It is the only TestMain of this directory (internal and external tests share a binary).
// A recorder child launched from it by mistake (the production recorder starts os.Executable() --recorder) fails at
// once instead of running the test suite.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--recorder" {
		fmt.Fprintln(os.Stderr, "app tests: the test binary is not a recorder child")
		os.Exit(1)
	}
	if code, ok := testenv.RunFakeWhisper(os.Args[1:]); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// fakes are the parts of the outside world a test drives.
type fakes struct {
	power    *testenv.FakePower
	recorder *testenv.FakeRecorder // set when openCore builds the recording session

	// secrets are the credential stores by service: tests never touch the real Credential Manager.
	secrets map[string]*testenv.MemCredentials
}

// testEnv is the outside world of the core in tests: the real clock (the wiring runs on real goroutines), fake power
// on AC, the fake recorder, the test binary as whisper-cli, in-memory credential stores (empty: the Hub and mail are
// not configured) and a fast single-worker runner.
func testEnv(t *testing.T) (env, *fakes) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakes{power: testenv.NewFakePower(true), secrets: map[string]*testenv.MemCredentials{
		hubSecretsService: testenv.NewMemCredentials(), mailSecretsService: testenv.NewMemCredentials(),
	}}

	runner := jobrunner.DefaultConfig()
	runner.Workers = 1
	runner.PollInterval = 10 * time.Millisecond

	return env{
		lock:   acquireLock,
		clock:  clock.System{},
		local:  clock.Local{},
		ids:    random.IDs{},
		jitter: random.Jitter{},
		power:  f.power,
		recorder: func(_ string, files *filestore.Store, _ *slog.Logger) (recordingsession.Recorder, error) {
			f.recorder = testenv.NewFakeRecorder(files, time.Minute)
			return f.recorder, nil
		},
		whisperBinary: bin,
		secrets: func(service string) (secretStore, error) {
			s, ok := f.secrets[service]
			if !ok {
				return nil, fmt.Errorf("no test credential store for %q", service)
			}
			return s, nil
		},
		runner:      runner,
		recordings:  recordingsession.DefaultConfig(),
		stopTimeout: 5 * time.Second,
	}, f
}
