package recording

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func stateOf(r *Recording) State {
	d, _ := r.Duration()
	return State{
		Version: r.Version(), ID: r.ID(), LessonID: r.LessonID(), Lesson: r.LessonSnapshot(), Correction: r.LessonCorrection(), Status: r.Status(),
		MaxDuration: r.MaxDuration(), Duration: d, Chunks: r.Chunks(), ChunksDir: r.ChunksDir(), Audio: r.Audio(), Silent: r.Silent(), Problem: r.Problem(),
		StartedAt: r.StartedAt(), StoppedAt: r.StoppedAt(), HeartbeatAt: r.HeartbeatAt(),
		CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		DeletingAt: r.DeletingAt(), DeletingAssignments: r.DeletingAssignments(),
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			r := inStatus(t, s)
			r.Persisted(3)
			got, err := Restore(stateOf(r))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, r) {
				t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
			}
		})
	}
}

func TestRestoreRejectsInconsistentState(t *testing.T) {
	completed := inStatus(t, StatusCompleted)
	completed.Persisted(1)
	recordingState := inStatus(t, StatusRecording)
	recordingState.Persisted(1)

	cases := []struct {
		name   string
		base   *Recording
		mutate func(s *State)
	}{
		{"version 0", completed, func(s *State) { s.Version = 0 }},
		{"unknown status", completed, func(s *State) { s.Status = "PAUSED" }},
		{"bad id", completed, func(s *State) { s.ID = "a/b" }},
		{"bad lesson", completed, func(s *State) { s.LessonID = "a b" }},
		{"zero cap", completed, func(s *State) { s.MaxDuration = 0 }},
		{"negative chunks", completed, func(s *State) { s.Chunks = -1 }},
		{"negative duration", completed, func(s *State) { s.Duration = -time.Second }},
		{"completed without audio", completed, func(s *State) { s.Audio, s.Duration = shared.ArtifactRef{}, 0 }},
		{"audio outside recordings", completed, func(s *State) { s.Audio = mustRef(t, "exports/r1.wav") }},
		{"chunks outside recordings", completed, func(s *State) { s.ChunksDir = mustRef(t, "models/r1") }},
		{"missing created time", completed, func(s *State) { s.CreatedAt = time.Time{} }},
		{"missing start time", completed, func(s *State) { s.StartedAt = time.Time{} }},
		{"long problem", completed, func(s *State) { s.Problem = string(make([]byte, MaxProblemBytes+1)) }},
		{"recording with audio", recordingState, func(s *State) { s.Audio = mustRef(t, "recordings/r1.wav") }},
		{"duration without audio", recordingState, func(s *State) { s.Duration = time.Minute }},
		{"recording with stop time", recordingState, func(s *State) { s.StoppedAt = t0 }},
		{"stopping without stop time", recordingState, func(s *State) { s.Status = StatusStopping }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stateOf(tc.base)
			tc.mutate(&s)
			if _, err := Restore(s); !errors.Is(err, ErrInvalidRecording) {
				t.Fatalf("Restore error = %v, want ErrInvalidRecording", err)
			}
		})
	}
}
