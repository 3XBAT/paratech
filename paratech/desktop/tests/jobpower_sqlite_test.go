package tests

// Scenario tests of power-aware processing (TASK-012, PRD section 7.4, AC-21, AC-22) on the real SQLite adapter:
// only the clock and the power source are fake (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const (
	powerHeavyType job.Type = "stt"
	powerLightType job.Type = "summarize"

	// maxChunks bounds the reporting loop of the heavy handler in case the power loss is never seen.
	maxChunks = 100000
)

// powerEnv is a runner with a heavy and a light handler on a real database.
type powerEnv struct {
	t        *testing.T
	db       *sqlite.DB
	jobs     *sqlite.JobRepository
	settings *sqlite.SettingsRepository
	clock    *testenv.FakeClock
	svc      *jobrunner.Service
	seq      int
}

func newPowerEnv(t *testing.T, power jobrunner.PowerMonitor, heavy, light jobrunner.Handler) *powerEnv {
	t.Helper()
	db := openRunnerDB(t, runnerDB(t))
	e := &powerEnv{
		t: t, db: db, jobs: sqlite.NewJobRepository(db), settings: sqlite.NewSettingsRepository(db),
		clock: testenv.NewFakeClock(runnerT0),
	}
	svc, err := jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock, Power: power, Settings: e.settings},
		runnerConfig(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, reg := range []jobrunner.Registration{
		{Type: powerHeavyType, Handler: heavy, Recovery: jobrunner.RecoveryRequeue, Heavy: true},
		{Type: powerLightType, Handler: light, Recovery: jobrunner.RecoveryRequeue},
	} {
		if err := svc.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return e
}

func (e *powerEnv) enqueue(typ job.Type) shared.JobID {
	e.t.Helper()
	e.seq++
	id := shared.JobID(fmt.Sprintf("job-%d", e.seq))
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: string(id), MaxAttempts: 3}, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, created, err := e.jobs.Enqueue(context.Background(), j); err != nil || !created {
		e.t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
	return id
}

func (e *powerEnv) get(id shared.JobID) *job.Job {
	e.t.Helper()
	j, err := e.jobs.Get(context.Background(), id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return j
}

func (e *powerEnv) status(id shared.JobID) job.Status { return e.get(id).Status() }

func (e *powerEnv) drive(what string, cond func() bool) {
	e.t.Helper()
	testenv.Eventually(e.t, what, cond, func() { e.clock.Advance(time.Second) })
}

// setOnlyOnAC changes the user setting through the settings aggregate and the real store.
func (e *powerEnv) setOnlyOnAC(on bool) {
	e.t.Helper()
	st, err := e.settings.Load(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	if err := st.SetProcessOnlyOnACPower(on, e.clock.Now()); err != nil {
		e.t.Fatal(err)
	}
	if err := e.settings.Save(context.Background(), st); err != nil {
		e.t.Fatal(err)
	}
}

// chunkHandler is a heavy handler that resumes from its checkpoint: a job without one reports chunk after chunk until
// the runner tells it to stop, a job with one finishes at once.
type chunkHandler struct {
	mu      sync.Mutex
	calls   map[shared.JobID][]string
	started chan shared.JobID
	stopErr chan error
}

func newChunkHandler() *chunkHandler {
	return &chunkHandler{calls: map[shared.JobID][]string{}, started: make(chan shared.JobID, 8), stopErr: make(chan error, 8)}
}

func (h *chunkHandler) Handle(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	h.mu.Lock()
	h.calls[snap.ID()] = append(h.calls[snap.ID()], fmt.Sprintf("attempt=%d checkpoint=%s", snap.Attempt(), snap.Checkpoint()))
	h.mu.Unlock()
	if snap.Checkpoint() != "" {
		return nil
	}

	h.started <- snap.ID()
	for i := 1; i <= maxChunks; i++ {
		p, err := job.NewProgress(i%100, fmt.Sprintf("chunk-%d", i))
		if err != nil {
			return err
		}
		if err := report(ctx, p); err != nil {
			h.stopErr <- err
			return fmt.Errorf("stopped after chunk %d: %w", i, err)
		}
		time.Sleep(time.Millisecond)
	}
	return nil
}

func (h *chunkHandler) of(id shared.JobID) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls[id]...)
}

// quickHandler finishes at once and counts its calls.
type quickHandler struct{ calls atomic.Int32 }

func (h *quickHandler) Handle(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
	h.calls.Add(1)
	return nil
}

func TestPowerAwareProcessingOnSQLite(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	power := testenv.NewFakePower(false)
	heavy, light := newChunkHandler(), &quickHandler{}
	e := newPowerEnv(t, power, heavy, light)

	// On battery with the default setting: the heavy job waits, the light one runs (AC-21).
	stt := e.enqueue(powerHeavyType)
	summary := e.enqueue(powerLightType)
	if err := e.svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.drive("the heavy job waits and the light job completes", func() bool {
		return e.status(stt) == job.StatusWaitingForPower && e.status(summary) == job.StatusCompleted
	})
	for range 10 {
		e.clock.Advance(time.Second)
	}
	if calls := heavy.of(stt); len(calls) != 0 || e.status(stt) != job.StatusWaitingForPower {
		t.Fatalf("heavy job on battery: calls=%v status=%s", calls, e.status(stt))
	}

	// AC returns: the job runs; power is lost mid-run: it stops at a checkpoint and waits again.
	power.Set(true)
	e.drive("the heavy job starts on AC", func() bool { return len(heavy.of(stt)) == 1 })
	<-heavy.started
	power.Set(false)
	if err := <-heavy.stopErr; !errors.Is(err, powerpolicy.ErrPowerLost) {
		t.Fatalf("the handler was stopped with %v, want ErrPowerLost", err)
	}
	runnerEventually(t, "WAITING_FOR_POWER after the power loss", func() bool { return e.status(stt) == job.StatusWaitingForPower })
	waiting := e.get(stt)
	checkpoint := waiting.Progress().Checkpoint()
	if checkpoint == "" || waiting.Attempts() != 0 || waiting.LastError() != "" {
		t.Fatalf("waiting job: checkpoint=%q attempts=%d lastError=%q", checkpoint, waiting.Attempts(), waiting.LastError())
	}

	// AC returns again: the job resumes from the saved checkpoint.
	power.Set(true)
	e.drive("the heavy job resumes and completes", func() bool { return e.status(stt) == job.StatusCompleted })
	want := []string{"attempt=1 checkpoint=", "attempt=1 checkpoint=" + checkpoint}
	if got := heavy.of(stt); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("heavy calls = %v, want %v", got, want)
	}

	// The setting is off: heavy work runs on battery (AC-22).
	power.Set(false)
	e.setOnlyOnAC(false)
	quick := e.enqueue(powerHeavyType)
	e.drive("the heavy job starts on battery", func() bool { return len(heavy.of(quick)) == 1 })
	<-heavy.started
	e.setOnlyOnAC(true) // and on again: the running job stops at its next checkpoint once a poll re-reads the setting
	var stopErr error
	e.drive("the running job is stopped", func() bool {
		select {
		case stopErr = <-heavy.stopErr:
			return true
		default:
			return false
		}
	})
	if !errors.Is(stopErr, powerpolicy.ErrPowerLost) {
		t.Fatalf("the handler was stopped with %v, want ErrPowerLost", stopErr)
	}
	e.drive("the job waits once the setting is on again", func() bool { return e.status(quick) == job.StatusWaitingForPower })

	// "Run now" starts that one job on battery and the attempt is not stopped (AC-22).
	if err := e.svc.ForceRun(context.Background(), quick); err != nil {
		t.Fatal(err)
	}
	e.drive("the forced job completes on battery", func() bool { return e.status(quick) == job.StatusCompleted })
	if got := e.get(quick); got.RunNow() || got.Attempts() != 1 {
		t.Fatalf("forced job: runNow=%v attempts=%d", got.RunNow(), got.Attempts())
	}

	// The override covered that job only.
	later := e.enqueue(powerHeavyType)
	e.drive("another heavy job waits", func() bool { return e.status(later) == job.StatusWaitingForPower })
	if light.calls.Load() != 1 {
		t.Fatalf("light calls = %d", light.calls.Load())
	}
}

// unknownPower cannot read the power source, like a desktop without a battery whose status query fails.
type unknownPower struct{}

func (unknownPower) IsOnACPower(context.Context) (bool, error) {
	return false, errors.New("no power status")
}

func (unknownPower) Subscribe(context.Context) (<-chan bool, error) {
	return nil, errors.New("no power events")
}

func TestUnknownPowerSourceCountsAsACOnSQLite(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	heavy := &quickHandler{}
	e := newPowerEnv(t, unknownPower{}, heavy, &quickHandler{})
	id := e.enqueue(powerHeavyType)
	if err := e.svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.drive("the heavy job completes", func() bool { return e.status(id) == job.StatusCompleted })
	if heavy.calls.Load() != 1 {
		t.Fatalf("calls = %d", heavy.calls.Load())
	}
}
