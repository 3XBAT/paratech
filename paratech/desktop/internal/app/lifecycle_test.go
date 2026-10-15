package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/testenv"
)

// events is the order in which the fakes were called.
type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, name)
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

func (e *events) index(t *testing.T, name string) int {
	t.Helper()
	i := slices.Index(e.all(), name)
	if i < 0 {
		t.Fatalf("%s not called; events %v", name, e.all())
	}
	return i
}

type fakeRunner struct {
	ev         *events
	recoverErr error
	startErr   error
}

func (f *fakeRunner) Recover(context.Context) (jobrunner.RecoveryReport, error) {
	f.ev.add("runner.Recover")
	return jobrunner.RecoveryReport{}, f.recoverErr
}

func (f *fakeRunner) Start(context.Context) error {
	f.ev.add("runner.Start")
	return f.startErr
}

func (f *fakeRunner) Stop(context.Context) error {
	f.ev.add("runner.Stop")
	return nil
}

// fakeRecordings blocks Recover until Close, like a long assembly that only a shutdown ends.
type fakeRecordings struct {
	ev      *events
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newFakeRecordings(ev *events) *fakeRecordings {
	return &fakeRecordings{ev: ev, entered: make(chan struct{}), closed: make(chan struct{})}
}

func (f *fakeRecordings) Recover(ctx context.Context) ([]recordingsession.Info, error) {
	f.ev.add("recordings.Recover")
	close(f.entered)
	<-f.closed
	f.ev.add("recordings.Recover returned")
	return nil, recordingsession.ErrClosed
}

func (f *fakeRecordings) Close(context.Context) error {
	f.ev.add("recordings.Close")
	f.once.Do(func() { close(f.closed) })
	return nil
}

type fakeDeliveries struct {
	ev         *events
	recoverErr error
	// launched, when set, is waited for (bounded) by Recover: the recording recovery runs in a goroutine, so only a
	// wait makes "launched before" observable without a race.
	launched <-chan struct{}
}

func (f *fakeDeliveries) Recover(context.Context) (int, error) {
	if f.launched != nil {
		select {
		case <-f.launched:
		case <-time.After(5 * time.Second):
			f.ev.add("recording recovery not launched")
		}
	}
	f.ev.add("deliveries.Recover")
	return 0, f.recoverErr
}

func (f *fakeDeliveries) Resume(context.Context) error {
	f.ev.add("deliveries.Resume")
	return nil
}

type lifeFakes struct {
	ev         *events
	runner     *fakeRunner
	recordings *fakeRecordings
	deliveries *fakeDeliveries
	life       *lifecycle
}

func newLifeFakes() *lifeFakes {
	ev := &events{}
	f := &lifeFakes{ev: ev, runner: &fakeRunner{ev: ev}, recordings: newFakeRecordings(ev), deliveries: &fakeDeliveries{ev: ev}}
	step := func(name string, err error) cleanup {
		return cleanup{name: name, run: func(context.Context) (int, error) { ev.add(name); return 1, err }}
	}
	release := func(name string) closer { return closer{name: name, close: func() error { ev.add(name); return nil }} }
	f.life = &lifecycle{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)), runner: f.runner, recordings: f.recordings, deliveries: f.deliveries,
		cleanups:    []cleanup{step("cleanup work", nil), step("cleanup temp", errors.New("disk busy"))},
		closers:     []closer{release("close storage"), release("release lock")},
		stopTimeout: time.Second,
	}
	return f
}

// The start order of TASK-056; the recording recovery runs in the background and does not hold up the workers.
func TestStartOrder(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newLifeFakes()
	f.deliveries.recoverErr = errors.New("database busy") // logged, the start goes on
	f.deliveries.launched = f.recordings.entered

	if err := f.life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-f.recordings.entered

	got := f.ev.all()
	want := []string{"cleanup work", "cleanup temp", "runner.Recover", "deliveries.Recover", "deliveries.Resume", "runner.Start"}
	without := slices.DeleteFunc(slices.Clone(got), func(s string) bool { return s == "recordings.Recover" })
	if !slices.Equal(without, want) {
		t.Fatalf("start order %v, want %v (plus the recording recovery after the job recovery)", got, want)
	}
	if f.ev.index(t, "recordings.Recover") < f.ev.index(t, "runner.Recover") {
		t.Fatalf("the recording recovery ran before the job recovery: %v", got)
	}
	if f.ev.index(t, "recordings.Recover") > f.ev.index(t, "deliveries.Recover") {
		t.Fatalf("the recording recovery was launched after the delivery recovery: %v", got)
	}
	if slices.Contains(got, "recordings.Recover returned") {
		t.Fatal("the recording recovery must still be running: the fake blocks until Close")
	}

	if err := f.life.shutdown(); err != nil {
		t.Fatal(err)
	}
}

// Shutdown is the reverse: the workers, then the recordings (which ends the background recovery), then the storage
// and last the instance lock. It is safe to repeat.
func TestShutdownOrder(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newLifeFakes()
	if err := f.life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-f.recordings.entered
	n := len(f.ev.all())

	for i := range 2 {
		if err := f.life.shutdown(); err != nil {
			t.Fatalf("shutdown #%d: %v", i+1, err)
		}
	}
	got := f.ev.all()[n:]
	want := []string{"runner.Stop", "recordings.Close", "recordings.Recover returned", "close storage", "release lock"}
	if !slices.Equal(got, want) {
		t.Fatalf("shutdown order %v, want %v", got, want)
	}
}

func TestShutdownWithoutStart(t *testing.T) {
	f := newLifeFakes()
	if err := f.life.shutdown(); err != nil {
		t.Fatal(err)
	}
	want := []string{"runner.Stop", "recordings.Close", "close storage", "release lock"}
	if got := f.ev.all(); !slices.Equal(got, want) {
		t.Fatalf("shutdown %v, want %v", got, want)
	}
}

// A failed job recovery or worker start stops what started; the storage stays for the UI until shutdown.
func TestStartUnwinds(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name  string
		setup func(*lifeFakes)
		ctx   func() context.Context
		want  []string
	}{
		{
			name:  "job recovery fails",
			setup: func(f *lifeFakes) { f.runner.recoverErr = boom },
			want:  []string{"cleanup work", "cleanup temp", "runner.Recover", "runner.Stop", "recordings.Close"},
		},
		{
			name:  "worker start fails",
			setup: func(f *lifeFakes) { f.runner.startErr = boom },
			want: []string{"cleanup work", "cleanup temp", "runner.Recover", "deliveries.Recover", "deliveries.Resume",
				"runner.Start", "runner.Stop", "recordings.Close", "recordings.Recover returned"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			f := newLifeFakes()
			tt.setup(f)
			if err := f.life.start(context.Background()); !errors.Is(err, boom) {
				t.Fatalf("start = %v, want boom", err)
			}
			got := slices.DeleteFunc(f.ev.all(), func(s string) bool { return s == "recordings.Recover" })
			if !slices.Equal(got, tt.want) {
				t.Fatalf("events %v, want %v", got, tt.want)
			}
			if err := f.life.shutdown(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Close during the startup: the workers are not started.
func TestStartStopsWhenCancelled(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newLifeFakes()
	ctx, cancel := context.WithCancel(context.Background())
	f.life.cleanups = append(f.life.cleanups, cleanup{name: "cancel", run: func(context.Context) (int, error) {
		cancel()
		return 0, nil
	}})

	if err := f.life.start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("start = %v, want context.Canceled", err)
	}
	if slices.Contains(f.ev.all(), "runner.Start") {
		t.Fatalf("the workers started after the cancellation: %v", f.ev.all())
	}
	if err := f.life.shutdown(); err != nil {
		t.Fatal(err)
	}
}

// The Started hook of the UI shell starts the core once, in the background; Close ends a startup that still runs and
// waits for it. Before openCore and after Close the hook does nothing.
func TestStartedHook(t *testing.T) {
	a, _ := newTestAppWith(t, t.TempDir())
	a.launchStart() // the core is not open: nothing to start

	if _, err := a.openCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	cancelled := make(chan error, 1)
	a.life.cleanups = append([]cleanup{{name: "block", run: func(ctx context.Context) (int, error) {
		entered <- struct{}{}
		<-ctx.Done()
		cancelled <- ctx.Err()
		return 0, ctx.Err()
	}}}, a.life.cleanups...)

	a.launchStart()
	a.launchStart() // repeated ApplicationStarted: ignored
	<-entered

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("startup context: %v, want cancelled by Close", err)
	}
	select {
	case <-entered:
		t.Fatal("the startup ran twice")
	default:
	}
	a.launchStart() // closed: nothing happens
}
