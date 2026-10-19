package recordingsession

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/testenv"
)

// zeros is the payload of a chunk of silence for the fake recorder.
const zeros = "\x00\x00\x00\x00"

// TASK-082: a recording of only silence is COMPLETED, marked silent and not handed over for transcription; an
// audible one is.
func TestSilentRecordingIsNotTranscribed(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		silent bool
	}{
		{"zeros only", []string{zeros, zeros}, true},
		{"sound", []string{zeros, "voice"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := newEnv(t, DefaultConfig())
			info := e.start()
			e.feed(info.ID, tt.chunks...)
			got, err := e.svc.Stop(testCtx(t), info.ID)
			if err != nil {
				t.Fatal(err)
			}
			r := e.stored(info.ID)
			if r.Status() != recording.StatusCompleted || r.Silent() != tt.silent || got.Silent != tt.silent {
				t.Fatalf("status %s, silent %v (info %v)", r.Status(), r.Silent(), got.Silent)
			}
			if d, ok := r.Duration(); !ok || d != 2*chunkLen {
				t.Fatalf("duration %v", d)
			}
			if n := len(e.done.all()); (n == 0) != tt.silent {
				t.Fatalf("%d completions for a silent %v recording", n, tt.silent)
			}
		})
	}
}

// The recovery after a restart marks a silent recording too.
func TestRecoverSilentRecording(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("old", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: zeros}, chunk{payload: zeros})

	infos, err := e.svc.Recover(testCtx(t))
	if err != nil || len(infos) != 1 || infos[0].Status != recording.StatusCompleted || !infos[0].Silent {
		t.Fatalf("Recover = %+v, %v", infos, err)
	}
	if r := e.stored("old"); !r.Silent() {
		t.Fatal("the recovered recording is not marked silent")
	}
	if n := len(e.done.all()); n != 0 {
		t.Fatalf("%d completions for a silent recording", n)
	}
}
