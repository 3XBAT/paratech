package recordingsession

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// restart builds the Service of a new process on the same database and files: a fresh recorder and no sessions.
func (e *env) restart() *env {
	e.t.Helper()
	n := &env{t: e.t, clock: e.clock, repo: e.repo, tr: e.tr, tx: e.tx, files: e.files}
	n.rec = testenv.NewFakeRecorder(n.files, chunkLen)
	n.hooks = &hookRecorder{FakeRecorder: n.rec}
	svc, err := New(Deps{
		Recordings: n.repo, Transcripts: n.tr, Recorder: n.hooks, Artifacts: n.files, Tx: n.tx,
		Clock: n.clock, Sleeper: n.clock, IDs: &seqIDs{n: 100},
	}, DefaultConfig())
	if err != nil {
		e.t.Fatal(err)
	}
	n.svc = svc
	e.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Close(ctx); err != nil {
			e.t.Errorf("Close: %v", err)
		}
	})
	return n
}

// chunk is the content of a chunk file of the fake recorder; broken cuts it short as a crash would.
type chunk struct {
	payload string
	broken  bool
}

// leftBehind stores a recording in status s as an earlier process left it, with its chunk files.
func (e *env) leftBehind(id shared.RecordingID, s recording.Status, created time.Time, chunks ...chunk) {
	e.t.Helper()
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: limits.DefaultMaxDuration}, created)
	if err != nil {
		e.t.Fatal(err)
	}
	later := created.Add(10 * time.Minute)
	steps := map[recording.Status][]func() error{
		recording.StatusRecording: {func() error { return r.Begin(created) }, func() error { return r.Heartbeat(later) }},
		recording.StatusStopping:  {func() error { return r.Begin(created) }, func() error { return r.Stop(later) }},
		recording.StatusFinalizing: {func() error { return r.Begin(created) }, func() error { return r.Stop(later) },
			func() error { return r.Finalize(later) }},
		recording.StatusRecoveryRequired: {func() error { return r.Begin(created) },
			func() error { return r.RequireRecovery(later, "found earlier") }},
		recording.StatusCompleted: {func() error { return r.Begin(created) }, func() error { return r.Stop(later) },
			func() error { return r.Finalize(later) }, func() error {
				track, err := r.TrackRef("wav")
				if err != nil {
					return err
				}
				return r.Complete(later, track, time.Minute)
			}},
		recording.StatusFailed: {func() error { return r.Fail(later, "no device") }},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			e.t.Fatal(err)
		}
	}
	e.repo.put(r)

	for i, c := range chunks {
		ref, err := r.ChunksDir().Child("chunk-000" + string(rune('1'+i)))
		if err != nil {
			e.t.Fatal(err)
		}
		content := testenv.EncodeChunk([]byte(c.payload))
		if c.broken {
			content = content[:len(content)-1]
		}
		if err := e.files.Write(testCtx(e.t), ref, bytes.NewReader(content)); err != nil {
			e.t.Fatal(err)
		}
	}
}

// recoverAdvancing runs Recover under a moving clock, so the retry delays pass.
func (e *env) recoverAdvancing() (infos []Info, err error) {
	e.t.Helper()
	ctx := testCtx(e.t)
	e.advancing(func() { infos, err = e.svc.Recover(ctx) })
	return infos, err
}

func trackRef(t *testing.T, id shared.RecordingID) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactRecordings, string(id)+".wav")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// AC-05, PRD section 14 case 1: a session that died during the capture leaves RECORDING; the next process marks it
// RECOVERY_REQUIRED before it touches the chunks and then assembles them.
func TestRecoverAfterACrashDuringTheCapture(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a", "b", "c")
	e.svc.shutdown() // the process dies: no stop, no finalization
	testenv.Eventually(t, "session end", func() bool { _, active := e.svc.Active(); return !active }, nil)
	if r := e.stored(info.ID); r.Status() != recording.StatusRecording {
		t.Fatalf("after the crash: %s", r.Status())
	}

	again := e.restart()
	entered, release := again.rec.HoldFinalize()
	type result struct {
		infos []Info
		err   error
	}
	done := make(chan result, 1)
	go func() {
		infos, err := again.svc.Recover(testCtx(t))
		done <- result{infos, err}
	}()
	<-entered
	if r := again.stored(info.ID); r.Status() != recording.StatusRecoveryRequired || r.Problem() != problemInterrupted {
		t.Fatalf("before the assembly: %s %q", r.Status(), r.Problem())
	}
	release()

	res := <-done
	if res.err != nil || len(res.infos) != 1 {
		t.Fatalf("Recover = %+v, %v", res.infos, res.err)
	}
	got := res.infos[0]
	if got.ID != info.ID || got.Status != recording.StatusCompleted || got.Chunks != 3 || got.Duration != 3*chunkLen ||
		got.Problem != problemInterrupted || !got.StoppedAt.Equal(t0) {
		t.Fatalf("recovered: %+v", got)
	}
	r := again.stored(info.ID)
	if r.Status() != recording.StatusCompleted || again.content(r.Audio()) != "abc" {
		t.Fatalf("stored: %s, track %s", r.Status(), r.Audio())
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording,
		recording.StatusRecoveryRequired, recording.StatusCompleted)
}

// The decision for every unfinished status and the handling of broken chunks (PRD section 6.3: a crash loses at
// most the latest chunk).
func TestRecoverEveryStatus(t *testing.T) {
	ab := []chunk{{payload: "a"}, {payload: "b"}}
	cases := []struct {
		name      string
		status    recording.Status
		chunks    []chunk
		want      recording.Status
		history   []recording.Status // saves of the recovery
		track     string             // "" = no final track
		problem   string             // contained in the problem text
		finalizes int
	}{
		{"starting", recording.StatusStarting, nil, recording.StatusFailed,
			[]recording.Status{recording.StatusFailed}, "", problemInterrupted, 0},
		{"recording", recording.StatusRecording, ab, recording.StatusCompleted,
			[]recording.Status{recording.StatusRecoveryRequired, recording.StatusCompleted}, "ab", problemInterrupted, 1},
		{"stopping", recording.StatusStopping, ab, recording.StatusCompleted,
			[]recording.Status{recording.StatusStopping, recording.StatusCompleted}, "ab", "", 1},
		{"finalizing", recording.StatusFinalizing, ab, recording.StatusCompleted,
			[]recording.Status{recording.StatusFinalizing, recording.StatusCompleted}, "ab", "", 1},
		{"recovery required", recording.StatusRecoveryRequired, ab, recording.StatusCompleted,
			[]recording.Status{recording.StatusRecoveryRequired, recording.StatusCompleted}, "ab", "found earlier", 1},
		{"broken last chunk is dropped", recording.StatusRecording,
			[]chunk{{payload: "a"}, {payload: "b"}, {payload: "c", broken: true}}, recording.StatusCompleted,
			[]recording.Status{recording.StatusRecoveryRequired, recording.StatusCompleted}, "ab", problemInterrupted, 1},
		{"no chunks", recording.StatusRecording, nil, recording.StatusFailed,
			[]recording.Status{recording.StatusRecoveryRequired, recording.StatusFailed}, "", problemRecoveryFailed, 1},
		{"only a broken chunk is not retried", recording.StatusRecording, []chunk{{payload: "a", broken: true}},
			recording.StatusFailed,
			[]recording.Status{recording.StatusRecoveryRequired, recording.StatusFailed}, "", problemRecoveryFailed, 1},
		{"stopping without chunks", recording.StatusStopping, nil, recording.StatusFailed,
			[]recording.Status{recording.StatusStopping, recording.StatusFailed}, "", problemRecoveryFailed, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newEnv(t, DefaultConfig())
			e.leftBehind("old", tc.status, t0.Add(-time.Hour), tc.chunks...)

			infos, err := e.svc.Recover(testCtx(t))
			if err != nil || len(infos) != 1 || infos[0].Status != tc.want || infos[0].Chunks != len(tc.chunks) {
				t.Fatalf("Recover = %+v, %v", infos, err)
			}
			requireHistory(t, e.repo.statuses(), tc.history...)
			if n := e.hooks.finalizeCalls(); n != tc.finalizes {
				t.Fatalf("Finalize called %d times, want %d", n, tc.finalizes)
			}

			r := e.stored("old")
			if !strings.Contains(r.Problem(), tc.problem) {
				t.Fatalf("problem = %q, want %q", r.Problem(), tc.problem)
			}
			if tc.track == "" {
				if !r.Audio().IsZero() || slices.Contains(e.files.Paths(), "recordings/old.wav") {
					t.Fatalf("a track without audio: %s %v", r.Audio(), e.files.Paths())
				}
			} else {
				d, _ := r.Duration()
				if r.Audio() != trackRef(t, "old") || e.content(r.Audio()) != tc.track || d != time.Duration(len(tc.track))*chunkLen {
					t.Fatalf("track %s, duration %v", r.Audio(), d)
				}
			}
			for i := range tc.chunks {
				if p := "recordings/old/chunk-000" + string(rune('1'+i)); !slices.Contains(e.files.Paths(), p) {
					t.Fatalf("chunk %s was removed", p)
				}
			}
		})
	}
}

func TestRecoverLeavesFinishedRecordings(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("done", recording.StatusCompleted, t0.Add(-2*time.Hour))
	e.leftBehind("failed", recording.StatusFailed, t0.Add(-time.Hour))

	infos, err := e.svc.Recover(testCtx(t))
	if err != nil || len(infos) != 0 || e.repo.saveCalls() != 0 {
		t.Fatalf("Recover = %+v, %v; %d saves", infos, err, e.repo.saveCalls())
	}
}

// A transient assembly error keeps the recording for the next start, with a problem for the UI; the next Recover
// finishes it.
func TestRecoverRetriesAtTheNextStart(t *testing.T) {
	for _, status := range []recording.Status{recording.StatusRecording, recording.StatusFinalizing} {
		t.Run(string(status), func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newEnv(t, DefaultConfig())
			e.leftBehind("old", status, t0.Add(-time.Hour), chunk{payload: "a"})
			for range finalizeAttempts {
				e.hooks.failFinalizes(shared.ErrStorageBusy)
			}

			kept := status
			if status == recording.StatusRecording {
				kept = recording.StatusRecoveryRequired
			}
			infos, err := e.recoverAdvancing()
			if err != nil || len(infos) != 1 || infos[0].Status != kept ||
				!strings.Contains(infos[0].Problem, problemRecoveryPending) {
				t.Fatalf("Recover = %+v, %v", infos, err)
			}
			if n := e.hooks.finalizeCalls(); n != finalizeAttempts {
				t.Fatalf("Finalize called %d times, want %d", n, finalizeAttempts)
			}
			if r := e.stored("old"); r.Status() != kept || r.Problem() != infos[0].Problem {
				t.Fatalf("stored %s %q", r.Status(), r.Problem())
			}

			again := e.restart()
			infos, err = again.svc.Recover(testCtx(t))
			if err != nil || len(infos) != 1 || infos[0].Status != recording.StatusCompleted {
				t.Fatalf("second Recover = %+v, %v", infos, err)
			}
		})
	}
}

// A process that dies in the middle of the recovery leaves the stored mark; the next one finishes, and a further one
// finds nothing to do.
func TestRecoverIsSafeToRepeat(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("old", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: "a"}, chunk{payload: "b"})
	entered, release := e.rec.HoldFinalize()

	ctx, cancel := context.WithCancel(testCtx(t))
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Recover(ctx)
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted Recover = %v", err)
	}
	release()
	if r := e.stored("old"); r.Status() != recording.StatusRecoveryRequired {
		t.Fatalf("after the interrupted recovery: %s", r.Status())
	}

	again := e.restart()
	infos, err := again.svc.Recover(testCtx(t))
	if err != nil || len(infos) != 1 || infos[0].Status != recording.StatusCompleted || infos[0].Duration != 2*chunkLen {
		t.Fatalf("second Recover = %+v, %v", infos, err)
	}
	if infos, err := again.svc.Recover(testCtx(t)); err != nil || len(infos) != 0 {
		t.Fatalf("third Recover = %+v, %v", infos, err)
	}
	requireHistory(t, e.repo.statuses(), recording.StatusRecoveryRequired, recording.StatusRecoveryRequired,
		recording.StatusCompleted)
}

// One recording that cannot be saved does not stop the recovery of the others.
func TestRecoverGoesOnAfterAFailedSave(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("first", recording.StatusRecording, t0.Add(-2*time.Hour), chunk{payload: "a"})
	e.leftBehind("second", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: "b"})
	e.repo.failNextSaves(shared.ErrVersionConflict)

	infos, err := e.svc.Recover(testCtx(t))
	if !errors.Is(err, shared.ErrVersionConflict) || !strings.Contains(err.Error(), "first") || len(infos) != 2 {
		t.Fatalf("Recover = %+v, %v", infos, err)
	}
	if e.stored("first").Status() != recording.StatusRecording || e.stored("second").Status() != recording.StatusCompleted {
		t.Fatalf("stored: first %s, second %s", e.stored("first").Status(), e.stored("second").Status())
	}
}

func TestRecoverListError(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.repo.listErr = shared.ErrStorageBusy
	if _, err := e.svc.Recover(testCtx(t)); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Recover = %v", err)
	}
	e.repo.listErr = nil
	if _, err := e.svc.Recover(testCtx(t)); err != nil {
		t.Fatalf("Recover after the error = %v", err)
	}
}

// Recover never races a session: it refuses while one runs, and Start waits for it to end.
func TestRecoverAndStartExcludeEachOther(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())

	info := e.start()
	if _, err := e.svc.Recover(testCtx(t)); !errors.Is(err, limits.ErrRecordingInProgress) {
		t.Fatalf("Recover during a session = %v", err)
	}
	e.feed(info.ID, "a")
	if _, err := e.svc.Stop(testCtx(t), info.ID); err != nil {
		t.Fatal(err)
	}

	e.leftBehind("old", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: "a"})
	entered, release := e.rec.HoldFinalize()
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Recover(testCtx(t))
		done <- err
	}()
	<-entered
	if _, err := e.svc.Start(testCtx(t), StartRequest{}); !errors.Is(err, ErrRecovering) {
		t.Fatalf("Start during Recover = %v", err)
	}
	if _, err := e.svc.Recover(testCtx(t)); !errors.Is(err, ErrRecovering) {
		t.Fatalf("a second Recover = %v", err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if next := e.start(); next.Status != recording.StatusRecording {
		t.Fatalf("Start after Recover = %+v", next)
	}
}

func TestCloseDuringRecover(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("old", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: "a"})
	entered, release := e.rec.HoldFinalize()
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Recover(testCtx(t))
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(testCtx(t))
	cancel()
	if err := e.svc.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("forced Close = %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover after the shutdown = %v", err)
	}
	if r := e.stored("old"); r.Status() != recording.StatusRecoveryRequired {
		t.Fatalf("stored %s, want the mark for the next start", r.Status())
	}
	if _, err := e.svc.Recover(testCtx(t)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Recover after Close = %v", err)
	}
}
