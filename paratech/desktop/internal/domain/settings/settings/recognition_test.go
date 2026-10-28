package settings

import (
	"errors"
	"testing"
	"time"
)

// TASK-294: photo recognition is off on a fresh installation and on an older row; only known modes are taken.
func TestNoteRecognitionDefaultIsOff(t *testing.T) {
	if DefaultNoteRecognition != NoteRecognitionOff {
		t.Fatalf("default = %q, want off (ADR-0027, Open 14)", DefaultNoteRecognition)
	}
	if got := New().NoteRecognition(); got != NoteRecognitionOff {
		t.Fatalf("fresh installation: %q, want off", got)
	}
}

func TestNoteRecognitionRestore(t *testing.T) {
	tests := []struct {
		stored NoteRecognition
		want   NoteRecognition
	}{
		{stored: "", want: NoteRecognitionOff}, // a state from before the setting
		{stored: NoteRecognitionOff, want: NoteRecognitionOff},
		{stored: NoteRecognitionLocal, want: NoteRecognitionLocal},
		{stored: "cloud", want: NoteRecognitionOff}, // unknown here: never more than the student turned on
		{stored: "LOCAL", want: NoteRecognitionOff},
	}
	for _, tt := range tests {
		s, err := Restore(State{Version: 1, NoteRecognition: tt.stored})
		if err != nil || s.NoteRecognition() != tt.want {
			t.Errorf("Restore(%q) = %v, %v; want %q", tt.stored, s, err, tt.want)
		}
	}
}

func TestSetNoteRecognition(t *testing.T) {
	tests := []struct {
		name    string
		mode    NoteRecognition
		now     time.Time
		want    NoteRecognition
		updated time.Time
		err     bool
	}{
		{name: "same value is a no-op", mode: NoteRecognitionOff, now: t1, want: NoteRecognitionOff},
		{name: "local", mode: NoteRecognitionLocal, now: t1, want: NoteRecognitionLocal, updated: t1},
		{name: "unknown", mode: "cloud", now: t1, want: NoteRecognitionOff, err: true},
		{name: "empty", mode: "", now: t1, want: NoteRecognitionOff, err: true},
		{name: "no change time", mode: NoteRecognitionLocal, want: NoteRecognitionOff, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			err := s.SetNoteRecognition(tt.mode, tt.now)
			if tt.err != errors.Is(err, ErrInvalidSettings) || (!tt.err && err != nil) {
				t.Fatalf("err = %v, want error %v", err, tt.err)
			}
			if s.NoteRecognition() != tt.want || !s.UpdatedAt().Equal(tt.updated) {
				t.Fatalf("mode %q updated %v; want %q at %v", s.NoteRecognition(), s.UpdatedAt(), tt.want, tt.updated)
			}
		})
	}

	s := New()
	if err := s.SetNoteRecognition(NoteRecognitionLocal, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNoteRecognition(NoteRecognitionOff, t1); err != nil || s.NoteRecognition() != NoteRecognitionOff {
		t.Fatalf("off again: %q, %v", s.NoteRecognition(), err)
	}
}
