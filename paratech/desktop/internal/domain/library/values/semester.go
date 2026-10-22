package values

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidSemester is returned by ParseSemester for a code that is not a semester.
var ErrInvalidSemester = errors.New("invalid semester")

// Half is the part of the academic year.
type Half string

// Halves of the academic year.
const (
	Autumn Half = "autumn" // September to January
	Spring Half = "spring" // February to August
)

// Years a semester may have.
const (
	minSemesterYear = 2000
	maxSemesterYear = 2100
)

// Semester is one half of an academic year, named by the calendar year it starts in. The zero value is no semester.
type Semester struct {
	year int
	half Half
}

// SemesterOf derives the semester of a date, read in the date's own location: September to December are the autumn
// of that year, January belongs to the autumn of the year before (its exams), February to August are the spring.
func SemesterOf(t time.Time) Semester {
	switch m := t.Month(); {
	case m >= time.September:
		return Semester{year: t.Year(), half: Autumn}
	case m == time.January:
		return Semester{year: t.Year() - 1, half: Autumn}
	default:
		return Semester{year: t.Year(), half: Spring}
	}
}

// ParseSemester reads the stored code "<year>-autumn" or "<year>-spring".
func ParseSemester(code string) (Semester, error) {
	y, h, ok := strings.Cut(code, "-")
	if !ok || len(y) != 4 {
		return Semester{}, fmt.Errorf("%w: %q", ErrInvalidSemester, code)
	}
	year, err := strconv.Atoi(y)
	if err != nil || year < minSemesterYear || year > maxSemesterYear {
		return Semester{}, fmt.Errorf("%w: %q", ErrInvalidSemester, code)
	}
	switch Half(h) {
	case Autumn, Spring:
		return Semester{year: year, half: Half(h)}, nil
	}
	return Semester{}, fmt.Errorf("%w: %q", ErrInvalidSemester, code)
}

// IsZero reports whether s is no semester.
func (s Semester) IsZero() bool { return s == Semester{} }

// Year returns the calendar year the semester starts in.
func (s Semester) Year() int { return s.year }

// Half returns the part of the academic year.
func (s Semester) Half() Half { return s.half }

// Code returns the stored form "<year>-<half>"; "" for the zero value.
func (s Semester) Code() string {
	if s.IsZero() {
		return ""
	}
	return strconv.Itoa(s.year) + "-" + string(s.half)
}

// Label returns the Russian label shown, «Осень 2026» or «Весна 2027»; "" for the zero value.
func (s Semester) Label() string {
	switch s.half {
	case Autumn:
		return "Осень " + strconv.Itoa(s.year)
	case Spring:
		return "Весна " + strconv.Itoa(s.year)
	}
	return ""
}
