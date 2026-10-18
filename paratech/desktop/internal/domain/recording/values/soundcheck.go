package values

import (
	"errors"
	"fmt"
	"math"
)

// The "Check sound" level check (TASK-075, replacing the tone self-test of TASK-058): the recorder captures the
// user's source for a while without recording, and a level meter shows what it hears.

// ErrInvalidSoundLevel means a level is not a finite number in 0..1.
var ErrInvalidSoundLevel = errors.New("invalid sound level")

// SoundLevel is the loudness the capture heard in a short window: the peak of the mono samples, linear, 0 (silence)
// to 1 (full scale).
type SoundLevel float64

// HeardLevel is the least peak that counts as sound: -40 dBFS. Loopback captures the digital mix, so silence is
// exact zeros and the noise of an idle app stays far below; speech or music at any usual volume of the app peaks well
// above it. A low Windows master volume should not lower it (ADR-0002, not confirmed for every device).
const HeardLevel SoundLevel = 0.01

// NewSoundLevel checks v strictly: a finite number in 0..1.
func NewSoundLevel(v float64) (SoundLevel, error) {
	if math.IsNaN(v) || v < 0 || v > 1 {
		return 0, fmt.Errorf("%w: %v", ErrInvalidSoundLevel, v)
	}
	return SoundLevel(v), nil
}

// ClampSoundLevel bounds a measured peak to 0..1; NaN is silence.
func ClampSoundLevel(v float64) SoundLevel {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	return SoundLevel(min(v, 1))
}

// Heard reports whether l counts as sound.
func (l SoundLevel) Heard() bool { return l >= HeardLevel }

// SoundCause is the verdict of a sound check, or how its capture ended.
type SoundCause string

// Causes of a sound check.
const (
	// SoundOK: sound was heard; as an end, the capture ran until its time was up or it was stopped.
	SoundOK SoundCause = "ok"

	// SoundSilence: the capture ran but heard nothing.
	SoundSilence SoundCause = "silence"

	// SoundNotRunning: the chosen app does not run.
	SoundNotRunning SoundCause = "not_running"

	// SoundNoDevice: there is no active output device to capture.
	SoundNoDevice SoundCause = "no_device"

	// SoundUnsupported: this build or this Windows cannot capture.
	SoundUnsupported SoundCause = "unsupported"

	// SoundBusy: a recording runs or started; the check was refused or stopped, so it never disturbs the lecture.
	SoundBusy SoundCause = "busy"

	// SoundFailed: anything else.
	SoundFailed SoundCause = "failed"
)

// Valid reports whether c is one of the causes.
func (c SoundCause) Valid() bool {
	switch c {
	case SoundOK, SoundSilence, SoundNotRunning, SoundNoDevice, SoundUnsupported, SoundBusy, SoundFailed:
		return true
	}
	return false
}

// LevelCheck is what a sound check heard so far: the latest level and the loudest one. The zero value heard
// nothing. It holds no audio.
type LevelCheck struct {
	Level SoundLevel // the latest window
	Peak  SoundLevel // the loudest window
}

// Hear adds the level of the next window.
func (c LevelCheck) Hear(l SoundLevel) LevelCheck {
	c.Level = l
	c.Peak = max(c.Peak, l)
	return c
}

// Heard reports whether any window counted as sound.
func (c LevelCheck) Heard() bool { return c.Peak.Heard() }

// Verdict is the answer after the capture ended with end. A recording that took over wins; otherwise sound heard at
// any time is ok, whatever ended the capture afterwards; a capture that ran without hearing anything is silence, and
// one that could not run gives its cause.
func (c LevelCheck) Verdict(end SoundCause) SoundCause {
	switch {
	case end == SoundBusy:
		return SoundBusy
	case c.Heard():
		return SoundOK
	case end == SoundOK || end == SoundSilence:
		return SoundSilence
	case end.Valid():
		return end
	}
	return SoundFailed
}
