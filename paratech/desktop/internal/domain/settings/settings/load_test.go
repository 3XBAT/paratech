package settings

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestRecognitionLoadDefaultsToGentle(t *testing.T) {
	if got := New().RecognitionLoad(); got != shared.LoadGentle {
		t.Errorf("New().RecognitionLoad() = %q, want gentle", got)
	}
}

func TestRestoreRecognitionLoad(t *testing.T) {
	tests := []struct {
		stored shared.RecognitionLoad
		want   shared.RecognitionLoad
	}{
		{shared.LoadGentle, shared.LoadGentle},
		{shared.LoadNormal, shared.LoadNormal},
		{shared.LoadMax, shared.LoadMax},
		{"", shared.LoadGentle},
		{"unlimited", shared.LoadGentle},
	}
	for _, tt := range tests {
		s, err := Restore(State{Version: 1, RecognitionLoad: tt.stored})
		if err != nil {
			t.Fatalf("Restore(%q): %v", tt.stored, err)
		}
		if got := s.RecognitionLoad(); got != tt.want {
			t.Errorf("Restore(%q).RecognitionLoad() = %q, want %q", tt.stored, got, tt.want)
		}
	}
}

func TestSetRecognitionLoad(t *testing.T) {
	s := New()
	if err := s.SetRecognitionLoad(shared.LoadGentle, t1); err != nil || !s.UpdatedAt().IsZero() {
		t.Fatalf("same mode: err %v, updated %v; want a no-op", err, s.UpdatedAt())
	}
	if err := s.SetRecognitionLoad(shared.LoadMax, t1); err != nil {
		t.Fatal(err)
	}
	if s.RecognitionLoad() != shared.LoadMax || !s.UpdatedAt().Equal(t1) {
		t.Errorf("after max: %q at %v", s.RecognitionLoad(), s.UpdatedAt())
	}
	for _, bad := range []shared.RecognitionLoad{"", "MAX", "turbo"} {
		if err := s.SetRecognitionLoad(bad, t1); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("SetRecognitionLoad(%q) = %v, want ErrInvalidSettings", bad, err)
		}
	}
	if s.RecognitionLoad() != shared.LoadMax {
		t.Errorf("a refused mode changed the setting to %q", s.RecognitionLoad())
	}
	if err := s.SetRecognitionLoad(shared.LoadNormal, t0); err != nil || s.RecognitionLoad() != shared.LoadNormal {
		t.Errorf("normal: err %v, mode %q", err, s.RecognitionLoad())
	}
}
