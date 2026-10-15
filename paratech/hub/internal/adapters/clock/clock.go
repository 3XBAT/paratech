// Package clock is the real time source behind the Clock ports of the use cases (ARCH-24).
package clock

import "time"

// System reads the system clock. The zero value is ready to use.
type System struct{}

// Now returns the current time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }
