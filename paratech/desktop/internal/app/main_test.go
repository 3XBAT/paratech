package app

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/testenv"
)

// testProfile is the Credential Manager profile of the cores a test builds directly: the stores are fakes.
const testProfile wincred.Profile = "0123456789abcdef"

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
	recorder *testenv.FakeRecorder  // set when openCore builds the recording session
	apps     *testenv.FakeAudioApps // the open apps of the source picker

	// secrets are the credential stores by service: tests never touch the real Credential Manager.
	secrets map[string]*testenv.MemCredentials
	// profiles are the Credential Manager profiles the core asked the stores of, by service.
	profiles map[string]wincred.Profile

	// browser opens Google's consent page; the Google endpoints and the Calendar API point to a closed local port
	// unless a test sets env.googleOAuth or env.googleCalendar to a fake server, so no test reaches Google.
	browser *testenv.FakeBrowser
}

// testEnv is the outside world of the core in tests: the real clock (the wiring runs on real goroutines), fake power
// on AC, the fake recorder and open apps, the test binary as whisper-cli, in-memory credential stores (empty: the Hub and mail are
// not configured) and a fast single-worker runner.
func testEnv(t *testing.T) (env, *fakes) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakes{power: testenv.NewFakePower(true), apps: &testenv.FakeAudioApps{}, secrets: map[string]*testenv.MemCredentials{
		hubSecretsService: testenv.NewMemCredentials(), mailSecretsService: testenv.NewMemCredentials(),
		googleSecretsService: testenv.NewMemCredentials(),
	}, profiles: map[string]wincred.Profile{}, browser: testenv.NewFakeBrowser()}
	const nowhere = "http://127.0.0.1:1"

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
		apps:   func(*slog.Logger) recordingprompt.AudioApps { return f.apps },
		speech: testSpeechEnv(t, bin),
		secrets: func(profile wincred.Profile, service string) (secretStore, error) {
			s, ok := f.secrets[service]
			if !ok {
				return nil, fmt.Errorf("no test credential store for %q", service)
			}
			f.profiles[service] = profile
			return s, nil
		},
		authPages:      f.browser,
		googleOAuth:    []googleauth.Option{googleauth.WithEndpoints(nowhere+"/auth", nowhere+"/token", nowhere+"/revoke")},
		googleCalendar: []googlecal.Option{googlecal.WithBaseURL(nowhere + "/calendar/v3")},
		runner:         runner,
		recordings:     recordingsession.DefaultConfig(),
		stopTimeout:    5 * time.Second,
	}, f
}
