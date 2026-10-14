package icsschedule

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"

	"paratech/desktop/internal/domain/schedule/lesson"
)

const (
	layoutDate     = "20060102"
	layoutDateTime = "20060102T150405"

	paramTZID  = "TZID"
	paramValue = "VALUE"
	valueDate  = "DATE"
)

// stamp is one DATE or DATE-TIME value as written in the file. civil holds the written fields in UTC; whether they
// are an instant depends on the kind.
type stamp struct {
	civil time.Time
	date  bool   // VALUE=DATE: a whole day (all-day events)
	utc   bool   // "Z" suffix: an instant
	tzid  string // TZID parameter; empty for floating (and UTC) values
}

var errStamp = errors.New("not an iCalendar date or date-time")

// parseStamps reads the comma-separated values of a DATE / DATE-TIME property (DTSTART has one, EXDATE may have
// several). golang-ical's own time getters are not used: they read floating times in time.Local, which would make
// the result depend on the machine.
func parseStamps(p *ics.IANAProperty) ([]stamp, error) {
	tzids := param(p, paramTZID)
	if len(tzids) > 1 {
		return nil, errStamp
	}
	tzid := ""
	if len(tzids) == 1 {
		tzid = strings.TrimSpace(tzids[0])
	}
	values := param(p, paramValue)
	dateOnly := len(values) == 1 && strings.EqualFold(values[0], valueDate)

	var out []stamp
	for _, v := range strings.Split(p.Value, ",") {
		s, err := parseStamp(strings.TrimSpace(v), tzid)
		if err != nil {
			return nil, err
		}
		if dateOnly && !s.date {
			return nil, errStamp
		}
		out = append(out, s)
	}
	return out, nil
}

func parseStamp(v, tzid string) (stamp, error) {
	switch len(v) {
	case len(layoutDate):
		t, err := time.Parse(layoutDate, v)
		if err != nil {
			return stamp{}, errStamp
		}
		return stamp{civil: t, date: true}, nil
	case len(layoutDateTime), len(layoutDateTime) + 1:
		utc := len(v) == len(layoutDateTime)+1
		if utc && v[len(v)-1] != 'Z' {
			return stamp{}, errStamp
		}
		t, err := time.Parse(layoutDateTime, v[:len(layoutDateTime)])
		if err != nil {
			return stamp{}, errStamp
		}
		if utc {
			tzid = "" // RFC 5545: TZID must not be used with UTC times; the "Z" wins
		}
		return stamp{civil: t, utc: utc, tzid: tzid}, nil
	}
	return stamp{}, errStamp
}

// param returns the values of a property parameter; parameter names are case-insensitive (RFC 5545 section 2). The
// same name in several spellings is merged in key order, so the result never depends on map order.
func param(p *ics.IANAProperty, name string) []string {
	var keys []string
	for k := range p.ICalParameters {
		if strings.EqualFold(k, name) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, p.ICalParameters[k]...)
	}
	return out
}

// zones resolves TZID values and caches them for one Parse call.
type zones map[string]*time.Location

// resolve maps a TZID to a location: an IANA name first, then a Windows zone name (Outlook and Exchange exports, for
// example "Russian Standard Time") through golang-ical's CLDR table. "Local" and "" are refused: LoadLocation maps
// them to the machine zone and to UTC.
func (z zones) resolve(name string) (*time.Location, bool) {
	if loc, ok := z[name]; ok {
		return loc, loc != nil
	}
	var loc *time.Location
	if name != "" && name != "Local" && lesson.ValidateTimeZoneName(name) == nil {
		if l, err := time.LoadLocation(name); err == nil {
			loc = l
		}
	}
	if loc == nil {
		if l := ics.WindowsTimezoneToIANA(name); l != nil && lesson.ValidateTimeZoneName(l.String()) == nil {
			loc = l
		}
	}
	z[name] = loc
	return loc, loc != nil
}

// place puts a wall-clock time on a date in loc. ok is false for a time skipped by a daylight saving change:
// time.Date would silently move it; t is the moved time then.
func place(date time.Time, h, m, s int, loc *time.Location) (t time.Time, ok bool) {
	t = time.Date(date.Year(), date.Month(), date.Day(), h, m, s, 0, loc)
	ok = t.Year() == date.Year() && t.Month() == date.Month() && t.Day() == date.Day() &&
		t.Hour() == h && t.Minute() == m && t.Second() == s
	return t, ok
}

// civilDate returns the calendar date of t in its own location, as midnight UTC (for date arithmetic without DST).
func civilDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

var errDuration = errors.New("not an iCalendar duration")

// maxDurationSeconds keeps the conversion to time.Duration from overflowing.
const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

// parseDuration reads a non-negative RFC 5545 duration: P[n]W or P[nD][T[nH][nM][nS]]. Days count as 24 hours.
func parseDuration(v string) (time.Duration, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "+")
	if !strings.HasPrefix(v, "P") || len(v) < 3 {
		return 0, errDuration
	}
	v = v[1:]

	var total int64
	inTime := false
	seen := false
	for len(v) > 0 {
		if v[0] == 'T' {
			if inTime {
				return 0, errDuration
			}
			inTime = true
			v = v[1:]
			continue
		}
		i := 0
		for i < len(v) && v[i] >= '0' && v[i] <= '9' {
			i++
		}
		if i == 0 || i > 9 || i == len(v) {
			return 0, errDuration
		}
		n, err := strconv.ParseInt(v[:i], 10, 64)
		if err != nil {
			return 0, errDuration
		}
		var unit int64
		switch u := v[i]; {
		case u == 'W' && !inTime:
			unit = 7 * 24 * 3600
		case u == 'D' && !inTime:
			unit = 24 * 3600
		case u == 'H' && inTime:
			unit = 3600
		case u == 'M' && inTime:
			unit = 60
		case u == 'S' && inTime:
			unit = 1
		default:
			return 0, errDuration
		}
		total += n * unit // n < 1e9 and unit <= 604800, so no overflow
		if total > maxDurationSeconds {
			return 0, errDuration
		}
		seen = true
		v = v[i+1:]
	}
	if !seen {
		return 0, errDuration
	}
	return time.Duration(total) * time.Second, nil
}
