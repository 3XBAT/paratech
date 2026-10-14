package scheduler

import (
	"log/slog"
	"time"
)

// Defaults of Config.
const (
	DefaultLead         = 5 * time.Minute
	DefaultPollInterval = 15 * time.Second
)

// Deps are the ports the Service needs.
type Deps struct {
	Lessons Lessons
	Clock   Clock
	Sleeper Sleeper
	Events  Events
	Log     *slog.Logger // optional: nil discards
}

// Config tunes the scheduler.
type Config struct {
	Lead         time.Duration // how long before the start the lesson is announced, 0..lesson.MaxStartLead
	PollInterval time.Duration // wait between two reads of the schedule, below lesson.StartLateGrace; an announcement may be this late
}

// DefaultConfig returns the scheduler settings of the desktop.
func DefaultConfig() Config {
	return Config{Lead: DefaultLead, PollInterval: DefaultPollInterval}
}
