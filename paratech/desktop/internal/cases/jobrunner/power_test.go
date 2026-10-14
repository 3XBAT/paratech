package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeSettings is a SettingsReader with a switchable "only on AC power" setting and an injectable failure.
type fakeSettings struct {
	mu       sync.Mutex
	onlyOnAC bool
	err      error
}

func newFakeSettings(onlyOnAC bool) *fakeSettings { return &fakeSettings{onlyOnAC: onlyOnAC} }

func (f *fakeSettings) set(onlyOnAC bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onlyOnAC = onlyOnAC
}

func (f *fakeSettings) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeSettings) Load(context.Context) (*settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return settings.Restore(settings.State{Version: 1, ProcessOnlyOnACPower: f.onlyOnAC})
}

// blockingSettings blocks its first Load until released or ctx ends, so a test can act while Start reads the settings.
type blockingSettings struct {
	*fakeSettings
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func newBlockingSettings(onlyOnAC bool) *blockingSettings {
	return &blockingSettings{fakeSettings: newFakeSettings(onlyOnAC), entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingSettings) Load(ctx context.Context) (*settings.Settings, error) {
	if b.first.CompareAndSwap(false, true) {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.fakeSettings.Load(ctx)
}

// brokenPower cannot tell the power source at all.
type brokenPower struct{}

func (brokenPower) IsOnACPower(context.Context) (bool, error) {
	return false, errors.New("no power status")
}

func (brokenPower) Subscribe(context.Context) (<-chan bool, error) {
	return nil, errors.New("no power events")
}

// pollOnlyPower is a FakePower without events, so the runner has to poll it.
type pollOnlyPower struct{ *testenv.FakePower }

func (pollOnlyPower) Subscribe(context.Context) (<-chan bool, error) {
	return nil, errors.New("no power events")
}

// newPowerEnv is newEnv with a power monitor and settings; nil leaves the dependency out.
func newPowerEnv(t *testing.T, power PowerMonitor, st SettingsReader) *env {
	t.Helper()
	testenv.NoGoroutineLeaks(t)
	e := &env{t: t, repo: newMemRepo(), clock: testenv.NewFakeClock(t0)}
	deps := Deps{Jobs: e.repo, Clock: e.clock, Sleeper: e.clock}
	if power != nil {
		deps.Power = power
	}
	if st != nil {
		deps.Settings = st
	}
	svc, err := New(deps, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return e
}

func (e *env) registerHeavy(typ job.Type, h handlerFunc) {
	e.t.Helper()
	if err := e.svc.Register(Registration{Type: typ, Handler: h, Recovery: RecoveryRequeue, Heavy: true}); err != nil {
		e.t.Fatal(err)
	}
}

// awaitSource waits until the runner has seen the power source.
func (e *env) awaitSource(src powerpolicy.Source) {
	e.t.Helper()
	eventually(e.t, fmt.Sprintf("power source %v", src), func() bool { return e.svc.conditions().Source == src })
}

func mustProgress(t *testing.T, percent int, checkpoint string) job.Progress {
	t.Helper()
	p, err := job.NewProgress(percent, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// countingHandler counts calls and succeeds.
func countingHandler(calls *atomic.Int32) handlerFunc {
	return func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		calls.Add(1)
		return nil
	}
}

func TestHeavyJobWaitsOnBatteryAndRunsWhenACReturns(t *testing.T) {
	power := testenv.NewFakePower(false)
	e := newPowerEnv(t, power, newFakeSettings(true))
	var sttCalls, mailCalls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&sttCalls))
	e.register(typeMail, countingHandler(&mailCalls))
	stt := e.enqueue(typeStt, 3)
	mail := e.enqueue(typeMail, 3)
	e.start()

	e.waitStatus(stt, job.StatusWaitingForPower)
	e.drive("the light job completes on battery", func() bool { return e.status(mail) == job.StatusCompleted })
	for range 20 {
		e.clock.Advance(time.Second)
	}
	e.idle(1)
	if sttCalls.Load() != 0 || e.status(stt) != job.StatusWaitingForPower {
		t.Fatalf("heavy job on battery: calls=%d status=%s, want 0 and WAITING_FOR_POWER (AC-21)", sttCalls.Load(), e.status(stt))
	}

	power.Set(true)
	e.drive("the heavy job completes on AC", func() bool { return e.status(stt) == job.StatusCompleted })
	want := []job.Status{job.StatusQueued, job.StatusWaitingForPower, job.StatusQueued, job.StatusRunning, job.StatusCompleted}
	if got := statuses(e.repo.path(stt)); !equalStatuses(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	if sttCalls.Load() != 1 || mailCalls.Load() != 1 {
		t.Fatalf("calls stt=%d mail=%d, want 1 each", sttCalls.Load(), mailCalls.Load())
	}
}

func TestStartDoesNotBlockTheServiceDuringPowerInit(t *testing.T) {
	st := newBlockingSettings(true)
	e := newPowerEnv(t, testenv.NewFakePower(false), st)
	var sttCalls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&sttCalls))
	e.register(typeMail, countingHandler(new(atomic.Int32)))
	stt := e.enqueue(typeStt, 3)
	mail := e.enqueue(typeMail, 3)

	startErr := make(chan error, 1)
	go func() { startErr <- e.svc.Start(context.Background()) }()
	<-st.entered

	noop := countingHandler(new(atomic.Int32))
	if err := e.svc.Register(Registration{Type: "other", Handler: noop, Recovery: RecoveryRequeue}); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("Register during Start = %v, want ErrAlreadyStarted", err)
	}
	if err := e.svc.Cancel(context.Background(), "job-404"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Cancel of an unknown job = %v, want ErrNotFound", err)
	}
	if err := e.svc.Cancel(context.Background(), mail); err != nil {
		t.Fatalf("Cancel of a queued job = %v", err)
	}
	if got := e.status(mail); got != job.StatusCancelled {
		t.Fatalf("cancelled job is %s", got)
	}

	close(st.release)
	if err := <-startErr; err != nil {
		t.Fatalf("Start = %v", err)
	}
	// The workers started with the battery and the setting known: the heavy job was never claimed (AC-21).
	e.waitStatus(stt, job.StatusWaitingForPower)
	if sttCalls.Load() != 0 {
		t.Fatalf("heavy calls = %d, want 0", sttCalls.Load())
	}
}

func TestStopDuringPowerInitIsClean(t *testing.T) {
	tests := []struct {
		name string
		stop func(e *env, cancel context.CancelFunc) error
	}{
		{"Stop", func(e *env, _ context.CancelFunc) error { return e.svc.Stop(context.Background()) }},
		{"start context cancelled", func(_ *env, cancel context.CancelFunc) error {
			cancel()
			return nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newBlockingSettings(true)
			e := newPowerEnv(t, testenv.NewFakePower(true), st)
			var calls atomic.Int32
			e.register(typeMail, countingHandler(&calls))
			mail := e.enqueue(typeMail, 3)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			startErr := make(chan error, 1)
			go func() { startErr <- e.svc.Start(ctx) }()
			<-st.entered

			// The settings read ends with the loop context, so neither Stop nor Start waits for the release.
			if err := tc.stop(e, cancel); err != nil {
				t.Fatalf("stop = %v", err)
			}
			if err := <-startErr; err != nil {
				t.Fatalf("Start = %v", err)
			}
			if err := e.svc.Stop(context.Background()); err != nil {
				t.Fatalf("Stop = %v", err)
			}
			if calls.Load() != 0 || e.status(mail) != job.StatusQueued {
				t.Fatalf("after a stop during init: calls=%d status=%s, want no claim", calls.Load(), e.status(mail))
			}
		})
	}
}

func TestPowerLostMidRunStopsAtTheNextCheckpoint(t *testing.T) {
	power := testenv.NewFakePower(true)
	e := newPowerEnv(t, power, newFakeSettings(true))
	started, proceed := make(chan struct{}), make(chan struct{})
	stopErr := make(chan error, 1)
	var calls atomic.Int32
	var resumed atomic.Value
	e.registerHeavy(typeStt, func(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
		if calls.Add(1) > 1 {
			resumed.Store(fmt.Sprintf("attempt=%d checkpoint=%s", snap.Attempt(), snap.Checkpoint()))
			return nil
		}
		if err := report(ctx, mustProgress(t, 30, "chunk-1")); err != nil {
			return err
		}
		close(started)
		select {
		case <-proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
		err := report(ctx, mustProgress(t, 60, "chunk-2"))
		stopErr <- err
		return fmt.Errorf("stopping at chunk-2: %w", err)
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	<-started

	power.Set(false)
	e.awaitSource(powerpolicy.SourceBattery)
	close(proceed)
	if err := <-stopErr; !errors.Is(err, powerpolicy.ErrPowerLost) {
		t.Fatalf("report on battery = %v, want ErrPowerLost", err)
	}
	e.waitStatus(id, job.StatusWaitingForPower)
	st := e.repo.snapshot(id)
	if st.Checkpoint != "chunk-2" || st.Percent != 60 || st.Attempts != 0 || st.LastError != "" {
		t.Fatalf("waiting job: checkpoint=%q percent=%d attempts=%d lastError=%q; want the checkpoint saved and the attempt given back",
			st.Checkpoint, st.Percent, st.Attempts, st.LastError)
	}

	power.Set(true)
	e.drive("the job resumes and completes", func() bool { return e.status(id) == job.StatusCompleted })
	if got := resumed.Load(); got != "attempt=1 checkpoint=chunk-2" {
		t.Fatalf("resumed attempt saw %v, want attempt 1 from chunk-2", got)
	}
}

func TestPowerBackBeforeTheCheckpointKeepsRunning(t *testing.T) {
	power := testenv.NewFakePower(true)
	e := newPowerEnv(t, power, newFakeSettings(true))
	started, proceed := make(chan struct{}), make(chan struct{})
	e.registerHeavy(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		close(started)
		<-proceed
		return report(ctx, mustProgress(t, 50, "chunk-1"))
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	<-started

	power.Set(false)
	e.awaitSource(powerpolicy.SourceBattery)
	power.Set(true)
	e.awaitSource(powerpolicy.SourceAC)
	close(proceed)
	e.waitStatus(id, job.StatusCompleted)
}

func TestSettingOffRunsHeavyJobsOnBattery(t *testing.T) {
	var calls atomic.Int32
	e := newPowerEnv(t, testenv.NewFakePower(false), newFakeSettings(false))
	e.registerHeavy(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		calls.Add(1)
		return report(ctx, mustProgress(t, 50, "chunk-1")) // never stopped: the restriction is off
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusCompleted)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestSettingChangesApplyAtTheNextPoll(t *testing.T) {
	st := newFakeSettings(true)
	e := newPowerEnv(t, testenv.NewFakePower(false), st)
	started, proceed := make(chan struct{}), make(chan struct{})
	stopErr := make(chan error, 1)
	var calls atomic.Int32
	e.registerHeavy(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		if calls.Add(1) > 1 {
			return nil
		}
		close(started)
		<-proceed
		err := report(ctx, mustProgress(t, 40, "chunk-3"))
		stopErr <- err
		return err
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusWaitingForPower)

	// Turning the restriction off releases the waiting job (AC-22).
	st.set(false)
	e.drive("released and started", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})

	// Turning it on again stops the running job at its next checkpoint.
	st.set(true)
	e.drive("the setting to be re-read", func() bool { return e.svc.conditions().OnlyOnACPower })
	close(proceed)
	if err := <-stopErr; !errors.Is(err, powerpolicy.ErrPowerLost) {
		t.Fatalf("report = %v, want ErrPowerLost", err)
	}
	e.waitStatus(id, job.StatusWaitingForPower)
	if st := e.repo.snapshot(id); st.Checkpoint != "chunk-3" || st.Attempts != 0 {
		t.Fatalf("checkpoint=%q attempts=%d", st.Checkpoint, st.Attempts)
	}
}

func TestForceRunStartsOneJobOnBattery(t *testing.T) {
	e := newPowerEnv(t, testenv.NewFakePower(false), newFakeSettings(true))
	var calls atomic.Int32
	reportErr := make(chan error, 2)
	e.registerHeavy(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		reportErr <- report(ctx, mustProgress(t, 10, "chunk-1"))
		if calls.Add(1) == 1 {
			return errors.New("transient failure")
		}
		return nil
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusWaitingForPower)

	if err := e.svc.ForceRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.drive("the forced attempt fails", func() bool { return calls.Load() == 1 })
	if err := <-reportErr; err != nil {
		t.Fatalf("report of a forced attempt on battery = %v, want nil", err)
	}

	// The override covered one start: the retry waits for power again.
	e.drive("the retry waits for power", func() bool { return e.status(id) == job.StatusWaitingForPower })
	if st := e.repo.snapshot(id); st.RunNow || calls.Load() != 1 {
		t.Fatalf("after the retry: runNow=%v calls=%d", st.RunNow, calls.Load())
	}

	if err := e.svc.ForceRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.drive("the second forced attempt completes", func() bool { return e.status(id) == job.StatusCompleted })
	if err := <-reportErr; err != nil {
		t.Fatalf("second report = %v", err)
	}
}

func TestForceRunOfAQueuedJobBeforeStart(t *testing.T) {
	e := newPowerEnv(t, testenv.NewFakePower(false), newFakeSettings(true))
	var calls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&calls))
	forced := e.enqueue(typeStt, 3)
	other := e.enqueue(typeStt, 3)
	if err := e.svc.ForceRun(context.Background(), forced); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.waitStatus(forced, job.StatusCompleted)
	e.waitStatus(other, job.StatusWaitingForPower)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want only the forced job", calls.Load())
	}
}

func TestForceRunRefusals(t *testing.T) {
	e := newPowerEnv(t, nil, nil)
	e.register(typeStt, countingHandler(new(atomic.Int32)))
	done := e.enqueue(typeStt, 3)
	retrying := e.enqueue(typeStt, 3)
	for id, mutate := range map[shared.JobID]func(*job.Job) error{
		done: func(j *job.Job) error { return j.Cancel(t0) },
		retrying: func(j *job.Job) error {
			if err := j.Claim(t0); err != nil {
				return err
			}
			return j.Retry(t0, t0.Add(time.Minute), "x")
		},
	} {
		j, err := e.repo.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if err := mutate(j); err != nil {
			t.Fatal(err)
		}
		if err := e.repo.Save(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		id   shared.JobID
		want error
	}{
		{"cancelled job", done, ErrCannotRunNow},
		{"job in RETRY_WAIT", retrying, ErrCannotRunNow},
		{"unknown job", "job-404", shared.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := e.svc.ForceRun(context.Background(), tc.id); !errors.Is(err, tc.want) {
				t.Fatalf("ForceRun = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestUnknownPowerSourceCountsAsAC(t *testing.T) {
	tests := []struct {
		name  string
		power PowerMonitor
	}{
		{"no monitor", nil},
		{"monitor that cannot tell", brokenPower{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPowerEnv(t, tc.power, newFakeSettings(true))
			var calls atomic.Int32
			e.registerHeavy(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
				calls.Add(1)
				return report(ctx, mustProgress(t, 50, "chunk-1"))
			})
			id := e.enqueue(typeStt, 3)
			e.start()
			e.waitStatus(id, job.StatusCompleted)
			if calls.Load() != 1 {
				t.Fatalf("calls = %d", calls.Load())
			}
		})
	}
}

func TestWithoutEventsTheKeeperPollsThePower(t *testing.T) {
	fake := testenv.NewFakePower(false)
	e := newPowerEnv(t, pollOnlyPower{fake}, newFakeSettings(true))
	var calls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&calls))
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusWaitingForPower)

	fake.Set(true)
	e.drive("the poll sees AC and the job runs", func() bool { return e.status(id) == job.StatusCompleted })
}

func TestSettingsFailureKeepsTheLastValue(t *testing.T) {
	st := newFakeSettings(false)
	e := newPowerEnv(t, testenv.NewFakePower(false), st)
	var calls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&calls))
	e.start()
	eventually(t, "the setting read at start", func() bool { return !e.svc.conditions().OnlyOnACPower })

	st.fail(errors.New("database is locked"))
	e.idle(1)
	e.clock.Advance(time.Second)
	e.idle(1)
	id := e.enqueue(typeStt, 3)
	e.drive("the job runs with the last known setting", func() bool { return e.status(id) == job.StatusCompleted })
}

func TestSettingsFailureAtStartUsesTheDefault(t *testing.T) {
	st := newFakeSettings(false)
	st.fail(errors.New("database is locked"))
	e := newPowerEnv(t, testenv.NewFakePower(false), st)
	var calls atomic.Int32
	e.registerHeavy(typeStt, countingHandler(&calls))
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusWaitingForPower) // the default is "only on AC power" (AC-21)
	if calls.Load() != 0 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestPowerLostErrorWithoutAStopIsAnOrdinaryFailure(t *testing.T) {
	e := newPowerEnv(t, testenv.NewFakePower(true), newFakeSettings(true))
	e.registerHeavy(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		return powerpolicy.ErrPowerLost // not from report: the runner did not ask the handler to stop
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	e.waitStatus(id, job.StatusRetryWait)
}

func TestLightJobsIgnoreThePowerSource(t *testing.T) {
	e := newPowerEnv(t, testenv.NewFakePower(false), newFakeSettings(true))
	reportErr := make(chan error, 1)
	e.register(typeMail, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		reportErr <- report(ctx, mustProgress(t, 50, ""))
		return nil
	})
	id := e.enqueue(typeMail, 3)
	e.start()
	e.waitStatus(id, job.StatusCompleted)
	if err := <-reportErr; err != nil {
		t.Fatalf("report of a light job on battery = %v", err)
	}
}
