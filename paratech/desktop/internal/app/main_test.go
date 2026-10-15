package app

import (
	"fmt"
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
func TestMain(m *testing.M) {
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
		recorder: func(files *filestore.Store) recordingsession.Recorder {
			f.recorder = testenv.NewFakeRecorder(files, time.Minute)
			return f.recorder
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
