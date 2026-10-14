package icsschedule

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	ics "github.com/arran4/golang-ical"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Property names (RowError.Field uses them).
const (
	propUID          = "UID"
	propSummary      = "SUMMARY"
	propStatus       = "STATUS"
	propDTStart      = "DTSTART"
	propDTEnd        = "DTEND"
	propDuration     = "DURATION"
	propRRule        = "RRULE"
	propExRule       = "EXRULE"
	propExDate       = "EXDATE"
	propRDate        = "RDATE"
	propRecurrenceID = "RECURRENCE-ID"
	propXWRTimezone  = "X-WR-TIMEZONE"

	statusCancelled = "CANCELLED"
	paramRange      = "RANGE"
)

// Identity of events in row keys.
const (
	maxRawUIDBytes = 255 // longer or odd UIDs are hashed so the source uid stays within lesson.MaxSourceUIDBytes
	idHashPrefix   = "sha256-"
	idAutoPrefix   = "auto-" // no UID: the identity is the title (with the start in the key)
	keyTimeLayout  = time.RFC3339
)

// Row error reasons of an event; they quote nothing from the file.
const (
	reasonNoStart       = "no DTSTART"
	reasonBadTime       = "not a valid date-time"
	reasonUnknownZone   = "TZID is not a known IANA or Windows time zone"
	reasonFloating      = "a time without a time zone, and the calendar has no X-WR-TIMEZONE"
	reasonGap           = "a time skipped by a daylight saving change"
	reasonNoEnd         = "neither DTEND nor DURATION"
	reasonBothEnds      = "both DTEND and DURATION"
	reasonBadDuration   = "not a valid duration"
	reasonDateMix       = "a date where a date-time is expected"
	reasonManyRules     = "more than one RRULE"
	reasonExRule        = "EXRULE is not supported"
	reasonOverrideRule  = "a changed occurrence (RECURRENCE-ID) with its own RRULE"
	reasonRange         = "RECURRENCE-ID with RANGE is not supported"
	reasonDuplicateUID  = "another event has the same UID"
	reasonDuplicateOccu = "another changed occurrence has the same UID and RECURRENCE-ID"
)

// event is one VEVENT as far as an import needs it.
type event struct {
	line      int
	uid       string // as written; "" when missing
	id        string // identity part of the row keys
	title     string
	cancelled bool
	allDay    bool

	start    time.Time      // instant of DTSTART, in the expansion zone
	label    *time.Location // zone of the lesson content (TimeZone); differs from the expansion zone for UTC times
	duration time.Duration

	recurrenceID *time.Time // set for a changed (or cancelled) occurrence of a series

	rule      *rule
	ruleEnded bool // RRULE ends before the import bounds
	hasRule   bool
	exInstant []time.Time
	exDay     []time.Time // EXDATE;VALUE=DATE: civil dates
	rdates    []time.Time
}

// reader turns VEVENTs into events; one per Parse call.
type reader struct {
	zones    zones
	calZone  *time.Location // X-WR-TIMEZONE, nil when absent or unknown
	from, to time.Time
}

func rowError(line int, field, reason string) *importplan.RowError {
	return &importplan.RowError{Line: line, Field: field, Reason: reason}
}

// read converts one VEVENT. All-day events are returned with allDay set and nothing else checked: they are not
// lessons and are skipped. A cancelled event needs only its identity.
func (r *reader) read(ev *ics.VEvent, line int) (event, *importplan.RowError) {
	e := event{line: line}
	if p := prop(ev, propUID); p != nil {
		e.uid = strings.TrimSpace(p.Value)
	}
	if p := prop(ev, propSummary); p != nil {
		e.title = cleanTitle(p.Value)
	}
	e.id = identity(e.uid, e.title)
	if p := prop(ev, propStatus); p != nil {
		e.cancelled = strings.EqualFold(strings.TrimSpace(p.Value), statusCancelled)
	}
	recur := prop(ev, propRecurrenceID)

	startProp := prop(ev, propDTStart)
	if startProp == nil {
		if e.cancelled && recur == nil {
			return e, nil
		}
		return e, rowError(line, propDTStart, reasonNoStart)
	}
	st, rerr := single(startProp, line, propDTStart)
	if rerr != nil {
		return e, rerr
	}
	if st.date {
		e.allDay = true
		return e, nil
	}
	loc, label, reason := r.eventZone(st)
	if reason != "" {
		return e, rowError(line, propDTStart, reason)
	}
	e.label = label
	if e.start, reason = r.instant(st, loc); reason != "" {
		return e, rowError(line, propDTStart, reason)
	}

	if recur != nil {
		if len(param(recur, paramRange)) > 0 {
			return e, rowError(line, propRecurrenceID, reasonRange)
		}
		rs, rerr := single(recur, line, propRecurrenceID)
		if rerr != nil {
			return e, rerr
		}
		if rs.date {
			return e, rowError(line, propRecurrenceID, reasonDateMix)
		}
		t, reason := r.instant(rs, loc)
		if reason != "" {
			return e, rowError(line, propRecurrenceID, reason)
		}
		e.recurrenceID = &t
	}
	if e.cancelled {
		return e, nil
	}

	if rerr := r.readDuration(ev, &e, loc); rerr != nil {
		return e, rerr
	}
	return e, r.readRecurrence(ev, &e, loc)
}

func (r *reader) readDuration(ev *ics.VEvent, e *event, loc *time.Location) *importplan.RowError {
	endProp, durProp := prop(ev, propDTEnd), prop(ev, propDuration)
	switch {
	case endProp == nil && durProp == nil:
		return rowError(e.line, propDTEnd, reasonNoEnd)
	case endProp != nil && durProp != nil:
		return rowError(e.line, propDuration, reasonBothEnds)
	case durProp != nil:
		d, err := parseDuration(durProp.Value)
		if err != nil {
			return rowError(e.line, propDuration, reasonBadDuration)
		}
		e.duration = d
		return nil
	}

	es, rerr := single(endProp, e.line, propDTEnd)
	if rerr != nil {
		return rerr
	}
	if es.date {
		return rowError(e.line, propDTEnd, reasonDateMix)
	}
	end, reason := r.instant(es, loc)
	if reason != "" {
		return rowError(e.line, propDTEnd, reason)
	}
	// An end before the start is kept as a negative length: the lesson rules of importplan report it.
	e.duration = end.Sub(e.start)
	return nil
}

func (r *reader) readRecurrence(ev *ics.VEvent, e *event, loc *time.Location) *importplan.RowError {
	if len(props(ev, propExRule)) > 0 {
		return rowError(e.line, propExRule, reasonExRule)
	}
	rules := props(ev, propRRule)
	switch {
	case len(rules) > 1:
		return rowError(e.line, propRRule, reasonManyRules)
	case len(rules) == 1 && e.recurrenceID != nil:
		return rowError(e.line, propRRule, reasonOverrideRule)
	case len(rules) == 1:
		e.hasRule = true
		// A series that starts after the bounds has no occurrence in them; its rule is not checked.
		if e.start.Before(r.to) {
			ru, ended, reason := compileRule(rules[0].Value, loc, r.from)
			if reason != "" {
				return rowError(e.line, propRRule, reason)
			}
			e.rule, e.ruleEnded = ru, ended
		}
	}

	for _, p := range props(ev, propExDate) {
		stamps, err := parseStamps(p)
		if err != nil {
			return rowError(e.line, propExDate, reasonBadTime)
		}
		for _, s := range stamps {
			if s.date {
				e.exDay = append(e.exDay, s.civil)
				continue
			}
			t, reason := r.instant(s, loc)
			if reason != "" {
				return rowError(e.line, propExDate, reason)
			}
			e.exInstant = append(e.exInstant, t)
		}
	}
	for _, p := range props(ev, propRDate) {
		stamps, err := parseStamps(p)
		if err != nil {
			return rowError(e.line, propRDate, reasonBadTime) // also VALUE=PERIOD, which is not supported
		}
		for _, s := range stamps {
			if s.date {
				return rowError(e.line, propRDate, reasonDateMix)
			}
			t, reason := r.instant(s, loc)
			if reason != "" {
				return rowError(e.line, propRDate, reason)
			}
			e.rdates = append(e.rdates, t)
		}
	}
	return nil
}

// single reads a property that holds exactly one value.
func single(p *ics.IANAProperty, line int, field string) (stamp, *importplan.RowError) {
	stamps, err := parseStamps(p)
	if err != nil || len(stamps) != 1 {
		return stamp{}, rowError(line, field, reasonBadTime)
	}
	return stamps[0], nil
}

// eventZone picks the zone the event recurs in and the zone its lessons show. A UTC DTSTART recurs in UTC (RFC
// 5545) and is shown in X-WR-TIMEZONE when the calendar has one (Google writes single events in UTC); a floating
// DTSTART needs X-WR-TIMEZONE.
func (r *reader) eventZone(st stamp) (loc, label *time.Location, reason string) {
	switch {
	case st.utc:
		if r.calZone != nil {
			return time.UTC, r.calZone, ""
		}
		return time.UTC, time.UTC, ""
	case st.tzid != "":
		l, ok := r.zones.resolve(st.tzid)
		if !ok {
			return nil, nil, reasonUnknownZone
		}
		return l, l, ""
	case r.calZone != nil:
		return r.calZone, r.calZone, ""
	}
	return nil, nil, reasonFloating
}

// instant turns a date-time into an instant. A floating value of an event property (EXDATE, RECURRENCE-ID, ...)
// is read in the event zone loc.
func (r *reader) instant(s stamp, loc *time.Location) (time.Time, string) {
	c := s.civil
	switch {
	case s.date:
		return time.Time{}, reasonDateMix
	case s.utc:
		return c, ""
	case s.tzid != "":
		l, ok := r.zones.resolve(s.tzid)
		if !ok {
			return time.Time{}, reasonUnknownZone
		}
		loc = l
	}
	t, ok := place(c, c.Hour(), c.Minute(), c.Second(), loc)
	if !ok {
		return time.Time{}, reasonGap
	}
	return t.In(loc), ""
}

// prop returns the first property of that name; names are case-insensitive (RFC 5545 section 2).
func prop(ev *ics.VEvent, name string) *ics.IANAProperty {
	for i := range ev.Properties {
		if strings.EqualFold(ev.Properties[i].IANAToken, name) {
			return &ev.Properties[i]
		}
	}
	return nil
}

func props(ev *ics.VEvent, name string) []*ics.IANAProperty {
	var out []*ics.IANAProperty
	for i := range ev.Properties {
		if strings.EqualFold(ev.Properties[i].IANAToken, name) {
			out = append(out, &ev.Properties[i])
		}
	}
	return out
}

// cleanTitle turns the SUMMARY (already unescaped by golang-ical) into one line: an escaped line break or a tab
// becomes a space. Other control characters stay and the lesson rules refuse them.
func cleanTitle(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	return strings.TrimSpace(s)
}

// identity is the stable part of an event's row keys: the UID when it is short printable text, else its SHA-256;
// without a UID, a hash of the title (so the key is "title + start", as for CSV rows without uid).
func identity(uid, title string) string {
	switch {
	case uid == "":
		sum := sha256.Sum256([]byte(title))
		return idAutoPrefix + hex.EncodeToString(sum[:16])
	case len(uid) > maxRawUIDBytes || !utf8.ValidString(uid) || strings.ContainsFunc(uid, unicode.IsControl):
		sum := sha256.Sum256([]byte(uid))
		return idHashPrefix + hex.EncodeToString(sum[:])
	}
	return uid
}

// key is the row key of an occurrence: the event identity and the original start of the occurrence in UTC. A changed
// occurrence keeps the key of the time it replaces, so moving it in the calendar changes the lesson instead of
// replacing it.
func key(id string, original time.Time) string {
	return id + "/" + original.UTC().Format(keyTimeLayout)
}
