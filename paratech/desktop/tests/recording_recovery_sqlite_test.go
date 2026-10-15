package tests

// Scenario tests of the heartbeat and the recovery of interrupted recordings (TASK-014, PRD section 6.2, section 14
// case 1, AC-05) on the real SQLite adapter and the real Storage Manager: only the clock and the audio source are fake.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// crashDuringCapture leaves what a process killed during the capture of id leaves: a RECORDING row with its last
// heartbeat and the chunk files. The recorder of the dead process is simply dropped.
func (e *recordingEnv) crashDuringCapture(id shared.RecordingID, chunks func(id shared.RecordingID)) {
	e.t.Helper()
	ctx := context.Background()
	rec, err := recording.New(recording.Spec{ID: id, MaxDuration: 3 * time.Hour}, recT0)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return rec.Begin(recT0) },
		func() error { return e.repo.Save(ctx, rec) },
		func() error { return rec.Heartbeat(recT0.Add(30 * time.Second)) },
		func() error { return e.repo.Save(ctx, rec) },
		func() error { return e.rec.Start(ctx, id, rec.ChunksDir()) },
	} {
		if err := step(); err != nil {
			e.t.Fatal(err)
		}
	}
	chunks(id)
	if err := e.close(ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *recordingEnv) recoverAll() []recordingsession.Info {
	e.t.Helper()
	infos, err := e.svc.Recover(context.Background())
	if err != nil {
		e.t.Fatalf("Recover: %v", err)
	}
	return infos
}

// Test step 2: three chunks without finalization, a new manager marks the recording RECOVERY_REQUIRED and assembles it.
func TestRecoveryAfterACrashDuringTheCapture(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dir := t.TempDir()
	first := newRecordingEnv(t, dir)
	first.crashDuringCapture("crashed", func(id shared.RecordingID) { first.feed(id, "one-", "two-", "three") })

	again := newRecordingEnv(t, dir)
	ctx := context.Background()
	stored, err := again.repo.Get(ctx, "crashed")
	if err != nil || stored.Status() != recording.StatusRecording || !stored.HeartbeatAt().Equal(recT0.Add(30*time.Second)) {
		t.Fatalf("after the crash: %v, %v", stored, err)
	}

	entered, release := again.rec.HoldFinalize()
	type result struct {
		infos []recordingsession.Info
		err   error
	}
	done := make(chan result, 1)
	go func() {
		infos, err := again.svc.Recover(ctx)
		done <- result{infos, err}
	}()
	<-entered
	marked, err := again.repo.Get(ctx, "crashed")
	if err != nil || marked.Status() != recording.StatusRecoveryRequired || marked.Problem() == "" {
		t.Fatalf("during the recovery: %v, %v", marked, err)
	}
	release()
	res := <-done
	if res.err != nil || len(res.infos) != 1 || res.infos[0].Status != recording.StatusCompleted {
		t.Fatalf("Recover = %+v, %v", res.infos, res.err)
	}

	r, err := again.repo.Get(ctx, "crashed")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := r.Duration()
	if r.Status() != recording.StatusCompleted || r.Chunks() != 3 || d != 3*time.Minute ||
		!r.StoppedAt().Equal(recT0.Add(30*time.Second)) {
		t.Fatalf("recovered: %s, %d chunks, %v, stopped %v", r.Status(), r.Chunks(), d, r.StoppedAt())
	}
	if track := readFile(t, filepath.Join(again.root, "recordings", "crashed.wav")); track != "one-two-three" {
		t.Fatalf("final track = %q", track)
	}
	if infos := again.recoverAll(); len(infos) != 0 {
		t.Fatalf("a repeated recovery found %+v", infos)
	}
}

// Test step 3: the last chunk, cut short by the crash, is dropped; the others make the final track.
func TestRecoveryDropsABrokenLastChunk(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dir := t.TempDir()
	first := newRecordingEnv(t, dir)
	first.crashDuringCapture("crashed", func(id shared.RecordingID) {
		first.feed(id, "one-", "two-")
		if _, err := first.rec.FeedTruncated(context.Background(), id, []byte("three")); err != nil {
			t.Fatal(err)
		}
	})

	again := newRecordingEnv(t, dir)
	infos := again.recoverAll()
	if len(infos) != 1 || infos[0].Status != recording.StatusCompleted || infos[0].Chunks != 3 ||
		infos[0].Duration != 2*time.Minute {
		t.Fatalf("Recover = %+v", infos)
	}
	if track := readFile(t, filepath.Join(again.root, "recordings", "crashed.wav")); track != "one-two-" {
		t.Fatalf("final track = %q", track)
	}
	if broken := readFile(t, filepath.Join(again.root, "recordings", "crashed", "chunk-0003")); len(broken) == 0 {
		t.Fatal("the broken chunk was removed")
	}
}

// A recording whose only chunk is broken has no audio: it fails and its files stay.
func TestRecoveryWithoutReadableAudioFails(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dir := t.TempDir()
	first := newRecordingEnv(t, dir)
	first.crashDuringCapture("crashed", func(id shared.RecordingID) {
		if _, err := first.rec.FeedTruncated(context.Background(), id, []byte("one")); err != nil {
			t.Fatal(err)
		}
	})

	again := newRecordingEnv(t, dir)
	infos := again.recoverAll()
	if len(infos) != 1 || infos[0].Status != recording.StatusFailed || infos[0].Problem == "" {
		t.Fatalf("Recover = %+v", infos)
	}
	if got := readFile(t, filepath.Join(again.root, "recordings", "crashed", "chunk-0001")); len(got) == 0 {
		t.Fatal("the chunk was removed")
	}
	if err := again.svc.Delete(context.Background(), "crashed"); err != nil {
		t.Fatalf("Delete of the failed recording = %v", err)
	}
}

// A forced shutdown during the finalization leaves FINALIZING; the next start finishes it.
func TestRecoveryFinishesAnInterruptedFinalization(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dir := t.TempDir()
	e := newRecordingEnv(t, dir)
	ctx := context.Background()
	info, err := e.svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e.feed(info.ID, "a", "b")
	entered, release := e.rec.HoldFinalize()
	defer release()
	shutdown, cancel := context.WithCancel(ctx)
	closed := make(chan error, 1)
	go func() { closed <- e.close(shutdown) }()
	<-entered
	cancel()
	if err := <-closed; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close = %v", err)
	}

	again := newRecordingEnv(t, dir)
	infos := again.recoverAll()
	if len(infos) != 1 || infos[0].ID != info.ID || infos[0].Status != recording.StatusCompleted || infos[0].Duration != 2*time.Minute {
		t.Fatalf("Recover = %+v", infos)
	}
	if track := readFile(t, filepath.Join(again.root, "recordings", string(info.ID)+".wav")); track != "ab" {
		t.Fatalf("final track = %q", track)
	}
}

// AC: the heartbeat of the active recording is stored in the database at the interval.
func TestHeartbeatIsStored(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := recordingsession.DefaultConfig()
	e := newRecordingEnvWith(t, t.TempDir(), cfg)
	sleeps := e.trackSleeps(cfg)
	ctx := context.Background()
	info, err := e.svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 2; i++ {
		sleeps.awaitOnly(t, cfg.HeartbeatInterval)
		e.clock.Advance(cfg.HeartbeatInterval)
		want := recT0.Add(time.Duration(i) * cfg.HeartbeatInterval)
		testenv.Eventually(t, "stored heartbeat", func() bool {
			r, err := e.repo.Get(ctx, info.ID)
			return err == nil && r.Status() == recording.StatusRecording && r.HeartbeatAt().Equal(want)
		}, nil)
	}

	e.feed(info.ID, "a")
	if got, err := e.svc.Stop(ctx, info.ID); err != nil || got.Status != recording.StatusCompleted {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

// trackSleeps replaces the service of the idle env by one with the same ports whose Sleeper records the sleeps in
// flight, so that a heartbeat test advances the clock only once the watch of the session sleeps.
func (e *recordingEnv) trackSleeps(cfg recordingsession.Config) *recSleepLog {
	e.t.Helper()
	if err := e.svc.Close(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	sleeps := &recSleepLog{clock: e.clock, inFlight: map[time.Duration]int{}}
	svc, err := recordingsession.New(recordingsession.Deps{
		Recordings: e.repo, Transcripts: sqlite.NewRecordingTranscripts(e.db), Recorder: e.rec, Artifacts: e.store,
		Tx: e.db, Clock: e.clock, Sleeper: sleeps, IDs: &recIDs{}, Completions: ignoredCompletions{},
	}, cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	e.svc = svc
	return sleeps
}

// recSleepLog is a Sleeper on the fake clock that records the sleeps in flight by duration. Waiting for any sleeper
// is not enough: the timer of a detached save may still sleep, and an advance before the watch sleeps again is lost.
type recSleepLog struct {
	clock *testenv.FakeClock

	mu       sync.Mutex
	started  int
	inFlight map[time.Duration]int
}

func (l *recSleepLog) Sleep(ctx context.Context, d time.Duration) error {
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

func (l *recSleepLog) only(d time.Duration) (started int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, n := range l.inFlight {
		total += n
	}
	return l.started, total == 1 && l.inFlight[d] == 1
}

// awaitOnly waits until the only sleep is one of d and the clock holds it (see sleepLog in the recordingsession
// tests for why the count is read twice).
func (l *recSleepLog) awaitOnly(t *testing.T, d time.Duration) {
	t.Helper()
	testenv.Eventually(t, "a single sleep of "+d.String(), func() bool {
		before, ok := l.only(d)
		held := l.clock.Sleepers() == 1
		after, still := l.only(d)
		return ok && held && still && before == after
	}, nil)
}
