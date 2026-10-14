package icsschedule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// secret is written into hostile values; no message may quote it.
const secret = "SECRETVALUE"

func TestRowErrors(t *testing.T) {
	oct := [2]time.Time{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	march := [2]time.Time{time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)}
	const start, end = "DTSTART;TZID=Europe/Moscow:20261005T090000", "DTEND;TZID=Europe/Moscow:20261005T100000"
	rule := func(r string) []string { return []string{start, end, "RRULE:" + r} }

	tests := []struct {
		name   string
		header string
		props  []string
		extra  string
		bounds [2]time.Time
		field  string
		reason string
	}{
		{"unknown zone", "", []string{"DTSTART;TZID=" + secret + ":20261005T090000", "DURATION:PT1H"}, "", oct, propDTStart, reasonUnknownZone},
		{"floating without calendar zone", "", []string{"DTSTART:20261005T090000", "DURATION:PT1H"}, "", oct, propDTStart, reasonFloating},
		{"no start", "", []string{"DURATION:PT1H"}, "", oct, propDTStart, reasonNoStart},
		{"bad start", "", []string{"DTSTART:2026" + secret, "DURATION:PT1H"}, "", oct, propDTStart, reasonBadTime},
		{"no end", "", []string{start}, "", oct, propDTEnd, reasonNoEnd},
		{"both ends", "", []string{start, end, "DURATION:PT1H"}, "", oct, propDuration, reasonBothEnds},
		{"bad duration", "", []string{start, "DURATION:P" + secret}, "", oct, propDuration, reasonBadDuration},
		{"date end of a timed event", "", []string{start, "DTEND;VALUE=DATE:20261006"}, "", oct, propDTEnd, reasonDateMix},
		{"dst gap at the start", "", []string{"DTSTART;TZID=Europe/Berlin:20270328T023000", "DURATION:PT1H"}, "", march, propDTStart, reasonGap},
		{"dst gap in the series", "", []string{"DTSTART;TZID=Europe/Berlin:20270327T023000", "DURATION:PT1H", "RRULE:FREQ=DAILY;COUNT=3"},
			"", march, propRRule, reasonRuleGap},
		{"hourly", "", rule("FREQ=HOURLY"), "", oct, propRRule, reasonRuleFreq},
		{"interval 0", "", rule("FREQ=WEEKLY;INTERVAL=0"), "", oct, propRRule, reasonRuleInterval},
		{"count 0", "", rule("FREQ=WEEKLY;COUNT=0"), "", oct, propRRule, reasonRuleCount},
		{"count and until", "", rule("FREQ=WEEKLY;COUNT=3;UNTIL=20261231T000000Z"), "", oct, propRRule, reasonRuleBoth},
		{"bymonthday", "", rule("FREQ=MONTHLY;BYMONTHDAY=5"), "", oct, propRRule, reasonRuleParts},
		{"bysetpos", "", rule("FREQ=WEEKLY;BYDAY=MO,TU;BYSETPOS=1"), "", oct, propRRule, reasonRuleParts},
		{"numbered byday", "", rule("FREQ=MONTHLY;BYDAY=1MO"), "", oct, propRRule, reasonRuleByDay},
		{"numbered byday in weekly", "", rule("FREQ=WEEKLY;BYDAY=2MO"), "", oct, propRRule, reasonRuleByDay},
		{"lower-case key golang-ical would ignore", "", rule("FREQ=WEEKLY;count=3"), "", oct, propRRule, reasonRuleSyntax},
		{"unknown key", "", rule("FREQ=WEEKLY;" + secret + "=1"), "", oct, propRRule, reasonRuleSyntax},
		{"bad until", "", rule("FREQ=WEEKLY;UNTIL=" + secret), "", oct, propRRule, reasonRuleSyntax},
		{"two rules", "", append(rule("FREQ=WEEKLY"), "RRULE:FREQ=DAILY"), "", oct, propRRule, reasonManyRules},
		{"exrule", "", append(rule("FREQ=WEEKLY"), "EXRULE:FREQ=DAILY"), "", oct, propExRule, reasonExRule},
		{"bad exdate", "", append(rule("FREQ=WEEKLY"), "EXDATE:"+secret), "", oct, propExDate, reasonBadTime},
		{"rdate period", "", append(rule("FREQ=WEEKLY"), "RDATE;VALUE=PERIOD:20261010T090000Z/PT1H"), "", oct, propRDate, reasonBadTime},
		{"range", "", []string{"RECURRENCE-ID;RANGE=THISANDFUTURE;TZID=Europe/Moscow:20261005T090000", start, end}, "", oct,
			propRecurrenceID, reasonRange},
		{"changed occurrence with a rule", "", []string{"RECURRENCE-ID;TZID=Europe/Moscow:20261005T090000", start, end, "RRULE:FREQ=DAILY"},
			"", oct, propRRule, reasonOverrideRule},
		{"duplicate uid", "", []string{start, end}, "BEGIN:VEVENT\nUID:ev-1\nSUMMARY:Again\nDTSTART:20261006T090000Z\nDURATION:PT1H\nEND:VEVENT\n",
			oct, propUID, reasonDuplicateUID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWith(t, newParser(t, tt.bounds[0], tt.bounds[1]), calendar(tt.header, tt.props, tt.extra))
			if len(got.Errors) != 1 {
				t.Fatalf("errors = %+v, rows = %d", got.Errors, len(got.Rows))
			}
			e := got.Errors[0]
			wantLine := 3
			if tt.reason == reasonDuplicateUID {
				wantLine = 7 + len(tt.props) // the second VEVENT
			}
			if e.Line != wantLine || e.Field != tt.field || e.Reason != tt.reason {
				t.Fatalf("error = %+v, want line %d field %s reason %q", e, wantLine, tt.field, tt.reason)
			}
			if strings.Contains(e.Reason, secret) {
				t.Fatalf("reason quotes the file: %q", e.Reason)
			}
			plan, err := importplan.Build(importplan.SourceICS, got, nil)
			if err != nil || plan.Valid() {
				t.Fatalf("a row error must block the plan: %+v, %v", plan, err)
			}
		})
	}
}

func TestSkippedEvents(t *testing.T) {
	oct := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	tests := []struct {
		name  string
		props []string
	}{
		{"all-day event", []string{"DTSTART;VALUE=DATE:20261005", "DTEND;VALUE=DATE:20261006", "RRULE:FREQ=WEEKLY"}},
		{"cancelled event", []string{"STATUS:CANCELLED", "DTSTART:20261005T090000Z", "DURATION:PT1H"}},
		{"cancelled event without times", []string{"STATUS:CANCELLED"}},
		{"series before the bounds", []string{"DTSTART:20250105T090000Z", "DURATION:PT1H", "RRULE:FREQ=WEEKLY;UNTIL=20250601T000000Z;BYMONTH=1"}},
		{"series after the bounds", []string{"DTSTART:20270105T090000Z", "DURATION:PT1H", "RRULE:FREQ=MONTHLY;BYMONTHDAY=5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWith(t, oct, calendar("", tt.props, ""))
			if len(got.Rows) != 0 || len(got.Errors) != 0 {
				t.Fatalf("parsed = %+v", got)
			}
		})
	}
}

func TestFileErrors(t *testing.T) {
	ev := func(n int, body string) string {
		return strings.Repeat("BEGIN:VEVENT\n"+body+"END:VEVENT\n", n)
	}
	cal := func(body string) string { return "BEGIN:VCALENDAR\nVERSION:2.0\n" + body + "END:VCALENDAR\n" }
	// Old series with a large COUNT are walked from DTSTART: every one costs about 46000 steps.
	old := func(i int) string {
		return "BEGIN:VEVENT\nUID:old-" + string(rune('a'+i)) + "\nSUMMARY:Old\nDTSTART:19000101T090000Z\nDURATION:PT1H\n" +
			"RRULE:FREQ=DAILY;COUNT=999999\nEND:VEVENT\n"
	}
	var complexBody string
	for i := range 5 {
		complexBody += old(i)
	}

	tests := []struct {
		name string
		data string
		from time.Time
		days int // length of the bounds; 0 is 20 years
		kind error
	}{
		{"too large", cal(strings.Repeat("X-PAD:"+strings.Repeat("a", 1000)+"\n", MaxFileBytes/1000)), time.Time{}, 0, ErrTooLarge},
		{"not utf-8", cal("X-NAME:\xff\xfe" + secret + "\n"), time.Time{}, 0, ErrEncoding},
		{"not a calendar", "BEGIN:VCARD\nFN:" + secret + "\nEND:VCARD\n", time.Time{}, 0, ErrNotCalendar},
		{"cut file", "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:" + secret + "\n", time.Time{}, 0, ErrNotCalendar},
		{"two calendars", cal("") + cal(""), time.Time{}, 0, ErrNotCalendar},
		{"empty", "", time.Time{}, 0, ErrNotCalendar},
		{"long line", cal("X-PAD:" + strings.Repeat(secret, maxLineBytes/len(secret)+1) + "\n"), time.Time{}, 0, ErrMalformed},
		{"folded long line", cal("X-PAD:a\n" + strings.Repeat(" "+strings.Repeat("b", 1000)+"\n", 70)), time.Time{}, 0, ErrMalformed},
		{"many parameters", cal("X-P" + strings.Repeat(";A=1", maxParamSeparators+1) + ":" + secret + "\n"), time.Time{}, 0, ErrMalformed},
		{"deep nesting", cal(strings.Repeat("BEGIN:X\n", maxDepth) + strings.Repeat("END:X\n", maxDepth)), time.Time{}, 0, ErrMalformed},
		{"broken syntax", cal(ev(1, "UID:a\n"+secret+"\n")), time.Time{}, 0, ErrMalformed},
		{"too many events", cal(ev(MaxEvents+1, "")), time.Time{}, 0, ErrTooMany},
		{"too many lessons", cal(ev(1, "UID:d\nSUMMARY:Daily\nDTSTART:20260101T090000Z\nDURATION:PT1H\nRRULE:FREQ=DAILY\n")),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0, ErrTooMany},
		{"too complex", cal(complexBody), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 1, ErrTooComplex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := tt.from
			if from.IsZero() {
				from = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			}
			to := from.AddDate(20, 0, 0)
			if tt.days > 0 {
				to = from.AddDate(0, 0, tt.days)
			}
			p := newParser(t, from, to)
			_, err := p.Parse(context.Background(), []byte(tt.data))
			var fe *Error
			if !errors.Is(err, tt.kind) || !errors.Is(err, importplan.ErrUnreadableFile) || !errors.As(err, &fe) {
				t.Fatalf("Parse = %v, want %v", err, tt.kind)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("message quotes the file: %v", err)
			}
		})
	}
}

func TestFileErrorLine(t *testing.T) {
	p := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	_, err := p.Parse(context.Background(), []byte("BEGIN:VCALENDAR\nX-PAD:a\n b\nX-P"+strings.Repeat(";A=1", 40)+":x\nEND:VCALENDAR\n"))
	var fe *Error
	if !errors.As(err, &fe) || fe.Line != 4 || !strings.Contains(err.Error(), "at line 4") {
		t.Fatalf("Parse = %v", err)
	}
}
