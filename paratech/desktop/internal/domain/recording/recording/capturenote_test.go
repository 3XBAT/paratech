package recording

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

var muteSwitch = values.CaptureSwitch{From: values.CaptureInclude, To: values.CaptureSystem,
	Cause: values.SwitchHealthyButMute, Reason: "only zeros for 15s while the app played"}

// ARCH-62: the note is allowed only while RECORDING; every other status refuses it unchanged.
func TestNoteCaptureFallbackOnlyWhileRecording(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			r := inStatus(t, s)
			before, updated := r.Problem(), r.UpdatedAt()
			err := r.NoteCaptureFallback(t0.Add(time.Hour), muteSwitch)
			if s == StatusRecording {
				if err != nil || r.Problem() != muteSwitch.Note() || !r.UpdatedAt().Equal(t0.Add(time.Hour)) {
					t.Fatalf("note = %v, problem %q", err, r.Problem())
				}
				return
			}
			if !errors.Is(err, ErrInvalidTransition) || r.Problem() != before || !r.UpdatedAt().Equal(updated) ||
				r.Status() != s {
				t.Fatalf("note on %s = %v, problem %q", s, err, r.Problem())
			}
		})
	}
}

func TestNoteCaptureFallbackRefusesOtherSwitches(t *testing.T) {
	for _, sw := range []values.CaptureSwitch{
		{From: values.CaptureSystem, To: values.CaptureSystem},
		{From: values.CaptureInclude, To: values.CaptureExclude},
		{},
	} {
		r := inStatus(t, StatusRecording)
		if err := r.NoteCaptureFallback(t0.Add(time.Hour), sw); !errors.Is(err, ErrInvalidRecording) || r.Problem() != "" {
			t.Errorf("switch %+v: %v, problem %q", sw, err, r.Problem())
		}
	}
}

func TestNoteCaptureFallbackIsIdempotent(t *testing.T) {
	r := inStatus(t, StatusRecording)
	at := t0.Add(time.Hour)
	if err := r.NoteCaptureFallback(at, muteSwitch); err != nil {
		t.Fatal(err)
	}
	if err := r.NoteCaptureFallback(at.Add(time.Minute), muteSwitch); err != nil || !r.UpdatedAt().Equal(at) {
		t.Fatalf("second note = %v, updated %v", err, r.UpdatedAt())
	}
	if strings.Count(r.Problem(), "тишину") != 1 {
		t.Fatalf("problem %q", r.Problem())
	}
}

// What happens to the note on the way to a final status: kept by a stop and the cap, after the reason of an
// interruption or a lost session, replaced by a failure.
func TestCaptureNoteThroughTheLifecycle(t *testing.T) {
	track := mustRef(t, "recordings/r1.wav")
	at := t0.Add(time.Hour)
	note := muteSwitch.Note()
	tests := []struct {
		name  string
		steps func(r *Recording) error
		want  Status
		check func(t *testing.T, problem string)
	}{
		{"stop and complete keep it", func(r *Recording) error {
			return errors.Join(r.Stop(at), r.Finalize(at), r.Complete(at, track, time.Minute))
		}, StatusCompleted, func(t *testing.T, p string) {
			if p != note {
				t.Fatalf("problem %q", p)
			}
		}},
		{"the hard cap keeps it", func(r *Recording) error {
			return errors.Join(r.ReachLimit(t0.Add(time.Second+testCap)), r.Complete(at, track, time.Minute))
		}, StatusCompleted, func(t *testing.T, p string) {
			if p != note {
				t.Fatalf("problem %q", p)
			}
		}},
		{"an interruption puts its reason first", func(r *Recording) error {
			return errors.Join(r.Interrupt(at, "device lost"), r.Complete(at, track, time.Minute))
		}, StatusCompleted, func(t *testing.T, p string) {
			if p != "device lost; "+note {
				t.Fatalf("problem %q", p)
			}
		}},
		{"a lost session keeps it after the reason", func(r *Recording) error {
			return errors.Join(r.MarkInterrupted(at, "app ended"), r.CompleteRecovery(at, track, time.Minute))
		}, StatusCompleted, func(t *testing.T, p string) {
			if p != "app ended; "+note {
				t.Fatalf("problem %q", p)
			}
		}},
		{"a deferred recovery keeps reason and note", func(r *Recording) error {
			return errors.Join(r.RequireRecovery(at, "stale heartbeat"), r.DeferRecovery(at, "disk busy"),
				r.CompleteRecovery(at, track, time.Minute))
		}, StatusCompleted, func(t *testing.T, p string) {
			if p != "stale heartbeat; "+note {
				t.Fatalf("problem %q", p)
			}
		}},
		{"a failure replaces it", func(r *Recording) error {
			return errors.Join(r.Stop(at), r.Finalize(at), r.Fail(at, "no readable audio"))
		}, StatusFailed, func(t *testing.T, p string) {
			if p != "no readable audio" {
				t.Fatalf("problem %q", p)
			}
		}},
		{"a long reason wins over the note", func(r *Recording) error {
			return r.Interrupt(at, strings.Repeat("x", MaxProblemBytes-10))
		}, StatusFinalizing, func(t *testing.T, p string) {
			if len(p) > MaxProblemBytes || !strings.HasPrefix(p, strings.Repeat("x", MaxProblemBytes-10)) {
				t.Fatalf("problem %d bytes", len(p))
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := inStatus(t, StatusRecording)
			if err := r.NoteCaptureFallback(t0.Add(2*time.Second), muteSwitch); err != nil {
				t.Fatal(err)
			}
			if err := tt.steps(r); err != nil {
				t.Fatal(err)
			}
			if r.Status() != tt.want {
				t.Fatalf("status %s, want %s", r.Status(), tt.want)
			}
			tt.check(t, r.Problem())
		})
	}
}

// Without a note the reasons stay as they were.
func TestInterruptWithoutANote(t *testing.T) {
	r := inStatus(t, StatusRecording)
	if err := r.Interrupt(t0.Add(time.Hour), "device lost"); err != nil || r.Problem() != "device lost" {
		t.Fatalf("Interrupt = %v, problem %q", err, r.Problem())
	}
}
