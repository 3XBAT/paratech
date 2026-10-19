package recording

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

var (
	silentTrack  = values.AssembledTrack{Duration: 10 * time.Minute, Seconds: 600, AudibleSeconds: 1}
	audibleTrack = values.AssembledTrack{Duration: 10 * time.Minute, Seconds: 600, AudibleSeconds: 300}
)

// TASK-082: a silent track is stored, kept through Restore and never transcribed.
func TestCompleteTrackSilent(t *testing.T) {
	tests := []struct {
		name   string
		track  values.AssembledTrack
		silent bool
	}{
		{"silent", silentTrack, true},
		{"audible", audibleTrack, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := inStatus(t, StatusFinalizing)
			track := mustRef(t, "recordings/r1.wav")
			if err := r.CompleteTrack(t0.Add(time.Hour), track, tt.track); err != nil {
				t.Fatal(err)
			}
			if r.Silent() != tt.silent {
				t.Fatalf("Silent = %v", r.Silent())
			}
			if d, ok := r.Duration(); !ok || d != tt.track.Duration {
				t.Fatalf("duration %v, %v", d, ok)
			}
			if _, ok := r.TranscribableAudio(); ok == tt.silent {
				t.Fatalf("transcribable %v for a silent %v track", ok, tt.silent)
			}

			r.Persisted(2)
			got, err := Restore(stateOf(r))
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
}

func TestCompleteTrackRefusesLikeComplete(t *testing.T) {
	r := inStatus(t, StatusRecording)
	if err := r.CompleteTrack(t0.Add(time.Hour), mustRef(t, "recordings/r1.wav"), silentTrack); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
	if r.Silent() {
		t.Fatal("a refused completion marked the recording silent")
	}
}

func TestCompleteRecoveryTrackSilent(t *testing.T) {
	r := inStatus(t, StatusRecoveryRequired)
	if err := r.CompleteRecoveryTrack(t0.Add(2*time.Hour), mustRef(t, "recordings/r1.wav"), silentTrack); err != nil {
		t.Fatal(err)
	}
	if r.Status() != StatusCompleted || !r.Silent() || r.Problem() != "stale heartbeat" {
		t.Fatalf("status %s, silent %v, problem %q", r.Status(), r.Silent(), r.Problem())
	}

	r = inStatus(t, StatusCompleted)
	if err := r.CompleteRecoveryTrack(t0.Add(2*time.Hour), mustRef(t, "recordings/r1.wav"), silentTrack); !errors.Is(err, ErrInvalidTransition) || r.Silent() {
		t.Fatalf("error = %v, silent %v", err, r.Silent())
	}
}

func TestRestoreRejectsSilentUnfinished(t *testing.T) {
	r := inStatus(t, StatusFinalizing)
	r.Persisted(1)
	st := stateOf(r)
	st.Silent = true
	if _, err := Restore(st); !errors.Is(err, ErrInvalidRecording) {
		t.Fatalf("error = %v, want ErrInvalidRecording", err)
	}
}

func TestCanSwitchSource(t *testing.T) {
	for _, s := range Statuses() {
		if got, want := s.CanSwitchSource(), s == StatusRecording; got != want {
			t.Errorf("%s: CanSwitchSource = %v, want %v", s, got, want)
		}
	}
}
