package settings

import (
	"fmt"
	"time"
)

// CalendarOnDueChange is what happens to the Google deadline event when the student changes the deadline of an
// assignment («События в календаре при смене срока», ADR-0029 addendum 2, З3).
type CalendarOnDueChange string

// Calendar modes on a deadline change.
const (
	CalendarAsk    CalendarOnDueChange = "ask"    // «спрашивать»: the student is asked each time
	CalendarUpdate CalendarOnDueChange = "update" // «обновлять сами»: the event follows the deadline
	CalendarKeep   CalendarOnDueChange = "keep"   // «не обновлять»: the event stays as it is
)

// DefaultCalendarOnDueChange is the mode of a fresh installation and of every database from before the setting.
const DefaultCalendarOnDueChange = CalendarAsk

// Valid reports whether m is a known mode.
func (m CalendarOnDueChange) Valid() bool {
	return m == CalendarAsk || m == CalendarUpdate || m == CalendarKeep
}

// OrDefault returns m when it is known, otherwise DefaultCalendarOnDueChange.
func (m CalendarOnDueChange) OrDefault() CalendarOnDueChange {
	if m.Valid() {
		return m
	}
	return DefaultCalendarOnDueChange
}

// String returns the stored form of the mode.
func (m CalendarOnDueChange) String() string { return string(m) }

// CalendarOnDueChange returns what happens to the Google event when the student changes a deadline.
func (s *Settings) CalendarOnDueChange() CalendarOnDueChange { return s.calendarOnDueChange }

// SetCalendarOnDueChange sets what happens to the Google event when the student changes a deadline; it applies to the
// next change.
func (s *Settings) SetCalendarOnDueChange(m CalendarOnDueChange, now time.Time) error {
	if !m.Valid() {
		return fmt.Errorf("%w: unknown calendar mode %q", ErrInvalidSettings, m)
	}
	return change(s, &s.calendarOnDueChange, m, now)
}
