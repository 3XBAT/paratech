package localhub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/localhub/values"
)

func TestNewRequiresEveryPort(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureDoesNotStartWhenSomethingIsMissing(t *testing.T) {
	tests := []struct {
		name    string
		facts   func(*values.Facts)
		secrets []values.SecretName
		reason  values.Reason
		next    values.NextStep
		exit    int
	}{
		{name: "no hub.exe", facts: func(f *values.Facts) { f.HubExe = false }, reason: values.ReasonNoHubExe, next: values.NextRebuildBeta},
		{name: "no provider key", secrets: []values.SecretName{values.SecretDatabaseURL}, reason: values.ReasonNoProviderKey, next: values.NextEnterProviderKey},
		{name: "no database url", secrets: []values.SecretName{values.SecretProviderKey}, reason: values.ReasonNoDatabaseURL, next: values.NextEnterDatabaseURL},
		{name: "docker off", facts: func(f *values.Facts) { f.DockerEngine, f.DatabasePort = false, false }, reason: values.ReasonDockerOff, next: values.NextStartDocker},
		{name: "database down", facts: func(f *values.Facts) { f.DatabasePort = false }, reason: values.ReasonDatabaseDown, next: values.NextStartDatabase},
		{name: "port busy", facts: func(f *values.Facts) { f.Listener = values.ListenerForeign }, reason: values.ReasonPortBusy, next: values.NextFreePort},
		{name: "exited before", facts: func(f *values.Facts) { f.Exited, f.ExitCode = true, 1 }, reason: values.ReasonExited, next: values.NextOpenLogs, exit: 1},
		{name: "config error before", facts: func(f *values.Facts) { f.Exited, f.ExitCode = true, 2 }, reason: values.ReasonConfigError, next: values.NextCheckSettings, exit: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, localURL)
			f := readyFacts()
			if tt.facts != nil {
				tt.facts(&f)
			}
			w.probe.set(f)
			if tt.secrets != nil {
				w.secrets.values = newSecrets(tt.secrets...).values
			}

			st, err := w.svc.Ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := Status{State: values.StatusNotRunning, Reason: tt.reason, Next: tt.next, ExitCode: tt.exit, Port: 8080, URL: localURL}
			if st != want {
				t.Fatalf("status = %+v, want %+v", st, want)
			}
			if n := w.launcher.startCount(); n != 0 {
				t.Fatalf("started %d times", n)
			}
			if w.secrets.generatedCount() != 0 {
				t.Fatal("seal keys generated without a start")
			}
			if got, _ := w.svc.Status(context.Background()); got != want {
				t.Fatalf("cached status = %+v", got)
			}
		})
	}
}

func TestEnsureLeavesAnAnsweringHubAlone(t *testing.T) {
	degraded := readyFacts()
	degraded.Healthz, degraded.DatabasePort = true, false
	tests := []struct {
		name  string
		facts values.Facts
		want  Status
	}{
		{name: "running", facts: answering(), want: Status{State: values.StatusRunning, Port: 8080, URL: localURL}},
		{name: "a hand-run hub without our secrets", facts: values.Facts{Healthz: true, Readyz: true}, want: Status{State: values.StatusRunning, Port: 8080, URL: localURL}},
		{name: "degraded", facts: degraded, want: Status{State: values.StatusDegraded, Reason: values.ReasonDatabaseDown, Next: values.NextStartDatabase, Port: 8080, URL: localURL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, localURL)
			w.probe.set(tt.facts)
			for range 2 {
				st, err := w.svc.Ensure(context.Background())
				if err != nil || st != tt.want {
					t.Fatalf("Ensure = %+v, %v; want %+v", st, err, tt.want)
				}
			}
			if w.launcher.startCount() != 0 || w.secrets.generatedCount() != 0 {
				t.Fatal("an answering Hub was started again")
			}
		})
	}
}

func TestEnsureStartsAndWaits(t *testing.T) {
	w := newWorld(t, "http://localhost:9090/")
	settings, err := values.NewServerSettings("https://gw.example.test/anthropic", values.AuthBearer, 500)
	if err != nil {
		t.Fatal(err)
	}
	w.settings.v = settings
	notYet := readyFacts()
	w.probe.after = []values.Facts{notYet, notYet, answering()}

	st, err := w.svc.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Status{State: values.StatusRunning, Port: 9090, URL: "http://127.0.0.1:9090"}
	if st != want {
		t.Fatalf("status = %+v, want %+v", st, want)
	}
	if w.launcher.startCount() != 1 || w.launcher.settings[0] != settings || w.launcher.targets[0].ListenAddr() != "127.0.0.1:9090" {
		t.Fatalf("starts = %d, %+v, %+v", w.launcher.starts, w.launcher.settings, w.launcher.targets)
	}
	if w.secrets.generated[0] != values.SecretSealKeys || w.secrets.generatedCount() != 1 {
		t.Fatalf("generated = %v", w.secrets.generated)
	}
	if w.clock.sleeps != 3 {
		t.Fatalf("sleeps = %d, want 3 polls of 500 ms", w.clock.sleeps)
	}

	// Running now: a second Ensure does not start it again.
	if st, err := w.svc.Ensure(context.Background()); err != nil || st != want {
		t.Fatalf("second Ensure = %+v, %v", st, err)
	}
	if w.launcher.startCount() != 1 || w.secrets.generatedCount() != 1 {
		t.Fatal("started or generated twice")
	}
}

func TestEnsureKeepsExistingSealKeys(t *testing.T) {
	w := newWorld(t, localURL)
	w.secrets.values[values.SecretSealKeys] = "1:fake"
	w.probe.after = []values.Facts{answering()}
	if _, err := w.svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.secrets.generatedCount() != 0 || w.launcher.startCount() != 1 {
		t.Fatalf("generated %d, started %d", w.secrets.generatedCount(), w.launcher.startCount())
	}
}

func TestEnsureAfterStart(t *testing.T) {
	exited := func(code int) values.Facts {
		f := readyFacts()
		f.Exited, f.ExitCode = true, code
		return f
	}
	degraded := readyFacts()
	degraded.Healthz, degraded.Listener = true, values.ListenerOurs
	tests := []struct {
		name   string
		after  []values.Facts
		want   Status
		sleeps int
	}{
		{name: "exit 2 is a config error", after: []values.Facts{readyFacts(), exited(2)},
			want: Status{State: values.StatusNotRunning, Reason: values.ReasonConfigError, Next: values.NextCheckSettings, ExitCode: 2}, sleeps: 2},
		{name: "exit 1", after: []values.Facts{exited(1)},
			want: Status{State: values.StatusNotRunning, Reason: values.ReasonExited, Next: values.NextOpenLogs, ExitCode: 1}, sleeps: 1},
		{name: "degraded", after: []values.Facts{degraded},
			want: Status{State: values.StatusDegraded, Reason: values.ReasonDatabaseDown, Next: values.NextStartDatabase}, sleeps: 1},
		{name: "silent for 20 s", after: []values.Facts{readyFacts()},
			want: Status{State: values.StatusStarting}, sleeps: 40},
		{name: "listening, silent for 20 s", after: []values.Facts{func() values.Facts { f := readyFacts(); f.Listener = values.ListenerOurs; return f }()},
			want: Status{State: values.StatusStarting}, sleeps: 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, localURL)
			w.probe.after = tt.after
			st, err := w.svc.Ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Port, tt.want.URL = 8080, localURL
			if st != tt.want {
				t.Fatalf("status = %+v, want %+v", st, tt.want)
			}
			if w.launcher.startCount() != 1 || w.clock.sleeps != tt.sleeps {
				t.Fatalf("starts %d, sleeps %d", w.launcher.startCount(), w.clock.sleeps)
			}
		})
	}
}

func TestRecheckStartsOnceDockerIsOn(t *testing.T) {
	w := newWorld(t, localURL)
	off := readyFacts()
	off.DockerEngine, off.DatabasePort = false, false
	w.probe.set(off)

	st, err := w.svc.Ensure(context.Background())
	if err != nil || st.Reason != values.ReasonDockerOff || w.launcher.startCount() != 0 {
		t.Fatalf("Ensure = %+v, %v, starts %d", st, err, w.launcher.startCount())
	}

	w.probe.set(readyFacts())
	w.probe.after = []values.Facts{answering()}
	st, err = w.svc.Recheck(context.Background())
	if err != nil || st.State != values.StatusRunning || w.launcher.startCount() != 1 {
		t.Fatalf("Recheck = %+v, %v, starts %d", st, err, w.launcher.startCount())
	}
}

func TestEnsureWithARemoteHub(t *testing.T) {
	tests := []struct{ url, shown string }{
		{url: "https://hub.example.test", shown: "https://hub.example.test"},
		{url: "http://192.0.2.1:8080", shown: "http://192.0.2.1:8080"},
		{url: "http://127.0.0.1:8080/hub", shown: "http://127.0.0.1:8080/hub"},
		{url: "", shown: ""},
		{url: "https://tester:fake-pass@hub.example.test/x", shown: "https://hub.example.test/x"},
		{url: "https://tester@hub.example.test", shown: "https://hub.example.test"},
		{url: "tester:fake-pass@hub.example.test", shown: ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			w := newWorld(t, tt.url)
			st, err := w.svc.Ensure(context.Background())
			if err != nil || st != (Status{State: values.StatusRemote, URL: tt.shown}) {
				t.Fatalf("Ensure = %+v, %v", st, err)
			}
			if st, err := w.svc.Status(context.Background()); err != nil || st.State != values.StatusRemote {
				t.Fatalf("Status = %+v, %v", st, err)
			}
			if w.probe.callCount() != 0 || w.launcher.startCount() != 0 || w.secrets.generatedCount() != 0 {
				t.Fatal("a remote Hub was probed, started or given seal keys")
			}
		})
	}
}

func TestConcurrentEnsureStartsOnce(t *testing.T) {
	w := newWorld(t, localURL)
	w.launcher.entered = make(chan struct{}, 1)
	w.launcher.release = make(chan struct{})
	w.probe.after = []values.Facts{answering()}

	const callers = 8
	results := make(chan Status, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	call := func() {
		defer wg.Done()
		st, err := w.svc.Ensure(context.Background())
		results <- st
		errs <- err
	}
	wg.Add(1)
	go call()
	<-w.launcher.entered

	// The starting status is visible while the start runs.
	if st, err := w.svc.Status(context.Background()); err != nil || st.State != values.StatusStarting {
		t.Fatalf("Status while starting = %+v, %v", st, err)
	}

	wg.Add(callers - 1)
	for range callers - 1 {
		go call()
	}
	waitFor(t, func() bool {
		w.svc.mu.Lock()
		defer w.svc.mu.Unlock()
		return w.svc.flight != nil && w.svc.flight.joined == callers
	})
	close(w.launcher.release)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for st := range results {
		if st.State != values.StatusRunning {
			t.Fatalf("a caller got %+v", st)
		}
	}
	if n := w.launcher.startCount(); n != 1 {
		t.Fatalf("started %d times", n)
	}
	if w.secrets.generatedCount() != 1 {
		t.Fatalf("seal keys generated %d times", w.secrets.generatedCount())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 5000 {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestEnsureFailures(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		prepare func(*world)
		url     *fakeURL
		want    Status // the kept status
		starts  int
	}{
		{name: "probe fails", prepare: func(w *world) { w.probe.err = boom }},
		{name: "secret store fails", prepare: func(w *world) { w.secrets.isSetErr = boom }},
		{name: "server address unreadable", url: &fakeURL{err: boom}},
		{name: "seal keys cannot be generated", prepare: func(w *world) { w.secrets.genErr = boom },
			want: Status{State: values.StatusNotRunning, Reason: values.ReasonExited, Next: values.NextOpenLogs, ExitCode: values.ExitNotStarted, Port: 8080, URL: localURL}},
		{name: "settings unreadable", prepare: func(w *world) { w.settings.loadErr = boom },
			want: Status{State: values.StatusNotRunning, Reason: values.ReasonConfigError, Next: values.NextCheckSettings, Port: 8080, URL: localURL}},
		{name: "start fails", prepare: func(w *world) { w.launcher.err = boom }, starts: 1,
			want: Status{State: values.StatusNotRunning, Reason: values.ReasonExited, Next: values.NextOpenLogs, ExitCode: values.ExitNotStarted, Port: 8080, URL: localURL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, localURL)
			if tt.url != nil {
				w.svc.url = *tt.url
			}
			if tt.prepare != nil {
				tt.prepare(w)
			}
			st, err := w.svc.Ensure(context.Background())
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v", err)
			}
			if st != tt.want || w.launcher.startCount() != tt.starts {
				t.Fatalf("status = %+v, starts %d; want %+v, %d", st, w.launcher.startCount(), tt.want, tt.starts)
			}
		})
	}
}

func TestEnsureWithAnEndedContextStartsNothing(t *testing.T) {
	w := newWorld(t, localURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, call := range []func(context.Context) (Status, error){w.svc.Ensure, w.svc.Recheck} {
		if _, err := call(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	}
	if w.launcher.startCount() != 0 || w.probe.callCount() != 0 {
		t.Fatalf("starts %d, probes %d", w.launcher.startCount(), w.probe.callCount())
	}
}

// TestCancelledLeaderDoesNotAbandonTheStart: the caller that began the check leaves, the check goes on for the rest.
func TestCancelledLeaderDoesNotAbandonTheStart(t *testing.T) {
	w := newWorld(t, localURL)
	w.launcher.entered = make(chan struct{}, 1)
	w.launcher.release = make(chan struct{})
	w.probe.after = []values.Facts{answering()}

	leaderCtx, cancel := context.WithCancel(context.Background())
	type result struct {
		st  Status
		err error
	}
	leader, follower := make(chan result, 1), make(chan result, 1)
	go func() {
		st, err := w.svc.Ensure(leaderCtx)
		leader <- result{st, err}
	}()
	<-w.launcher.entered
	go func() {
		st, err := w.svc.Recheck(context.Background())
		follower <- result{st, err}
	}()
	waitFor(t, func() bool {
		w.svc.mu.Lock()
		defer w.svc.mu.Unlock()
		return w.svc.flight != nil && w.svc.flight.joined == 2
	})

	cancel()
	got := <-leader
	if !errors.Is(got.err, context.Canceled) || got.st.State != values.StatusStarting {
		t.Fatalf("leader = %+v, %v; want the kept starting status and ctx's error", got.st, got.err)
	}

	close(w.launcher.release)
	got = <-follower
	if got.err != nil || got.st.State != values.StatusRunning {
		t.Fatalf("follower = %+v, %v", got.st, got.err)
	}
	if n := w.launcher.startCount(); n != 1 {
		t.Fatalf("started %d times", n)
	}
	if st, _ := w.svc.Status(context.Background()); st.State != values.StatusRunning {
		t.Fatalf("kept status = %+v", st)
	}
}

func exitedFacts(code int) values.Facts {
	f := readyFacts()
	f.Exited, f.ExitCode = true, code
	return f
}

// TestRecordedExitNeedsTheOwner: the automatic Ensure never starts over an exit; Recheck, SaveSettings and
// SetSecret allow one new start, after which the new process's own exit counts again.
func TestRecordedExitNeedsTheOwner(t *testing.T) {
	ack := map[string]func(*world) error{
		"recheck": func(w *world) error { _, err := w.svc.Recheck(context.Background()); return err },
		"save settings": func(w *world) error {
			return w.svc.SaveSettings(context.Background(), Settings{BaseURL: "https://gw.example.test"})
		},
		"set secret": func(w *world) error {
			return w.svc.SetSecret(context.Background(), "provider-key", "sk-fake-new")
		},
	}
	for name, acknowledge := range ack {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, localURL)
			w.probe.after = []values.Facts{exitedFacts(values.ExitConfigError)}
			st, err := w.svc.Ensure(context.Background())
			if err != nil || st.Reason != values.ReasonConfigError || w.launcher.startCount() != 1 {
				t.Fatalf("first Ensure = %+v, %v, starts %d", st, err, w.launcher.startCount())
			}

			// The automatic path keeps the exit: no respawn loop.
			for range 3 {
				if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonConfigError {
					t.Fatalf("automatic Ensure = %+v, %v", st, err)
				}
			}
			if n := w.launcher.startCount(); n != 1 {
				t.Fatalf("respawned automatically: %d starts", n)
			}

			// The owner acts; the new process fails as well and its exit is seen again.
			if err := acknowledge(w); err != nil {
				t.Fatal(err)
			}
			st, err = w.svc.Ensure(context.Background())
			if err != nil || st.Reason != values.ReasonConfigError || st.ExitCode != values.ExitConfigError {
				t.Fatalf("Ensure after the owner = %+v, %v", st, err)
			}
			if n := w.launcher.startCount(); n != 2 {
				t.Fatalf("starts = %d, want one new start", n)
			}
			if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonConfigError || w.launcher.startCount() != 2 {
				t.Fatalf("the new exit was not kept: %+v, %v, starts %d", st, err, w.launcher.startCount())
			}

			// Fixed now: the next acknowledgement starts a Hub that answers.
			w.probe.then(exitedFacts(values.ExitConfigError), answering())
			if err := acknowledge(w); err != nil {
				t.Fatal(err)
			}
			if st, err := w.svc.Ensure(context.Background()); err != nil || st.State != values.StatusRunning || w.launcher.startCount() != 3 {
				t.Fatalf("Ensure after the fix = %+v, %v, starts %d", st, err, w.launcher.startCount())
			}
		})
	}
}

// TestAckWithoutARecordedExitIsSpent: an acknowledgement given while the Hub runs covers no exit, so a crash after it
// is not respawned automatically.
func TestAckWithoutARecordedExitIsSpent(t *testing.T) {
	ack := map[string]func(*world) error{
		"recheck": func(w *world) error { _, err := w.svc.Recheck(context.Background()); return err },
		"save settings": func(w *world) error {
			return w.svc.SaveSettings(context.Background(), Settings{BaseURL: "https://gw.example.test"})
		},
		"set secret": func(w *world) error {
			return w.svc.SetSecret(context.Background(), "provider-key", "sk-fake-new")
		},
	}
	for name, acknowledge := range ack {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, localURL)
			w.probe.set(answering())
			if err := acknowledge(w); err != nil {
				t.Fatal(err)
			}
			// The periodic check sees the Hub running.
			if st, err := w.svc.Ensure(context.Background()); err != nil || st.State != values.StatusRunning {
				t.Fatalf("Ensure while running = %+v, %v", st, err)
			}

			w.probe.set(exitedFacts(1))
			for range 2 {
				if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonExited {
					t.Fatalf("Ensure after the crash = %+v, %v", st, err)
				}
			}
			if n := w.launcher.startCount(); n != 0 {
				t.Fatalf("respawned after an unacknowledged crash: %d starts", n)
			}

			// A new acknowledgement now covers the recorded exit.
			w.probe.after = []values.Facts{answering()}
			if st, err := w.svc.Recheck(context.Background()); err != nil || st.State != values.StatusRunning || w.launcher.startCount() != 1 {
				t.Fatalf("Recheck = %+v, %v, starts %d", st, err, w.launcher.startCount())
			}
		})
	}
}

// TestHungPortEndsTheFlight: a port call that never returns is cut by the flight timeout, and the next Ensure runs a
// new check.
func TestHungPortEndsTheFlight(t *testing.T) {
	w := newWorld(t, localURL)
	w.svc.timeout = 20 * time.Millisecond
	w.probe.hang = true

	if _, err := w.svc.Ensure(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the flight deadline", err)
	}
	w.svc.mu.Lock()
	left := w.svc.flight
	w.svc.mu.Unlock()
	if left != nil {
		t.Fatal("the hung flight is still registered")
	}

	w.probe.mu.Lock()
	w.probe.hang = false
	w.probe.mu.Unlock()
	w.svc.timeout = flightTimeout
	w.probe.after = []values.Facts{answering()}
	if st, err := w.svc.Ensure(context.Background()); err != nil || st.State != values.StatusRunning || w.launcher.startCount() != 1 {
		t.Fatalf("next Ensure = %+v, %v, starts %d", st, err, w.launcher.startCount())
	}
}

func TestRecheckAfterTheDatabaseIsBack(t *testing.T) {
	w := newWorld(t, localURL)
	w.probe.after = []values.Facts{exitedFacts(1)}
	if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonExited {
		t.Fatalf("Ensure = %+v, %v", st, err)
	}

	// The database went down: Recheck names it and starts nothing.
	down := exitedFacts(1)
	down.DatabasePort = false
	w.probe.then(down)
	if st, err := w.svc.Recheck(context.Background()); err != nil || st.Reason != values.ReasonDatabaseDown {
		t.Fatalf("Recheck = %+v, %v", st, err)
	}

	w.probe.then(exitedFacts(1), answering())
	if st, err := w.svc.Recheck(context.Background()); err != nil || st.State != values.StatusRunning {
		t.Fatalf("Recheck after the database = %+v, %v", st, err)
	}
	if n := w.launcher.startCount(); n != 2 {
		t.Fatalf("starts = %d", n)
	}
}

func TestAutomaticEnsureAfterAnExitBeforeAnyStart(t *testing.T) {
	w := newWorld(t, localURL)
	w.probe.set(exitedFacts(1))
	for range 3 {
		if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonExited {
			t.Fatalf("Ensure = %+v, %v", st, err)
		}
	}
	if w.launcher.startCount() != 0 {
		t.Fatal("started over a recorded exit")
	}
	w.probe.after = []values.Facts{answering()}
	if st, err := w.svc.Recheck(context.Background()); err != nil || st.State != values.StatusRunning || w.launcher.startCount() != 1 {
		t.Fatalf("Recheck = %+v, %v, starts %d", st, err, w.launcher.startCount())
	}
}

func TestStatusBeforeEnsureDoesNotStart(t *testing.T) {
	w := newWorld(t, localURL)
	st, err := w.svc.Status(context.Background())
	if err != nil || st.State != values.StatusNotRunning || st.Reason != values.ReasonNone {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if w.launcher.startCount() != 0 || w.probe.callCount() != 1 {
		t.Fatalf("starts %d, probes %d", w.launcher.startCount(), w.probe.callCount())
	}
}

func TestSettings(t *testing.T) {
	w := newWorld(t, localURL)
	in := Settings{BaseURL: "https://gw.example.test", Auth: values.AuthXAPIKey, QuotaLimit: 1000}
	if err := w.svc.SaveSettings(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got, err := w.svc.Settings(context.Background()); err != nil || got != in {
		t.Fatalf("Settings = %+v, %v", got, err)
	}

	for _, bad := range []Settings{{BaseURL: "http://gw.example.test"}, {Auth: "basic"}, {QuotaLimit: -5}} {
		if err := w.svc.SaveSettings(context.Background(), bad); !errors.Is(err, values.ErrInvalidSettings) {
			t.Fatalf("SaveSettings(%+v) = %v", bad, err)
		}
	}
	if w.settings.saves != 1 {
		t.Fatalf("saves = %d", w.settings.saves)
	}
}

func TestSecrets(t *testing.T) {
	const value = "sk-fake-provider-value-123"
	w := newWorld(t, localURL)
	w.secrets.values = map[values.SecretName]string{}

	if ok, err := w.svc.SecretSet(context.Background(), "provider-key"); err != nil || ok {
		t.Fatalf("SecretSet before = %v, %v", ok, err)
	}
	if err := w.svc.SetSecret(context.Background(), "provider-key", value); err != nil {
		t.Fatal(err)
	}
	if w.secrets.values[values.SecretProviderKey] != value {
		t.Fatal("value not handed to the store")
	}
	if ok, err := w.svc.SecretSet(context.Background(), "provider-key"); err != nil || !ok {
		t.Fatalf("SecretSet after = %v, %v", ok, err)
	}

	for _, name := range []string{"seal-keys", "api-key", ""} {
		if err := w.svc.SetSecret(context.Background(), name, value); !errors.Is(err, values.ErrUnknownSecret) {
			t.Fatalf("SetSecret(%q) = %v", name, err)
		}
		if _, err := w.svc.SecretSet(context.Background(), name); !errors.Is(err, values.ErrUnknownSecret) {
			t.Fatalf("SecretSet(%q) = %v", name, err)
		}
	}
	if _, ok := w.secrets.values[values.SecretSealKeys]; ok {
		t.Fatal("seal keys were set by name")
	}
	if err := w.svc.SetSecret(context.Background(), "database-url", " \tpostgres://fake@127.0.0.1:5432/hub\r\n"); err != nil {
		t.Fatal(err)
	}
	if got := w.secrets.values[values.SecretDatabaseURL]; got != "postgres://fake@127.0.0.1:5432/hub" {
		t.Fatalf("stored %q, want it trimmed", got)
	}

	for _, blank := range []string{"", "  \t", "\r\n"} {
		if err := w.svc.SetSecret(context.Background(), "database-url", blank); !errors.Is(err, ErrEmptySecret) {
			t.Fatalf("SetSecret(blank) = %v", err)
		}
	}
}

// TestSecretValueNeverLeaks drives every path a value could take and checks errors, statuses and the log.
func TestSecretValueNeverLeaks(t *testing.T) {
	const value = "postgres://hub:fake-db-password@127.0.0.1:55432/hub"
	w := newWorld(t, localURL)
	var seen []string

	w.secrets.setErr = func(v string) error { return fmt.Errorf("store refused %q", v) }
	err := w.svc.SetSecret(context.Background(), "database-url", value)
	if !errors.Is(err, ErrSecretStore) {
		t.Fatalf("err = %v, want ErrSecretStore", err)
	}
	seen = append(seen, err.Error())

	w.secrets.setErr = nil
	if err := w.svc.SetSecret(context.Background(), "database-url", value); err != nil {
		t.Fatal(err)
	}
	err = w.svc.SetSecret(context.Background(), value, value)
	seen = append(seen, err.Error())

	w.probe.after = []values.Facts{answering()}
	st, err := w.svc.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen = append(seen, fmt.Sprintf("%+v", st), w.logs.String())

	for _, s := range seen {
		if strings.Contains(s, "fake-db-password") {
			t.Fatalf("the secret value leaked: %s", s)
		}
	}
}
