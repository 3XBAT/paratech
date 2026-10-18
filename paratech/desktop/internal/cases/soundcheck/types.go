package soundcheck

import (
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

// State is the state of the last check.
type State string

// States of a check.
const (
	StateIdle    State = "idle"    // no check since the start
	StateRunning State = "running" // the capture runs
	StateDone    State = "done"    // ended: Verdict is set
)

// Status is the last check as the page shows it.
type Status struct {
	State  State
	Source values.Source // what is checked

	// Check is what the capture heard: the latest level (0 once done) and the loudest one.
	Check values.LevelCheck
	// Heard reports a level at or above values.HeardLevel at any time.
	Heard bool

	Elapsed  time.Duration // since the start, at most Duration
	Duration time.Duration // of a whole check

	Verdict values.SoundCause // once done
}
