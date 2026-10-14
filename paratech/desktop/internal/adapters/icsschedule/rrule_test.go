package icsschedule

import (
	"strings"
	"testing"
	"time"
)

// calendar wraps event property lines into a calendar with one VEVENT (on line 3) and extra raw text after it.
func calendar(header string, props []string, extra string) string {
	b := strings.Builder{}
	b.WriteString("BEGIN:VCALENDAR\nVERSION:2.0\n")
	if header != "" {
		b.WriteString(header + "\n")
	}
	b.WriteString("BEGIN:VEVENT\nUID:ev-1\nSUMMARY:Class\n")
	for _, p := range props {
		b.WriteString(p + "\n")
	}
	b.WriteString("END:VEVENT\n" + extra + "END:VCALENDAR\n")
	return b.String()
}

func TestRecurrenceExpansion(t *testing.T) {
	const layout = "2006-01-02 15:04"
	tests := []struct {
		name     string
		zone     string
		start    string // DTSTART line
		rule     string // RRULE value; "" for none
		extra    []string
		from, to string // bounds in the zone, layout
		want     []string
	}{
		{name: "single event", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261005T090000",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-05 09:00"}},
		{name: "single event before the bounds", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20260905T090000",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: nil},
		{name: "single event at the end bound", zone: "UTC", start: "DTSTART:20261101T000000Z",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: nil},
		{name: "daily count", zone: "UTC", start: "DTSTART:20261005T080000Z", rule: "FREQ=DAILY;COUNT=3",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-05 08:00", "2026-10-06 08:00", "2026-10-07 08:00"}},
		{name: "daily on weekdays", zone: "UTC", start: "DTSTART:20261009T080000Z", rule: "FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR",
			from: "2026-10-01 00:00", to: "2026-10-14 00:00", want: []string{"2026-10-09 08:00", "2026-10-12 08:00", "2026-10-13 08:00"}},
		{name: "daily interval and count", zone: "UTC", start: "DTSTART:20261001T080000Z", rule: "FREQ=DAILY;INTERVAL=10;COUNT=3",
			from: "2026-10-05 00:00", to: "2027-01-01 00:00", want: []string{"2026-10-11 08:00", "2026-10-21 08:00"}},
		{name: "weekly default day", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261006T090000", rule: "FREQ=WEEKLY",
			from: "2026-10-01 00:00", to: "2026-10-21 00:00", want: []string{"2026-10-06 09:00", "2026-10-13 09:00", "2026-10-20 09:00"}},
		{name: "weekly until a UTC instant is inclusive", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261006T090000",
			rule: "FREQ=WEEKLY;UNTIL=20261020T060000Z", from: "2026-10-01 00:00", to: "2026-12-01 00:00",
			want: []string{"2026-10-06 09:00", "2026-10-13 09:00", "2026-10-20 09:00"}},
		{name: "weekly until a date", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261006T090000",
			rule: "FREQ=WEEKLY;UNTIL=20261013", from: "2026-10-01 00:00", to: "2026-12-01 00:00",
			want: []string{"2026-10-06 09:00", "2026-10-13 09:00"}},
		{name: "weekly until a floating time", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261006T090000",
			rule: "FREQ=WEEKLY;UNTIL=20261013T085959", from: "2026-10-01 00:00", to: "2026-12-01 00:00",
			want: []string{"2026-10-06 09:00"}},
		{name: "weekly until before the bounds", zone: "UTC", start: "DTSTART:20250901T080000Z",
			rule: "FREQ=WEEKLY;UNTIL=20251201T000000Z;BYMONTHDAY=1", from: "2026-10-01 00:00", to: "2026-12-01 00:00", want: nil},
		{name: "every other week, two days", zone: "UTC", start: "DTSTART:20261005T080000Z", rule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,FR",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00",
			want: []string{"2026-10-05 08:00", "2026-10-09 08:00", "2026-10-19 08:00", "2026-10-23 08:00"}},
		// RFC 5545 example: WKST changes which weeks INTERVAL=2 picks.
		{name: "wkst monday", zone: "UTC", start: "DTSTART:19970805T090000Z", rule: "FREQ=WEEKLY;INTERVAL=2;COUNT=4;BYDAY=TU,SU;WKST=MO",
			from: "1997-08-01 00:00", to: "1997-09-01 00:00",
			want: []string{"1997-08-05 09:00", "1997-08-10 09:00", "1997-08-19 09:00", "1997-08-24 09:00"}},
		{name: "wkst sunday", zone: "UTC", start: "DTSTART:19970805T090000Z", rule: "FREQ=WEEKLY;INTERVAL=2;COUNT=4;BYDAY=TU,SU;WKST=SU",
			from: "1997-08-01 00:00", to: "1997-09-01 00:00",
			want: []string{"1997-08-05 09:00", "1997-08-17 09:00", "1997-08-19 09:00", "1997-08-31 09:00"}},
		{name: "dtstart off the rule is the first occurrence", zone: "UTC", start: "DTSTART:20261007T080000Z",
			rule: "FREQ=WEEKLY;COUNT=3;BYDAY=MO", from: "2026-10-01 00:00", to: "2026-11-01 00:00",
			want: []string{"2026-10-07 08:00", "2026-10-12 08:00", "2026-10-19 08:00"}},
		{name: "old open series jumps to the bounds", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:19900102T090000",
			rule: "FREQ=WEEKLY;BYDAY=TU", from: "2026-10-01 00:00", to: "2026-10-14 00:00",
			want: []string{"2026-10-06 09:00", "2026-10-13 09:00"}},
		{name: "monthly skips short months", zone: "UTC", start: "DTSTART:20260131T080000Z", rule: "FREQ=MONTHLY;COUNT=4",
			from: "2026-01-01 00:00", to: "2027-01-01 00:00",
			want: []string{"2026-01-31 08:00", "2026-03-31 08:00", "2026-05-31 08:00", "2026-07-31 08:00"}},
		{name: "yearly on february 29", zone: "UTC", start: "DTSTART:20240229T080000Z", rule: "FREQ=YEARLY",
			from: "2024-01-01 00:00", to: "2033-01-01 00:00", want: []string{"2024-02-29 08:00", "2028-02-29 08:00", "2032-02-29 08:00"}},
		// Wall-clock time is kept across the end of summer time in Berlin (2026-10-25).
		{name: "dst keeps local time", zone: "Europe/Berlin", start: "DTSTART;TZID=Europe/Berlin:20261024T090000", rule: "FREQ=DAILY;COUNT=3",
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-24 09:00", "2026-10-25 09:00", "2026-10-26 09:00"}},
		{name: "exdate with tzid and several values", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261005T090000",
			rule: "FREQ=DAILY;COUNT=5", extra: []string{"EXDATE;TZID=Europe/Moscow:20261006T090000,20261008T090000"},
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-05 09:00", "2026-10-07 09:00", "2026-10-09 09:00"}},
		{name: "exdate in UTC and as a date", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261005T090000",
			rule: "FREQ=DAILY;COUNT=4", extra: []string{"EXDATE:20261005T060000Z", "EXDATE;VALUE=DATE:20261007"},
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-06 09:00", "2026-10-08 09:00"}},
		{name: "rdate adds an occurrence", zone: "Europe/Moscow", start: "DTSTART;TZID=Europe/Moscow:20261005T090000",
			rule: "FREQ=WEEKLY;COUNT=2", extra: []string{"RDATE;TZID=Europe/Moscow:20261007T120000,20261012T090000"},
			from: "2026-10-01 00:00", to: "2026-11-01 00:00", want: []string{"2026-10-05 09:00", "2026-10-07 12:00", "2026-10-12 09:00"}},
		{name: "dst gap outside the bounds is ignored", zone: "Europe/Berlin", start: "DTSTART;TZID=Europe/Berlin:20260327T023000",
			rule: "FREQ=DAILY", from: "2026-10-01 00:00", to: "2026-10-03 00:00", want: []string{"2026-10-01 02:30", "2026-10-02 02:30"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := zone(t, tt.zone)
			props := []string{tt.start, "DURATION:PT1H"}
			if tt.rule != "" {
				props = append(props, "RRULE:"+tt.rule)
			}
			props = append(props, tt.extra...)
			from, err := time.ParseInLocation(layout, tt.from, loc)
			if err != nil {
				t.Fatal(err)
			}
			to, err := time.ParseInLocation(layout, tt.to, loc)
			if err != nil {
				t.Fatal(err)
			}

			got := parseWith(t, newParser(t, from, to), calendar("", props, ""))
			if len(got.Errors) != 0 {
				t.Fatalf("errors: %+v", got.Errors)
			}
			var starts []string
			for _, r := range got.Rows {
				starts = append(starts, r.Content.Start.In(loc).Format(layout))
				if r.Content.End.Sub(r.Content.Start) != time.Hour || r.Line != 3 {
					t.Errorf("row = %+v", r)
				}
			}
			if strings.Join(starts, ", ") != strings.Join(tt.want, ", ") {
				t.Fatalf("starts = %v\nwant     %v", starts, tt.want)
			}
		})
	}
}

func TestOverrides(t *testing.T) {
	msk := zone(t, "Europe/Moscow")
	at := func(day, h int) time.Time { return time.Date(2026, 10, day, h, 0, 0, 0, msk) }
	master := []string{"DTSTART;TZID=Europe/Moscow:20261005T090000", "DTEND;TZID=Europe/Moscow:20261005T100000", "RRULE:FREQ=DAILY;COUNT=4"}
	override := func(recur, start string, more ...string) string {
		return "BEGIN:VEVENT\nUID:ev-1\nSUMMARY:Moved\nRECURRENCE-ID;TZID=Europe/Moscow:" + recur + "\n" +
			"DTSTART;TZID=Europe/Moscow:" + start + "\nDURATION:PT2H\n" + strings.Join(more, "\n") + "\nEND:VEVENT\n"
	}
	p := newParser(t, at(1, 0), at(9, 0))

	// Moved: the override row replaces the generated one and keeps its key.
	got := parseWith(t, p, calendar("", master, override("20261006T090000", "20261007T150000")))
	if len(got.Errors) != 0 || len(got.Rows) != 4 {
		t.Fatalf("moved = %+v", got)
	}
	last := got.Rows[3]
	if last.Line != 10 || last.Key != "ev-1/2026-10-06T06:00:00Z" || !last.Content.Start.Equal(at(7, 15)) ||
		last.Content.End.Sub(last.Content.Start) != 2*time.Hour || last.Content.Title != "Moved" {
		t.Fatalf("override row = %+v", last)
	}
	for _, r := range got.Rows[:3] {
		if r.Content.Start.Equal(at(6, 9)) {
			t.Fatal("the replaced occurrence is still there")
		}
	}

	// Moved out of the bounds: the occurrence disappears; moved in from outside: it appears.
	got = parseWith(t, p, calendar("", master, override("20261006T090000", "20261020T090000")+override("20261008T090000", "20261001T090000")))
	if len(got.Errors) != 0 || len(got.Rows) != 3 || !got.Rows[2].Content.Start.Equal(at(1, 9)) || got.Rows[2].Key != "ev-1/2026-10-08T06:00:00Z" {
		t.Fatalf("moved across the bounds = %+v", got.Rows)
	}

	// A cancelled override removes its occurrence; a cancelled master removes the whole series and its overrides.
	got = parseWith(t, p, calendar("", master, override("20261006T090000", "20261006T090000", "STATUS:CANCELLED")))
	if len(got.Errors) != 0 || len(got.Rows) != 3 {
		t.Fatalf("cancelled occurrence = %+v", got)
	}
	got = parseWith(t, p, calendar("", append([]string{"STATUS:CANCELLED"}, master...), override("20261006T090000", "20261007T150000")))
	if len(got.Errors) != 0 || len(got.Rows) != 0 {
		t.Fatalf("cancelled series = %+v", got)
	}

	// An override without its master (the series is not in the file) is a lesson of its own.
	got = parseWith(t, p, "BEGIN:VCALENDAR\n"+override("20261006T090000", "20261007T150000")+"END:VCALENDAR\n")
	if len(got.Errors) != 0 || len(got.Rows) != 1 || got.Rows[0].Key != "ev-1/2026-10-06T06:00:00Z" {
		t.Fatalf("orphan override = %+v", got)
	}
}

// RDATE and EXDATE are matched through sets: an event with the most values allowed parses quickly.
func TestManyDatesInOneEvent(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// RDATEs at hours 1..5000, EXDATEs at the even hours 0..9998: DTSTART (hour 0) and the even RDATEs go.
	src := calendar("", []string{"DTSTART:20261001T000000Z", "DURATION:PT30M",
		strings.TrimSuffix(dateLines("RDATE", maxDatesPerEvent, day.Add(time.Hour), time.Hour), "\n"),
		strings.TrimSuffix(dateLines("EXDATE", maxDatesPerEvent, day, 2*time.Hour), "\n")}, "")
	began := time.Now()
	got := parseWith(t, newParser(t, day, day.AddDate(1, 0, 0)), src)
	if elapsed := time.Since(began); elapsed > 10*time.Second {
		t.Fatalf("took %v", elapsed)
	}
	if len(got.Errors) != 0 || len(got.Rows) != maxDatesPerEvent/2 {
		t.Fatalf("%d rows, errors %+v", len(got.Rows), got.Errors)
	}
	for _, r := range got.Rows {
		if r.Content.Start.Sub(day)/time.Hour%2 != 1 {
			t.Fatalf("excluded occurrence imported: %v", r.Content.Start)
		}
	}
}

// A date-only EXDATE is a date in the shown zone: here Moscow, while the series recurs in UTC.
func TestDateExdateOfUTCSeries(t *testing.T) {
	msk := zone(t, "Europe/Moscow")
	src := calendar("X-WR-TIMEZONE:Europe/Moscow",
		[]string{"DTSTART:20261005T220000Z", "DURATION:PT1H", "RRULE:FREQ=DAILY;COUNT=3", "EXDATE;VALUE=DATE:20261006"}, "")
	got := parseWith(t, newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)), src)
	var starts []string
	for _, r := range got.Rows {
		starts = append(starts, r.Content.Start.In(msk).Format("2006-01-02 15:04"))
	}
	if len(got.Errors) != 0 || strings.Join(starts, ", ") != "2026-10-07 01:00, 2026-10-08 01:00" {
		t.Fatalf("starts = %v, errors %+v", starts, got.Errors)
	}
}

func TestRowKeys(t *testing.T) {
	const start, moved = "DTSTART:20261005T060000Z", "DTSTART:20261009T060000Z"
	const occ = "/2026-10-05T06:00:00Z"
	override := "BEGIN:VEVENT\nUID:ev-1\nRECURRENCE-ID:20261005T060000Z\nDTSTART:20261005T060000Z\nSTATUS:CANCELLED\nEND:VEVENT\n"
	noUID := func(dtstart string) string {
		return "BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Class\n" + dtstart + "\nDURATION:PT1H\nEND:VEVENT\nEND:VCALENDAR\n"
	}
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"single event", calendar("", []string{start, "DURATION:PT1H"}, ""), []string{"ev-1"}},
		{"moved single event keeps its key", calendar("", []string{moved, "DURATION:PT1H"}, ""), []string{"ev-1"}},
		{"single event with exdate", calendar("", []string{start, "DURATION:PT1H", "EXDATE:20261006T060000Z"}, ""), []string{"ev-1"}},
		{"series", calendar("", []string{start, "DURATION:PT1H", "RRULE:FREQ=DAILY;COUNT=2"}, ""),
			[]string{"ev-1" + occ, "ev-1/2026-10-06T06:00:00Z"}},
		{"rdate makes a series", calendar("", []string{start, "DURATION:PT1H", "RDATE:20261007T060000Z"}, ""),
			[]string{"ev-1" + occ, "ev-1/2026-10-07T06:00:00Z"}},
		{"override makes a series", calendar("", []string{moved, "DURATION:PT1H"}, override), []string{"ev-1/2026-10-09T06:00:00Z"}},
		{"no uid keeps the start", noUID(start), []string{identity("", "Class") + occ}},
	}
	p := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWith(t, p, tt.src)
			var keys []string
			for _, r := range got.Rows {
				keys = append(keys, r.Key)
			}
			if len(got.Errors) != 0 || strings.Join(keys, " ") != strings.Join(tt.want, " ") {
				t.Fatalf("keys = %v, errors %+v; want %v", keys, got.Errors, tt.want)
			}
		})
	}

	// A UID with '/' is hashed, so a single event cannot take the key of an occurrence of another series.
	src := "BEGIN:VCALENDAR\n" +
		"BEGIN:VEVENT\nUID:x" + occ + "\nSUMMARY:A\n" + start + "\nDURATION:PT1H\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:x\nSUMMARY:B\n" + start + "\nDURATION:PT1H\nRRULE:FREQ=DAILY;COUNT=1\nEND:VEVENT\n" +
		"END:VCALENDAR\n"
	got := parseWith(t, p, src)
	if len(got.Rows) != 2 || got.Rows[1].Key != "x"+occ || got.Rows[0].Key == got.Rows[1].Key ||
		!strings.HasPrefix(got.Rows[0].Key, idHashPrefix) {
		t.Fatalf("rows = %+v", got.Rows)
	}
}
