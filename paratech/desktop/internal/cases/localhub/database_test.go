package localhub

import (
	"context"
	"errors"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/localhub/values"
)

// dbDown: the engine runs, the database port is silent and the container is startable.
func dbDown() values.Facts {
	f := readyFacts()
	f.DatabasePort, f.DatabaseStartable = false, true
	return f
}

func dbUp() values.Facts {
	f := readyFacts()
	f.DatabaseStartable = true
	return f
}

// withDatabase makes a successful StartDatabase bring the port up.
func withDatabase(w *world) {
	w.probe.set(dbDown())
	w.probe.after = []values.Facts{answering()}
	w.database.up = func() { w.probe.set(dbUp()) }
}

func TestEnsureStartsTheDatabaseThenTheHub(t *testing.T) {
	w := newWorld(t, localURL)
	withDatabase(w)
	w.database.entered = make(chan struct{}, 1)
	w.database.release = make(chan struct{})

	done := make(chan Status, 1)
	go func() {
		st, err := w.svc.Ensure(context.Background())
		if err != nil {
			t.Error(err)
		}
		done <- st
	}()
	<-w.database.entered
	if st, err := w.svc.Status(context.Background()); err != nil || st.State != values.StatusStartingDatabase || st.Reason != values.ReasonNone {
		t.Fatalf("Status while the database starts = %+v, %v", st, err)
	}
	close(w.database.release)

	if st := <-done; st.State != values.StatusRunning {
		t.Fatalf("Ensure = %+v", st)
	}
	if w.database.callCount() != 1 || !w.database.hasDL {
		t.Fatalf("database starts %d, deadline %v", w.database.callCount(), w.database.hasDL)
	}
	if w.launcher.startCount() != 1 || w.clock.sleeps != 1 {
		t.Fatalf("hub starts %d, sleeps %d; want 1 start and one 500 ms poll (the port answered at once)", w.launcher.startCount(), w.clock.sleeps)
	}
}

func TestEnsureWaitsForTheDatabasePort(t *testing.T) {
	w := newWorld(t, localURL)
	w.probe.set(dbDown())
	w.probe.after = []values.Facts{answering()}
	// After the command: the first probe and two more stay silent, then the port answers.
	w.database.up = func() { w.probe.queue(dbDown(), dbDown(), dbDown(), dbUp()) }

	st, err := w.svc.Ensure(context.Background())
	if err != nil || st.State != values.StatusRunning {
		t.Fatalf("Ensure = %+v, %v", st, err)
	}
	if w.clock.sleeps != 3+1 {
		t.Fatalf("sleeps = %d, want 3 database polls of 1 s and one hub poll", w.clock.sleeps)
	}
}

func TestEnsureDatabasePortSilentAfterTheStart(t *testing.T) {
	w := newWorld(t, localURL)
	w.probe.set(dbDown())

	st, err := w.svc.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Status{State: values.StatusNotRunning, Reason: values.ReasonDatabaseStartFailed, Next: values.NextOpenLogs, Port: 8080, URL: localURL}
	if st != want {
		t.Fatalf("status = %+v, want %+v", st, want)
	}
	if w.clock.sleeps != 30 || w.launcher.startCount() != 0 {
		t.Fatalf("sleeps %d (want 30 polls of 1 s), hub starts %d", w.clock.sleeps, w.launcher.startCount())
	}
	if _, err := w.svc.Ensure(context.Background()); err != nil || w.database.callCount() != 1 {
		t.Fatalf("the silent database was started again: %v, %d", err, w.database.callCount())
	}
}

func TestEnsureDoesNotStartTheDatabase(t *testing.T) {
	off := dbDown()
	off.DockerEngine = false
	degradedOff := values.Facts{Healthz: true, Listener: values.ListenerOurs, DatabaseStartable: true}
	notStartable := dbDown()
	notStartable.DatabaseStartable = false
	degradedNotStartable := notStartable
	degradedNotStartable.Healthz, degradedNotStartable.Listener = true, values.ListenerOurs
	tests := []struct {
		name  string
		facts values.Facts
		state values.Status
		next  values.NextStep
		rsn   values.Reason
	}{
		{name: "docker off", facts: off, state: values.StatusNotRunning, rsn: values.ReasonDockerOff, next: values.NextStartDocker},
		{name: "degraded, docker off", facts: degradedOff, state: values.StatusDegraded, rsn: values.ReasonDockerOff, next: values.NextStartDocker},
		{name: "not startable", facts: notStartable, state: values.StatusNotRunning, rsn: values.ReasonDatabaseDown, next: values.NextStartDatabase},
		{name: "degraded, not startable", facts: degradedNotStartable, state: values.StatusDegraded, rsn: values.ReasonDatabaseDown, next: values.NextStartDatabase},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, localURL)
			w.probe.set(tt.facts)
			st, err := w.svc.Ensure(context.Background())
			want := Status{State: tt.state, Reason: tt.rsn, Next: tt.next, Port: 8080, URL: localURL}
			if err != nil || st != want {
				t.Fatalf("Ensure = %+v, %v; want %+v", st, err, want)
			}
			if w.database.callCount() != 0 || w.launcher.startCount() != 0 {
				t.Fatalf("database starts %d, hub starts %d", w.database.callCount(), w.launcher.startCount())
			}
		})
	}
}

func TestEnsureStartsTheDatabaseOfADegradedHubWithoutRestartingIt(t *testing.T) {
	w := newWorld(t, localURL)
	degraded := dbDown()
	degraded.Healthz, degraded.Listener = true, values.ListenerOurs
	w.probe.set(degraded)
	w.database.up = func() {
		f := answering()
		f.DatabaseStartable = true
		w.probe.set(f)
	}

	st, err := w.svc.Ensure(context.Background())
	if err != nil || st.State != values.StatusRunning {
		t.Fatalf("Ensure = %+v, %v", st, err)
	}
	if w.database.callCount() != 1 || w.launcher.startCount() != 0 || w.secrets.generatedCount() != 0 {
		t.Fatalf("database starts %d, hub starts %d, seal keys %d", w.database.callCount(), w.launcher.startCount(), w.secrets.generatedCount())
	}
}

func TestDatabaseStartFailureIsRemembered(t *testing.T) {
	boom := errors.New("compose failed")
	forget := map[string]func(*world) error{
		"recheck": func(w *world) error { _, err := w.svc.Recheck(context.Background()); return err },
		"new database url": func(w *world) error {
			if err := w.svc.SetSecret(context.Background(), "database-url", "postgres://fake@127.0.0.1:5432/hub"); err != nil {
				return err
			}
			_, err := w.svc.Ensure(context.Background())
			return err
		},
		"engine off and on": func(w *world) error {
			off := dbDown()
			off.DockerEngine = false
			w.probe.set(off)
			if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonDockerOff {
				return errors.Join(err, errors.New("engine off: "+string(st.Reason)))
			}
			if w.database.callCount() != 1 {
				return errors.New("started with the engine off")
			}
			w.probe.set(dbDown())
			_, err := w.svc.Ensure(context.Background())
			return err
		},
	}
	for name, act := range forget {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, localURL)
			withDatabase(w)
			w.database.err = boom

			st, err := w.svc.Ensure(context.Background())
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v", err)
			}
			want := Status{State: values.StatusNotRunning, Reason: values.ReasonDatabaseStartFailed, Next: values.NextOpenLogs, Port: 8080, URL: localURL}
			if st != want {
				t.Fatalf("status = %+v, want %+v", st, want)
			}

			// The automatic check does not run the command again.
			for range 3 {
				if st, err := w.svc.Ensure(context.Background()); err != nil || st != want {
					t.Fatalf("automatic Ensure = %+v, %v", st, err)
				}
			}
			if st, err := w.svc.Status(context.Background()); err != nil || st != want {
				t.Fatalf("Status = %+v, %v", st, err)
			}
			if w.database.callCount() != 1 || w.launcher.startCount() != 0 {
				t.Fatalf("database starts %d, hub starts %d", w.database.callCount(), w.launcher.startCount())
			}

			// The owner acts; the next start succeeds and the Hub follows.
			w.database.mu.Lock()
			w.database.err = nil
			w.database.mu.Unlock()
			if err := act(w); err != nil {
				t.Fatal(err)
			}
			if w.database.callCount() != 2 || w.launcher.startCount() != 1 {
				t.Fatalf("database starts %d, hub starts %d; want 2 and 1", w.database.callCount(), w.launcher.startCount())
			}
			if st, _ := w.svc.Status(context.Background()); st.State != values.StatusRunning {
				t.Fatalf("status = %+v", st)
			}
		})
	}
}

func TestOtherSecretKeepsTheDatabaseFailure(t *testing.T) {
	w := newWorld(t, localURL)
	withDatabase(w)
	w.database.err = errors.New("compose failed")
	if _, err := w.svc.Ensure(context.Background()); err == nil {
		t.Fatal("no error")
	}
	if err := w.svc.SetSecret(context.Background(), "provider-key", "sk-fake-new"); err != nil {
		t.Fatal(err)
	}
	if st, err := w.svc.Ensure(context.Background()); err != nil || st.Reason != values.ReasonDatabaseStartFailed || w.database.callCount() != 1 {
		t.Fatalf("Ensure = %+v, %v, database starts %d", st, err, w.database.callCount())
	}
}

func TestDegradedHubDatabaseStartFailure(t *testing.T) {
	w := newWorld(t, localURL)
	degraded := dbDown()
	degraded.Healthz, degraded.Listener = true, values.ListenerOurs
	w.probe.set(degraded)
	w.database.err = errors.New("compose failed")

	st, err := w.svc.Ensure(context.Background())
	if err == nil || st.State != values.StatusDegraded || st.Reason != values.ReasonDatabaseStartFailed || st.Next != values.NextOpenLogs {
		t.Fatalf("Ensure = %+v, %v", st, err)
	}
	if w.launcher.startCount() != 0 {
		t.Fatal("the degraded Hub was started")
	}
}

func TestEngineGoneWhileWaitingForTheDatabase(t *testing.T) {
	w := newWorld(t, localURL)
	w.probe.set(dbDown())
	w.database.up = func() {
		off := dbDown()
		off.DockerEngine = false
		w.probe.set(off)
	}

	st, err := w.svc.Ensure(context.Background())
	if err != nil || st.Reason != values.ReasonDockerOff || w.launcher.startCount() != 0 {
		t.Fatalf("Ensure = %+v, %v, hub starts %d", st, err, w.launcher.startCount())
	}

	// Not a failure: once the engine is back, the container is started again.
	w.probe.set(dbDown())
	w.database.up = func() { w.probe.set(dbUp()) }
	w.probe.after = []values.Facts{answering()}
	if st, err := w.svc.Ensure(context.Background()); err != nil || st.State != values.StatusRunning || w.database.callCount() != 2 {
		t.Fatalf("Ensure = %+v, %v, database starts %d", st, err, w.database.callCount())
	}
}

func TestConcurrentEnsureStartsTheDatabaseOnce(t *testing.T) {
	w := newWorld(t, localURL)
	withDatabase(w)
	w.database.entered = make(chan struct{}, 1)
	w.database.release = make(chan struct{})

	const callers = 5
	var wg sync.WaitGroup
	results := make(chan Status, callers)
	call := func() {
		defer wg.Done()
		st, err := w.svc.Ensure(context.Background())
		if err != nil {
			t.Error(err)
		}
		results <- st
	}
	wg.Add(1)
	go call()
	<-w.database.entered
	wg.Add(callers - 1)
	for range callers - 1 {
		go call()
	}
	waitFor(t, func() bool {
		w.svc.mu.Lock()
		defer w.svc.mu.Unlock()
		return w.svc.flight != nil && w.svc.flight.joined == callers
	})
	close(w.database.release)
	wg.Wait()
	close(results)

	for st := range results {
		if st.State != values.StatusRunning {
			t.Fatalf("a caller got %+v", st)
		}
	}
	if w.database.callCount() != 1 || w.launcher.startCount() != 1 {
		t.Fatalf("database starts %d, hub starts %d", w.database.callCount(), w.launcher.startCount())
	}
}

func TestDatabaseStartCutByTheFlight(t *testing.T) {
	w := newWorld(t, localURL)
	withDatabase(w)
	w.svc.timeout = 0 // the flight has ended when the command runs

	if _, err := w.svc.Ensure(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the flight deadline", err)
	}
	// Not remembered as a failure.
	w.svc.timeout = flightTimeout
	if st, err := w.svc.Ensure(context.Background()); err != nil || st.State != values.StatusRunning || w.database.callCount() != 2 {
		t.Fatalf("Ensure = %+v, %v, database starts %d", st, err, w.database.callCount())
	}
}
