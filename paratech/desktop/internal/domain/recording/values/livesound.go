package values

import "time"

// The live sound of a running recording (TASK-099): a small level capsule in «Идёт запись» and in the popup shows
// that sound comes in. The recorder reports the level of short windows; nothing of it is stored.

const (
	// QuietHintAfter is how long a running recording may hear nothing before a calm hint shows (owner, 2026-10-20).
	QuietHintAfter = 2 * time.Minute

	// SoundHold is how long a heard window keeps the capsule «слышно»: a pause between words is no silence.
	SoundHold = time.Second

	// LevelStale is the age at which the latest level is no longer current (the recorder child restarts or hangs): it
	// then reads as silence.
	LevelStale = time.Second
)

// LiveSound is what the capture of a running recording heard: the latest level and when sound was last heard. It is
// a value: Hear returns the next state. The zero value heard nothing yet.
type LiveSound struct {
	level   SoundLevel
	at      time.Time // when level was measured; zero before the first level
	since   time.Time // the first level: the silence counts from here
	heardAt time.Time // the latest level that counted as sound; zero for none
}

// Hear adds the level of the window measured at at. A clock that went back starts counting again.
func (s LiveSound) Hear(l SoundLevel, at time.Time) LiveSound {
	if s.since.IsZero() || at.Before(s.at) {
		s = LiveSound{since: at}
	}
	s.level, s.at = l, at
	if l.Heard() {
		s.heardAt = at
	}
	return s
}

// SoundReading is what the capsule shows at a moment.
type SoundReading struct {
	Level SoundLevel    // the current level; 0 when it is stale
	Heard bool          // sound was heard within SoundHold
	Quiet time.Duration // how long nothing was heard; 0 while heard
	Hint  bool          // nothing was heard for QuietHintAfter: show the calm hint
}

// Reading is what the capsule shows at now.
func (s LiveSound) Reading(now time.Time) SoundReading {
	if s.since.IsZero() {
		return SoundReading{}
	}
	var r SoundReading
	if age := now.Sub(s.at); age >= 0 && age < LevelStale {
		r.Level = s.level
	}
	if !s.heardAt.IsZero() && now.Sub(s.heardAt) < SoundHold {
		r.Heard = true
		return r
	}
	from := s.since
	if !s.heardAt.IsZero() {
		from = s.heardAt
	}
	r.Quiet = max(now.Sub(from), 0)
	r.Hint = r.Quiet >= QuietHintAfter
	return r
}
