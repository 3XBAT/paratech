package recordingsession

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const beatEvery = 20 * time.Second

func heartbeatConfig() Config {
	cfg := DefaultConfig()
	cfg.HeartbeatInterval = beatEvery
	return cfg
}

// awaitHeartbeat waits until the stored heartbeat of id shows at.
func (e *env) awaitHeartbeat(id shared.RecordingID, at time.Time) {
	e.t.Helper()
	testenv.Eventually(e.t, "heartbeat at "+at.String(), func() bool {
		r, err := e.repo.Get(testCtx(e.t), id)
		return err == nil && r.HeartbeatAt().Equal(at)
	}, nil)
}

// beat moves the clock by one heartbeat interval once the watch of the session sleeps for it. Waiting for any sleeper
// is not enough: the timer of a detached save may still sleep, and an advance before the watch sleeps again is lost.
func (e *env) beat() {
	e.t.Helper()
	e.sleeps.awaitOnly(e.t, beatEvery)
	e.clock.Advance(beatEvery)
}

// sleepLog is a Sleeper on a fake clock that records the sleeps in flight by duration, so that a test can tell the
// sleep it waits for from the others.
type sleepLog struct {
	clock *testenv.FakeClock

	mu       sync.Mutex
	started  int // sleeps begun
	inFlight map[time.Duration]int
}

func newSleepLog(clock *testenv.FakeClock) *sleepLog {
	return &sleepLog{clock: clock, inFlight: map[time.Duration]int{}}
}

func (l *sleepLog) Sleep(ctx context.Context, d time.Duration) error {
	l.mu.Lock()
	l.started++
	l.inFlight[d]++
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.inFlight[d]--
		l.mu.Unlock()
	}()
	return l.clock.Sleep(ctx, d)
}

// only reports whether the single sleep in flight is one of d, and how many sleeps were begun.
func (l *sleepLog) only(d time.Duration) (started int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, n := range l.inFlight {
		total += n
	}
	return l.started, total == 1 && l.inFlight[d] == 1
}

// awaitOnly waits until the only sleep is one of d and the clock holds it. A sleep counts as in flight before it
// reaches the clock and after it left it, so a stable count around the look at the clock means the clock holds that
// very sleep.
func (l *sleepLog) awaitOnly(t *testing.T, d time.Duration) {
	t.Helper()
	testenv.Eventually(t, "a single sleep of "+d.String(), func() bool {
		before, ok := l.only(d)
		held := l.clock.Sleepers() == 1
		after, still := l.only(d)
		return ok && held && still && before == after
	}, nil)
}

// AC-05 (PRD section 6.2 watchdog): the session stores the heartbeat of a running capture at the interval.
func TestHeartbeatIsWrittenAtTheInterval(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, heartbeatConfig())
	info := e.start()
	if r := e.stored(info.ID); !r.HeartbeatAt().Equal(t0) {
		t.Fatalf("heartbeat after the start = %v", r.HeartbeatAt())
	}

	for i := 1; i <= 3; i++ {
		e.beat()
		e.awaitHeartbeat(info.ID, t0.Add(time.Duration(i)*beatEvery))
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording,
		recording.StatusRecording, recording.StatusRecording, recording.StatusRecording)

	e.feed(info.ID, "a")
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

// A heartbeat whose save fails is logged and the capture goes on; the next one is stored.
func TestHeartbeatSaveErrorKeepsTheSession(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, heartbeatConfig())
	info := e.start()

	before := e.repo.saveCalls()
	e.repo.failNextSaves(shared.ErrStorageBusy)
	e.beat()
	testenv.Eventually(t, "the failed heartbeat save", func() bool { return e.repo.saveCalls() == before+1 }, nil)
	if !e.rec.Running(info.ID) || e.stored(info.ID).Status() != recording.StatusRecording {
		t.Fatal("a failed heartbeat save ended the session")
	}

	e.beat()
	e.awaitHeartbeat(info.ID, t0.Add(2*beatEvery))
	if _, active := e.svc.Active(); !active {
		t.Fatal("the session ended")
	}
}

// A heartbeat that finds the recording changed elsewhere stops the capture and never overwrites the other writer.
func TestHeartbeatConflictStopsTheSession(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, heartbeatConfig())
	info := e.start()

	other := e.stored(info.ID)
	if err := other.RequireRecovery(t0.Add(time.Second), "taken over"); err != nil {
		t.Fatal(err)
	}
	e.repo.put(other)

	e.beat()
	testenv.Eventually(t, "session end", func() bool { _, active := e.svc.Active(); return !active }, nil)
	if e.rec.Running(info.ID) {
		t.Fatal("capture still runs")
	}
	if r := e.stored(info.ID); r.Status() != recording.StatusRecoveryRequired || r.Problem() != "taken over" {
		t.Fatalf("the session overwrote the other writer: %s %q", r.Status(), r.Problem())
	}
	if _, err := e.svc.Stop(testCtx(t), info.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop = %v", err)
	}
}

// The heartbeat does not delay the hard cap: one timer serves both.
func TestHeartbeatUntilTheCap(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := heartbeatConfig()
	cfg.MaxDuration = 3 * beatEvery // two beats, then the cap on the third tick
	e := newEnv(t, cfg)
	info := e.start()
	e.feed(info.ID, "a")

	e.beat()
	e.awaitHeartbeat(info.ID, t0.Add(beatEvery))
	e.beat()
	e.awaitHeartbeat(info.ID, t0.Add(2*beatEvery))
	e.beat()
	r := e.awaitStatus(info.ID, recording.StatusCompleted)
	if !r.StoppedAt().Equal(t0.Add(cfg.MaxDuration)) || !r.HeartbeatAt().Equal(t0.Add(2*beatEvery)) {
		t.Fatalf("stopped %v, heartbeat %v", r.StoppedAt(), r.HeartbeatAt())
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording, recording.StatusRecording,
		recording.StatusRecording, recording.StatusFinalizing, recording.StatusCompleted)
}
