// Package clock is the real time source behind the Clock and Sleeper ports of the use cases (ARCH-24).
package clock

import (
	"context"
	"time"
)

// System reads the system clock. The zero value is ready to use.
type System struct{}

// Now returns the current time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error. A non-positive d returns
// at once.
func (System) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Local reads the system clock in the local time zone of the computer: the student's zone, for the use cases whose
// Clock port names that zone (lesson times shown to the model, deadlines). The zero value is ready to use.
type Local struct{}

// Now returns the current time in the local time zone.
func (Local) Now() time.Time { return time.Now() }
