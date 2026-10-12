package jobrunner

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
)

// Deps are the ports the Service needs.
type Deps struct {
	Jobs    JobRepository
	Clock   Clock
	Sleeper Sleeper
	Jitter  Jitter       // optional: nil means no jitter
	Log     *slog.Logger // optional: nil discards
}

// Config tunes the worker pool.
type Config struct {
	Workers        int           // goroutines that run jobs, at least 1
	PollInterval   time.Duration // wait between polls of an idle worker
	DefaultTimeout time.Duration // limit of one attempt for a type without its own
	Backoff        backoff.Spec  // retry delays and the cap on attempts
}

// DefaultConfig returns the settings for a desktop with one background class recording.
func DefaultConfig() Config {
	return Config{
		Workers:        2,
		PollInterval:   time.Second,
		DefaultTimeout: 30 * time.Minute,
		Backoff:        backoff.DefaultSpec(),
	}
}

// Registration binds a job type to its handler.
type Registration struct {
	Type    job.Type
	Handler Handler

	// Timeout limits one attempt; zero means Config.DefaultTimeout.
	Timeout time.Duration

	// Uncancellable marks a handler that is not safe to interrupt: a user cancellation of its running job is
	// refused with ErrNotCancellable (a queued or waiting job can still be cancelled).
	Uncancellable bool
}
