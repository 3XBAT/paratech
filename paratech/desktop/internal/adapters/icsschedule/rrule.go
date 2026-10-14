package icsschedule

import (
	"context"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
)

// maxInterval bounds INTERVAL so period arithmetic stays far from overflow; no class repeats less often.
const maxInterval = 1000

// rule is the supported subset of an RRULE (see the package comment), compiled for one event.
type rule struct {
	freq     ics.Frequency
	interval int
	count    int                   // 0: no COUNT
	byDay    map[time.Weekday]bool // nil: none (WEEKLY then uses the weekday of DTSTART)
	wkst     time.Weekday

	untilSet     bool
	untilDate    bool      // UNTIL is a DATE: occurrences on or before that date (in the event zone)
	untilInstant time.Time // UNTIL as an instant (UTC, or a floating value placed in the event zone)
	untilDay     time.Time // civil date of a DATE UNTIL
}

var weekdays = map[ics.Weekday]time.Weekday{
	ics.WeekdaySunday: time.Sunday, ics.WeekdayMonday: time.Monday, ics.WeekdayTuesday: time.Tuesday,
	ics.WeekdayWednesday: time.Wednesday, ics.WeekdayThursday: time.Thursday, ics.WeekdayFriday: time.Friday,
	ics.WeekdaySaturday: time.Saturday,
}

// Row error reasons of a rule; they quote nothing from the file.
const (
	reasonRuleSyntax     = "not a valid recurrence rule"
	reasonRuleFreq       = "only DAILY, WEEKLY, MONTHLY and YEARLY recurrences are supported"
	reasonRuleInterval   = "INTERVAL must be between 1 and 1000"
	reasonRuleCount      = "COUNT must be at least 1"
	reasonRuleBoth       = "COUNT and UNTIL cannot be used together"
	reasonRuleParts      = "this recurrence (BYMONTH, BYMONTHDAY, BYSETPOS, ...) is not supported"
	reasonRuleByDay      = "BYDAY is supported only in DAILY and WEEKLY recurrences, without numbers like 2MO"
	reasonRuleGap        = "an occurrence falls on a time skipped by a daylight saving change"
	reasonRuleUntilValue = "UNTIL is not a date or date-time"
)

// compileRule reads an RRULE value with golang-ical (which parses but does not expand) and checks the subset. ended
// reports a rule whose UNTIL lies before from: it has no occurrence in the bounds, so its other parts are not checked.
// loc is the event zone, used for a floating UNTIL.
func compileRule(raw string, loc *time.Location, from time.Time) (r *rule, ended bool, reason string) {
	parsed, err := ics.ParseRecurrenceRule(raw)
	if err != nil {
		return nil, false, reasonRuleSyntax
	}
	parts, ok := ruleParts(raw)
	if !ok {
		return nil, false, reasonRuleSyntax
	}

	r = &rule{freq: parsed.Freq, interval: parsed.Interval, wkst: time.Monday}
	if _, ok := parts["COUNT"]; ok {
		if parsed.Count < 1 {
			return nil, false, reasonRuleCount
		}
		r.count = parsed.Count
	}
	if u, ok := parts["UNTIL"]; ok {
		if r.count > 0 {
			return nil, false, reasonRuleBoth
		}
		if !until(r, u, loc) {
			return nil, false, reasonRuleUntilValue
		}
		if r.before(from.In(loc)) {
			return r, true, ""
		}
	}

	switch r.freq {
	case ics.FrequencyDaily, ics.FrequencyWeekly, ics.FrequencyMonthly, ics.FrequencyYearly:
	default:
		return nil, false, reasonRuleFreq
	}
	if r.interval < 1 || r.interval > maxInterval {
		return nil, false, reasonRuleInterval
	}
	if len(parsed.BySecond)+len(parsed.ByMinute)+len(parsed.ByHour)+len(parsed.ByMonthDay)+len(parsed.ByYearDay)+
		len(parsed.ByWeekNo)+len(parsed.ByMonth)+len(parsed.BySetPos) > 0 {
		return nil, false, reasonRuleParts
	}
	if len(parsed.ByDay) > 0 {
		if r.freq != ics.FrequencyDaily && r.freq != ics.FrequencyWeekly {
			return nil, false, reasonRuleByDay
		}
		r.byDay = map[time.Weekday]bool{}
		for _, d := range parsed.ByDay {
			if d.OrdWeek != 0 {
				return nil, false, reasonRuleByDay
			}
			r.byDay[weekdays[d.Day]] = true
		}
	}
	if parsed.Wkst != "" {
		r.wkst = weekdays[parsed.Wkst]
	}
	return r, false, ""
}

// knownParts are the rule parts of RFC 5545; golang-ical ignores any other key, and a lower-case key too.
var knownParts = map[string]bool{
	"FREQ": true, "UNTIL": true, "COUNT": true, "INTERVAL": true, "BYSECOND": true, "BYMINUTE": true,
	"BYHOUR": true, "BYDAY": true, "BYMONTHDAY": true, "BYYEARDAY": true, "BYWEEKNO": true, "BYMONTH": true,
	"BYSETPOS": true, "WKST": true,
}

// ruleParts splits the raw rule, to see what golang-ical's struct cannot tell: whether COUNT was given and whether
// UNTIL had the "Z" suffix. ok is false for a key golang-ical would silently ignore (lower case, unknown, repeated):
// ignoring it could give wrong dates. X- extensions are ignored.
func ruleParts(raw string) (map[string]string, bool) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		if part == "" {
			continue
		}
		k, v, found := strings.Cut(part, "=")
		if strings.HasPrefix(strings.ToUpper(k), "X-") {
			continue
		}
		if _, dup := out[k]; !found || dup || !knownParts[k] {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}

// until reads UNTIL: a UTC date-time, a floating date-time (wall time in the event zone) or a date (inclusive).
func until(r *rule, v string, loc *time.Location) bool {
	s, err := parseStamp(v, "")
	if err != nil {
		return false
	}
	r.untilSet = true
	switch {
	case s.date:
		r.untilDate, r.untilDay = true, s.civil
	case s.utc:
		r.untilInstant = s.civil
	default:
		c := s.civil
		r.untilInstant = time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, loc)
	}
	return true
}

// before reports whether every occurrence at or after t is past UNTIL.
func (r *rule) before(t time.Time) bool {
	return r.untilSet && !r.allows(t)
}

// allows reports whether an occurrence starting at t (in the event zone) is within UNTIL (inclusive).
func (r *rule) allows(t time.Time) bool {
	switch {
	case !r.untilSet:
		return true
	case r.untilDate:
		return !civilDate(t).After(r.untilDay)
	default:
		return !t.After(r.untilInstant)
	}
}

// expansion is the state of one rule expansion; budget is shared by all rules of a file.
type expansion struct {
	ctx    context.Context
	budget *int
}

// expand returns the starts of the rule's occurrences in [from, to), in order, in the zone of start. The wall-clock
// time of DTSTART is kept on every date, so a lesson stays at 09:00 across a DST change. DTSTART is always the first
// occurrence (RFC 5545 section 3.8.5.3), also when it does not match BYDAY. COUNT counts from DTSTART, so a rule with
// COUNT is walked from its start; others jump close to from. reason is a row problem; err is ErrTooComplex or the
// context error.
func (x expansion) expand(r *rule, start, from, to time.Time) (out []time.Time, reason string, err error) {
	loc := start.Location()
	d0 := civilDate(start)
	h, m, s := start.Clock()
	toDay := civilDate(to.In(loc)).AddDate(0, 0, 1)

	byDay := r.byDay
	if byDay == nil && r.freq == ics.FrequencyWeekly {
		byDay = map[time.Weekday]bool{d0.Weekday(): true}
	}
	weekStart0 := d0.AddDate(0, 0, -((int(d0.Weekday()) - int(r.wkst) + 7) % 7))

	emitted := 0
	// emit handles one occurrence in order; stop ends the expansion.
	emit := func(t time.Time) (stop bool) {
		if (r.count > 0 && emitted >= r.count) || !r.allows(t) || !t.Before(to) {
			return true
		}
		emitted++
		if !t.Before(from) {
			out = append(out, t)
		}
		return false
	}

	k := 0
	if r.count == 0 {
		k = r.periodsBefore(d0, civilDate(from.In(loc)))
	}
	if k == 0 && byDay != nil && !byDay[d0.Weekday()] && emit(start) {
		return out, "", nil
	}

	for ; ; k++ {
		*x.budget--
		if *x.budget < 0 {
			return nil, "", &Error{Kind: ErrTooComplex, Detail: "use a shorter date range or simpler rules"}
		}
		if *x.budget%cancelCheck == 0 {
			if err := x.ctx.Err(); err != nil {
				return nil, "", err
			}
		}

		period, days := r.period(k, d0, weekStart0)
		if period.After(toDay) {
			return out, "", nil
		}
		for _, day := range days {
			if day.Before(d0) || (byDay != nil && !byDay[day.Weekday()]) {
				continue
			}
			t, ok := place(day, h, m, s, loc)
			if !ok && !t.Before(from) && t.Before(to) {
				return nil, reasonRuleGap, nil
			}
			if emit(t) {
				return out, "", nil
			}
		}
	}
}

// period returns the first day of period k and its candidate days in order. A MONTHLY or YEARLY candidate that does
// not exist (the 31st of a short month, February 29) is skipped, as RFC 5545 requires.
func (r *rule) period(k int, d0, weekStart0 time.Time) (time.Time, []time.Time) {
	n := k * r.interval
	switch r.freq {
	case ics.FrequencyDaily:
		day := d0.AddDate(0, 0, n)
		return day, []time.Time{day}
	case ics.FrequencyWeekly:
		ws := weekStart0.AddDate(0, 0, 7*n)
		days := make([]time.Time, 7)
		for i := range days {
			days[i] = ws.AddDate(0, 0, i)
		}
		return ws, days
	case ics.FrequencyMonthly:
		first := time.Date(d0.Year(), d0.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
		return first, existing(first.Year(), first.Month(), d0.Day())
	default: // YEARLY
		first := time.Date(d0.Year()+n, d0.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first, existing(first.Year(), first.Month(), d0.Day())
	}
}

func existing(y int, m time.Month, d int) []time.Time {
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if day.Month() != m {
		return nil
	}
	return []time.Time{day}
}

// periodsBefore returns a period index whose period starts on or before fromDay (one period early for safety), so a
// rule without COUNT does not walk years of history.
func (r *rule) periodsBefore(d0, fromDay time.Time) int {
	if !fromDay.After(d0) {
		return 0
	}
	days := int((fromDay.Unix() - d0.Unix()) / (24 * 3600)) // Unix seconds: Sub saturates for far-apart dates
	var k int
	switch r.freq {
	case ics.FrequencyDaily:
		k = days / r.interval
	case ics.FrequencyWeekly:
		k = days / (7 * r.interval)
	case ics.FrequencyMonthly:
		k = ((fromDay.Year()-d0.Year())*12 + int(fromDay.Month()) - int(d0.Month())) / r.interval
	default:
		k = (fromDay.Year() - d0.Year()) / r.interval
	}
	return max(k-1, 0)
}
