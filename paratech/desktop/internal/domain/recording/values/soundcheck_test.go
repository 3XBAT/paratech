package values

import (
	"errors"
	"math"
	"testing"
)

func TestSoundCauseValid(t *testing.T) {
	for _, c := range []SoundCause{SoundOK, SoundSilence, SoundNotRunning, SoundNoDevice, SoundUnsupported, SoundBusy,
		SoundFailed} {
		if !c.Valid() {
			t.Errorf("%s is a cause", c)
		}
	}
	for _, c := range []SoundCause{"", "loud", "OK", "hands_free"} {
		if c.Valid() {
			t.Errorf("%q is not a cause", c)
		}
	}
}

func TestNewSoundLevel(t *testing.T) {
	for _, v := range []float64{0, 0.005, 0.01, 0.5, 1} {
		if l, err := NewSoundLevel(v); err != nil || float64(l) != v {
			t.Errorf("NewSoundLevel(%v) = %v, %v", v, l, err)
		}
	}
	for _, v := range []float64{-0.001, 1.0001, 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NewSoundLevel(v); !errors.Is(err, ErrInvalidSoundLevel) {
			t.Errorf("NewSoundLevel(%v) = %v, want ErrInvalidSoundLevel", v, err)
		}
	}
}

func TestClampSoundLevel(t *testing.T) {
	tests := []struct {
		in   float64
		want SoundLevel
	}{
		{0, 0}, {0.3, 0.3}, {1, 1}, {1.7, 1}, {-0.2, 0}, {math.NaN(), 0}, {math.Inf(1), 1}, {math.Inf(-1), 0},
	}
	for _, tt := range tests {
		if got := ClampSoundLevel(tt.in); got != tt.want {
			t.Errorf("ClampSoundLevel(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSoundLevelHeard(t *testing.T) {
	tests := []struct {
		level SoundLevel
		want  bool
	}{
		{0, false}, {0.0099, false}, {HeardLevel, true}, {0.2, true}, {1, true},
	}
	for _, tt := range tests {
		if got := tt.level.Heard(); got != tt.want {
			t.Errorf("%v.Heard() = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestLevelCheckHear(t *testing.T) {
	var c LevelCheck
	if c.Heard() {
		t.Fatal("the zero check heard something")
	}
	c = c.Hear(0.002).Hear(0.4).Hear(0.05)
	if c.Level != 0.05 || c.Peak != 0.4 || !c.Heard() {
		t.Fatalf("check %+v", c)
	}
	if quiet := (LevelCheck{}).Hear(0.009).Hear(0); quiet.Heard() || quiet.Peak != 0.009 {
		t.Fatalf("quiet check %+v", quiet)
	}
}

func TestLevelCheckVerdict(t *testing.T) {
	heard := LevelCheck{}.Hear(0.3)
	quiet := LevelCheck{}.Hear(0.001)
	tests := []struct {
		name  string
		check LevelCheck
		end   SoundCause
		want  SoundCause
	}{
		{"heard, time up", heard, SoundOK, SoundOK},
		{"quiet, time up", quiet, SoundOK, SoundSilence},
		{"nothing at all", LevelCheck{}, SoundOK, SoundSilence},
		{"quiet, silence end", quiet, SoundSilence, SoundSilence},
		{"heard, then failed", heard, SoundFailed, SoundOK},
		{"heard, then a recording", heard, SoundBusy, SoundBusy},
		{"quiet, a recording", quiet, SoundBusy, SoundBusy},
		{"app not running", LevelCheck{}, SoundNotRunning, SoundNotRunning},
		{"no device", LevelCheck{}, SoundNoDevice, SoundNoDevice},
		{"unsupported", LevelCheck{}, SoundUnsupported, SoundUnsupported},
		{"failed", quiet, SoundFailed, SoundFailed},
		{"an unknown end", LevelCheck{}, "loud", SoundFailed},
		{"an empty end", LevelCheck{}, "", SoundFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.check.Verdict(tt.end); got != tt.want {
				t.Fatalf("Verdict(%s) = %s, want %s", tt.end, got, tt.want)
			}
		})
	}
}
