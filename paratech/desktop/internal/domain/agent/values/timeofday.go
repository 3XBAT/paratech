package values

import "fmt"

// TimeOfDay is a time of day without a date or a zone (catalog type "time", v6): with a deadline date it is that
// moment in the student's time zone. The zero value is no time.
type TimeOfDay struct {
	minutes int // minutes after midnight, plus one so that the zero value is unset
}

// ParseTimeOfDay accepts exactly "HH:MM", 24 h, two digits each (the Hub's pattern ^([01][0-9]|2[0-3]):[0-5][0-9]$).
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	if len(s) != len("15:04") || s[2] != ':' {
		return TimeOfDay{}, fmt.Errorf("%w: want HH:MM", ErrInvalidTime)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return TimeOfDay{}, fmt.Errorf("%w: want HH:MM", ErrInvalidTime)
		}
	}
	return NewTimeOfDay(int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0'))
}

// NewTimeOfDay builds a time of day of hour 0..23 and minute 0..59.
func NewTimeOfDay(hour, minute int) (TimeOfDay, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return TimeOfDay{}, fmt.Errorf("%w: out of range", ErrInvalidTime)
	}
	return TimeOfDay{minutes: hour*60 + minute + 1}, nil
}

// IsZero reports whether t is unset.
func (t TimeOfDay) IsZero() bool { return t.minutes == 0 }

// Hour returns the hour, 0 for zero.
func (t TimeOfDay) Hour() int {
	if t.IsZero() {
		return 0
	}
	return (t.minutes - 1) / 60
}

// Minute returns the minute, 0 for zero.
func (t TimeOfDay) Minute() int {
	if t.IsZero() {
		return 0
	}
	return (t.minutes - 1) % 60
}

// String returns the canonical "HH:MM", or "" for zero.
func (t TimeOfDay) String() string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}
