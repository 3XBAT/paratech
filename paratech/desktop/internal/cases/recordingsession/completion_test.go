package recordingsession

import (
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// Every path that saves COMPLETED tells the listener once, after the save; a recording that ends FAILED does not.
func TestCompletionIsAnnounced(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		end    func(e *env, id shared.RecordingID) // ends the session and waits until it is over
		want   recording.Status
	}{
		{
			name: "stop", chunks: []string{"a"}, want: recording.StatusCompleted,
			end: func(e *env, id shared.RecordingID) { _, _ = e.svc.Stop(testCtx(e.t), id) },
		},
		{
			name: "hard cap", chunks: []string{"a"}, want: recording.StatusCompleted,
			end: func(e *env, id shared.RecordingID) {
				if err := e.clock.AwaitSleepers(testCtx(e.t), 1); err != nil {
					e.t.Fatal(err)
				}
				e.clock.Advance(e.svc.policy.MaxDuration())
				e.awaitStatus(id, recording.StatusCompleted)
			},
		},
		{
			name: "capture ended by itself", chunks: []string{"a"}, want: recording.StatusCompleted,
			end: func(e *env, id shared.RecordingID) {
				e.rec.End(id, errDevice)
				e.awaitStatus(id, recording.StatusCompleted)
			},
		},
		{
			name: "no audio", want: recording.StatusFailed,
			end: func(e *env, id shared.RecordingID) { _, _ = e.svc.Stop(testCtx(e.t), id) },
		},
		{
			name: "assembly fails", chunks: []string{"a"}, want: recording.StatusFailed,
			end: func(e *env, id shared.RecordingID) {
				e.rec.FailFinalize(shared.ErrStoragePermission)
				_, _ = e.stopAdvancing(id)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newEnv(t, DefaultConfig())
			info := e.start()
			e.feed(info.ID, tc.chunks...)

			tc.end(e, info.ID)
			if got := e.stored(info.ID).Status(); got != tc.want {
				t.Fatalf("stored %s, want %s", got, tc.want)
			}
			var want []completion
			if tc.want == recording.StatusCompleted {
				want = []completion{{id: info.ID, stored: recording.StatusCompleted}}
			}
			if got := e.done.all(); !slices.Equal(got, want) {
				t.Fatalf("listener calls %+v, want %+v", got, want)
			}
		})
	}
}

// The startup recovery tells the listener about a recording it completes, not about one it fails or leaves alone.
func TestRecoveredCompletionIsAnnounced(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	created := t0.Add(-time.Hour)
	e.leftBehind("rec-cut", recording.StatusRecording, created, chunk{payload: "a"}, chunk{payload: "b"})
	e.leftBehind("rec-empty", recording.StatusRecording, created.Add(time.Second))
	e.leftBehind("rec-done", recording.StatusCompleted, created.Add(2*time.Second))

	if _, err := e.recoverAdvancing(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got := e.stored("rec-empty").Status(); got != recording.StatusFailed {
		t.Fatalf("recording without chunks is %s, want FAILED", got)
	}
	want := []completion{{id: "rec-cut", stored: recording.StatusCompleted}}
	if got := e.done.all(); !slices.Equal(got, want) {
		t.Fatalf("listener calls %+v, want %+v", got, want)
	}
}

// A failing listener changes nothing about the recording: it stays COMPLETED and Stop succeeds.
func TestListenerErrorKeepsTheRecording(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.done.failWith(errors.New("database busy"))
	info := e.start()
	e.feed(info.ID, "a")

	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted {
		t.Fatalf("Stop = %+v, %v; want COMPLETED", got, err)
	}
	if s := e.stored(info.ID).Status(); s != recording.StatusCompleted || len(e.done.all()) != 1 {
		t.Fatalf("stored %s after %d listener calls, want COMPLETED after one", s, len(e.done.all()))
	}
}
