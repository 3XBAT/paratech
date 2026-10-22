package values

import (
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date is a calendar date without a time zone. As a deadline it means the end of that day in the student's time
// zone (catalog type "date").
type Date struct {
	year  int
	month time.Month
	day   int
}

// ParseDate accepts exactly "YYYY-MM-DD" of a real day between 2000 and 2100.
func ParseDate(s string) (Date, error) {
	if len(s) != len(dateLayout) {
		return Date{}, fmt.Errorf("%w: want YYYY-MM-DD", ErrInvalidDate)
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("%w: want YYYY-MM-DD", ErrInvalidDate)
	}
	return NewDate(t.Year(), t.Month(), t.Day())
}

// NewDate builds a date and checks that the day exists.
func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < 2000 || year > 2100 {
		return Date{}, fmt.Errorf("%w: year out of range", ErrInvalidDate)
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("%w: no such day", ErrInvalidDate)
	}
	return Date{year: year, month: month, day: day}, nil
}

// IsZero reports whether d is unset.
func (d Date) IsZero() bool { return d == Date{} }

// Year returns the year.
func (d Date) Year() int { return d.year }

// Month returns the month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool {
	if d.year != o.year {
		return d.year < o.year
	}
	if d.month != o.month {
		return d.month < o.month
	}
	return d.day < o.day
}

// String returns "YYYY-MM-DD", or "" for zero.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}
