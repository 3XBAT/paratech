package recording

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func TestMarkDeleting(t *testing.T) {
	at := t0.Add(5 * time.Hour)
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			r := inStatus(t, s)
			err := r.MarkDeleting(at, []shared.AssignmentID{"a1", "a2"})
			if s.InProgress() {
				if !errors.Is(err, ErrNotDeletable) || r.Deleting() {
					t.Fatalf("MarkDeleting of %s = %v, deleting %v; want ErrNotDeletable and no mark", s, err, r.Deleting())
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkDeleting of %s: %v", s, err)
			}
			if !r.Deleting() || !r.DeletingAt().Equal(at) || !r.UpdatedAt().Equal(at) {
				t.Fatalf("mark = %v at %v (updated %v), want at %v", r.Deleting(), r.DeletingAt(), r.UpdatedAt(), at)
			}
			if got := r.DeletingAssignments(); !reflect.DeepEqual(got, []shared.AssignmentID{"a1", "a2"}) {
				t.Fatalf("assignments = %v", got)
			}
		})
	}
}

func TestMarkDeletingRefuses(t *testing.T) {
	many := make([]shared.AssignmentID, MaxDeletedAssignments+1)
	for i := range many {
		many[i] = shared.AssignmentID("a" + strconv.Itoa(i))
	}
	cases := []struct {
		name        string
		at          time.Time
		assignments []shared.AssignmentID
		want        error
	}{
		{"no time", time.Time{}, nil, ErrInvalidRecording},
		{"bad id", t0, []shared.AssignmentID{"a/b"}, ErrInvalidRecording},
		{"twice the same", t0, []shared.AssignmentID{"a1", "a1"}, ErrInvalidRecording},
		{"too many", t0, many, ErrInvalidRecording},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := inStatus(t, StatusCompleted)
			if err := r.MarkDeleting(tc.at, tc.assignments); !errors.Is(err, tc.want) {
				t.Fatalf("MarkDeleting = %v, want %v", err, tc.want)
			}
			if r.Deleting() {
				t.Fatal("refused mark was applied")
			}
		})
	}

	t.Run("already marked", func(t *testing.T) {
		r := inStatus(t, StatusCompleted)
		if err := r.MarkDeleting(t0, []shared.AssignmentID{"a1"}); err != nil {
			t.Fatal(err)
		}
		if err := r.MarkDeleting(t0.Add(time.Hour), nil); !errors.Is(err, ErrDeleting) {
			t.Fatalf("second MarkDeleting = %v, want ErrDeleting", err)
		}
		if got := r.DeletingAssignments(); len(got) != 1 || !r.DeletingAt().Equal(t0) {
			t.Fatalf("first mark changed: %v at %v", got, r.DeletingAt())
		}
	})
}

func TestMarkedRecordingIsNeitherTranscribedNorAssembled(t *testing.T) {
	done := inStatus(t, StatusCompleted)
	if _, ok := done.TranscribableAudio(); !ok {
		t.Fatal("a completed recording must be transcribable before the mark")
	}
	if err := done.MarkDeleting(t0, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := done.TranscribableAudio(); ok {
		t.Fatal("a recording marked for deletion is transcribable")
	}

	lost := inStatus(t, StatusRecoveryRequired)
	if err := lost.MarkDeleting(t0, nil); err != nil {
		t.Fatal(err)
	}
	if lost.AwaitsAssembly() {
		t.Fatal("a recording marked for deletion awaits assembly")
	}
	if err := lost.CompleteRecovery(t0, mustRef(t, "recordings/r1.wav"), time.Minute); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("CompleteRecovery of a marked recording = %v, want ErrInvalidTransition", err)
	}
}

func TestMarkSurvivesRestore(t *testing.T) {
	r := inStatus(t, StatusFailed)
	if err := r.MarkDeleting(t0, []shared.AssignmentID{"a1"}); err != nil {
		t.Fatal(err)
	}
	r.Persisted(2)
	got, err := Restore(stateOf(r))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
	}

	bad := []struct {
		name   string
		mutate func(s *State)
	}{
		{"mark on a recording in progress", func(s *State) {
			s.Status, s.StoppedAt, s.Problem = StatusStarting, time.Time{}, ""
		}},
		{"assignments without a mark", func(s *State) { s.DeletingAt = time.Time{} }},
		{"bad assignment", func(s *State) { s.DeletingAssignments = []shared.AssignmentID{"a b"} }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			s := stateOf(r)
			tc.mutate(&s)
			if _, err := Restore(s); !errors.Is(err, ErrInvalidRecording) {
				t.Fatalf("Restore = %v, want ErrInvalidRecording", err)
			}
		})
	}
}

func TestHasSound(t *testing.T) {
	cases := map[Status]bool{StatusStarting: false, StatusCompleted: true, StatusFailed: false}
	for s, want := range cases {
		if got := inStatus(t, s).HasSound(); got != want {
			t.Errorf("HasSound of %s = %v, want %v", s, got, want)
		}
	}
	r := inStatus(t, StatusRecording)
	if err := r.ObserveChunks(1, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !r.HasSound() {
		t.Error("a recording with a chunk has no sound")
	}
}
