package icsschedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// newParser returns a parser with fixed import dates, so expansion tests can pick any range.
func newParser(t *testing.T, from, to time.Time) *Parser {
	t.Helper()
	w := importplan.Window{From: from, To: to}
	return &Parser{window: func() importplan.Window { return w }}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

func parseWith(t *testing.T, p *Parser, src string) importplan.Parsed {
	t.Helper()
	got, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return got
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// eventLines returns the line of every BEGIN:VEVENT of a fixture (fixtures fold no BEGIN lines).
func eventLines(src string) []int {
	var out []int
	for i, l := range strings.Split(src, "\n") {
		if strings.TrimSuffix(l, "\r") == "BEGIN:VEVENT" {
			out = append(out, i+1)
		}
	}
	return out
}

// wantRow is an expected row: start in the zone of the content, and the length.
type wantRow struct {
	line  int
	key   string
	title string
	start time.Time
	dur   time.Duration
	zone  string
}

func checkRows(t *testing.T, got importplan.Parsed, want []wantRow) {
	t.Helper()
	if len(got.Errors) != 0 {
		t.Fatalf("row errors: %+v", got.Errors)
	}
	if len(got.Rows) != len(want) {
		for _, r := range got.Rows {
			t.Logf("row line %d key %s start %v", r.Line, r.Key, r.Content.Start)
		}
		t.Fatalf("%d rows, want %d", len(got.Rows), len(want))
	}
	for i, w := range want {
		g := got.Rows[i]
		c := lesson.Content{Title: w.title, Start: w.start, End: w.start.Add(w.dur), TimeZone: w.zone}
		if g.Line != w.line || g.Key != w.key || !g.Content.Equal(c) {
			t.Errorf("row %d = line %d key %q %+v\nwant      line %d key %q %+v", i, g.Line, g.Key, g.Content, w.line, w.key, c)
		}
		if err := g.Content.Validate(); err != nil {
			t.Errorf("row %d content invalid: %v", i, err)
		}
		if err := lesson.ValidateSourceUID(importplan.SourceICS.UID(g.Key)); err != nil {
			t.Errorf("row %d source uid invalid: %v", i, err)
		}
	}
}

func TestNew(t *testing.T) {
	if p, err := New(nil); p != nil || !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New(nil) = %v, %v", p, err)
	}
	if p, err := New(fixedClock(time.Now())); p == nil || err != nil {
		t.Fatalf("New = %v, %v", p, err)
	}
}

func TestWindow(t *testing.T) {
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	want := importplan.Window{From: day.Add(-24 * time.Hour), To: day.Add(-24*time.Hour + 366*24*time.Hour)}
	tests := []struct {
		name string
		now  time.Time
	}{
		{"midnight UTC", day},
		{"late in the UTC day", day.Add(23*time.Hour + 59*time.Minute)},
		{"another zone, same UTC day", time.Date(2026, 10, 6, 23, 30, 0, 0, zone(t, "America/New_York")).Add(-24 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Window(tt.now); !got.From.Equal(want.From) || !got.To.Equal(want.To) {
				t.Fatalf("Window(%v) = %v .. %v", tt.now, got.From, got.To)
			}
		})
	}
}

// Test step 2: a weekly recurring event expands into the right dates within the import bounds.
func TestWeeklyRecurringEventExpandsWithinBounds(t *testing.T) {
	msk := zone(t, "Europe/Moscow")
	const src = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n" +
		"UID:algebra@example.edu\r\nSUMMARY:Algebra\r\n" +
		"DTSTART;TZID=Europe/Moscow:20260901T090000\r\nDTEND;TZID=Europe/Moscow:20260901T103000\r\n" +
		"RRULE:FREQ=WEEKLY;BYDAY=TU,TH\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"

	// The bounds cut the open-ended series on both sides: 2026-10-06 00:00 to 2026-10-20 09:00 Moscow time.
	p := newParser(t, time.Date(2026, 10, 6, 0, 0, 0, 0, msk), time.Date(2026, 10, 20, 9, 0, 0, 0, msk))
	got := parseWith(t, p, src)

	var want []wantRow
	for _, day := range []int{6, 8, 13, 15} { // Tuesdays and Thursdays; the 20th at 09:00 is the excluded end
		s := time.Date(2026, 10, day, 9, 0, 0, 0, msk)
		want = append(want, wantRow{line: 3, key: "algebra@example.edu/" + s.UTC().Format(time.RFC3339),
			title: "Algebra", start: s, dur: 90 * time.Minute, zone: "Europe/Moscow"})
	}
	checkRows(t, got, want)
	if got.Rows[0].Key != "algebra@example.edu/2026-10-06T06:00:00Z" {
		t.Errorf("key = %q", got.Rows[0].Key)
	}
}

// Test step 2 with the import dates of New: they follow the clock, start the day before today (UTC) and last 366 days.
func TestWeeklyRecurringEventExpandsWithinClockWindow(t *testing.T) {
	msk := zone(t, "Europe/Moscow")
	const src = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n" +
		"UID:physics@example.edu\r\nSUMMARY:Physics\r\n" +
		"DTSTART;TZID=Europe/Moscow:20260101T020000\r\nDURATION:PT1H30M\r\n" +
		"RRULE:FREQ=WEEKLY;BYDAY=WE\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"

	// Thursday 2026-10-08 12:00 UTC: the window is 2026-10-07 00:00 UTC (03:00 Moscow) to 2027-10-08 00:00 UTC.
	p, err := New(fixedClock(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	got := parseWith(t, p, src)
	if len(got.Errors) != 0 {
		t.Fatalf("errors: %+v", got.Errors)
	}
	if w := got.Window; !w.From.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) || !w.To.Equal(time.Date(2027, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window = %v .. %v", w.From, w.To)
	}

	// Wednesdays at 02:00 Moscow (23:00 UTC the day before): Oct 7 is before the window, Oct 14 is the first one,
	// Oct 6 2027 the last.
	var want []time.Time
	for d := time.Date(2026, 10, 14, 2, 0, 0, 0, msk); d.Before(time.Date(2027, 10, 8, 3, 0, 0, 0, msk)); d = d.AddDate(0, 0, 7) {
		want = append(want, d)
	}
	if len(got.Rows) != len(want) || len(want) != 52 {
		t.Fatalf("%d rows, want %d", len(got.Rows), len(want))
	}
	for i, r := range got.Rows {
		if !r.Content.Start.Equal(want[i]) || r.Content.End.Sub(r.Content.Start) != 90*time.Minute || r.Content.TimeZone != "Europe/Moscow" ||
			r.Key != "physics@example.edu/"+want[i].UTC().Format(time.RFC3339) {
			t.Errorf("row %d = %s %+v, want start %v", i, r.Key, r.Content, want[i])
		}
	}
	plan, err := importplan.Build(importplan.SourceICS, got, nil)
	if err != nil || !plan.Valid() || len(plan.Added) != 52 {
		t.Fatalf("plan = %+v, %v", plan.Errors, err)
	}
}

func TestGoogleCalendarExport(t *testing.T) {
	msk, ber := zone(t, "Europe/Moscow"), zone(t, "Europe/Berlin")
	src := fixture(t, "google.ics")
	lines := eventLines(src)
	const math, phys, german = "5k2m9q0r1s2t3u4v5w6x7y8z9a@google.com", "1a2b3c4d5e6f7g8h9i0j@google.com", "9z8y7x6w5v4u3t2s1r0q@google.com"
	k := func(id string, t time.Time) string { return id + "/" + t.UTC().Format(time.RFC3339) }

	var want []wantRow
	for _, day := range []int{5, 7, 19, 21, 26, 28} { // the 12th is moved, the 14th is cancelled
		s := time.Date(2026, 10, day, 9, 0, 0, 0, msk)
		want = append(want, wantRow{lines[0], k(math, s), "Математический анализ", s, 90 * time.Minute, "Europe/Moscow"})
	}
	moved := time.Date(2026, 10, 12, 11, 0, 0, 0, msk)
	want = append(want, wantRow{lines[1], k(math, time.Date(2026, 10, 12, 9, 0, 0, 0, msk)),
		"Математический анализ (перенос)", moved, 90 * time.Minute, "Europe/Moscow"})
	// A single event in UTC is shown in the calendar zone X-WR-TIMEZONE; its key is the UID alone.
	consult := time.Date(2026, 10, 16, 15, 0, 0, 0, msk)
	want = append(want, wantRow{lines[3], phys, "Консультация по физике", consult, 90 * time.Minute, "Europe/Moscow"})
	// Berlin leaves summer time on 2026-10-25: the class stays at 18:00 local, which is an hour later in UTC.
	for _, day := range []int{6, 13, 20, 27} {
		s := time.Date(2026, 10, day, 18, 0, 0, 0, ber)
		want = append(want, wantRow{lines[5], k(german, s), "Немецкий язык A1 (онлайн-группа, Берлин)", s, 90 * time.Minute, "Europe/Berlin"})
	}

	p := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, msk), time.Date(2026, 11, 1, 0, 0, 0, 0, msk))
	for name, s := range map[string]string{"LF": src, "CRLF": strings.ReplaceAll(src, "\n", "\r\n")} {
		t.Run(name, func(t *testing.T) { checkRows(t, parseWith(t, p, s), want) })
	}
	if got := parseWith(t, p, src); got.Rows[len(got.Rows)-1].Content.Start.UTC().Hour() != 17 ||
		got.Rows[len(got.Rows)-2].Content.Start.UTC().Hour() != 16 {
		t.Errorf("DST change not followed: %v, %v", got.Rows[len(got.Rows)-2].Content.Start.UTC(), got.Rows[len(got.Rows)-1].Content.Start.UTC())
	}

	// The whole semester: UNTIL (Dec 25, 23:59:59 Moscow) and the EXDATE of Nov 4 apply, COUNT ends the Berlin series.
	sem := newParser(t, time.Date(2026, 9, 1, 0, 0, 0, 0, msk), time.Date(2027, 1, 1, 0, 0, 0, 0, msk))
	got := parseWith(t, sem, src)
	perLine := map[int]int{}
	for _, r := range got.Rows {
		perLine[r.Line]++
		if r.Content.Start.Equal(time.Date(2026, 11, 4, 9, 0, 0, 0, msk)) {
			t.Errorf("EXDATE occurrence imported: %+v", r)
		}
	}
	if len(got.Errors) != 0 || perLine[lines[0]] != 29 || perLine[lines[1]] != 1 || perLine[lines[3]] != 1 || perLine[lines[5]] != 6 {
		t.Fatalf("semester rows per event line = %v, errors %+v", perLine, got.Errors)
	}
	last := got.Rows[perLine[lines[0]]-1]
	if !last.Content.Start.Equal(time.Date(2026, 12, 23, 9, 0, 0, 0, msk)) {
		t.Errorf("last occurrence before UNTIL = %v", last.Content.Start)
	}
}

func TestUniversityExchangeExport(t *testing.T) {
	msk, ber := zone(t, "Europe/Moscow"), zone(t, "Europe/Berlin")
	src := fixture(t, "university_exchange.ics")
	lines := eventLines(src)
	p := newParser(t, time.Date(2026, 9, 1, 0, 0, 0, 0, msk), time.Date(2027, 1, 1, 0, 0, 0, 0, msk))
	got := parseWith(t, p, src)
	if len(got.Errors) != 0 {
		t.Fatalf("errors: %+v", got.Errors)
	}

	byLine := map[int][]importplan.Row{}
	for _, r := range got.Rows {
		byLine[r.Line] = append(byLine[r.Line], r)
	}

	// "Russian Standard Time" is Europe/Moscow; INTERVAL=2 is every other Tuesday until Dec 22 10:45 (inclusive).
	prog := byLine[lines[0]]
	wantProg := []time.Time{}
	for _, d := range [][2]int{{9, 1}, {9, 15}, {9, 29}, {10, 13}, {10, 27}, {11, 10}, {11, 24}, {12, 8}, {12, 22}} {
		wantProg = append(wantProg, time.Date(2026, time.Month(d[0]), d[1], 10, 45, 0, 0, msk))
	}
	if len(prog) != len(wantProg) {
		t.Fatalf("lecture: %d rows, want %d", len(prog), len(wantProg))
	}
	for i, r := range prog {
		c := r.Content
		if !c.Start.Equal(wantProg[i]) || c.End.Sub(c.Start) != 95*time.Minute || c.TimeZone != "Europe/Moscow" ||
			c.Title != "Программирование (лекция, верхняя неделя)" {
			t.Errorf("lecture %d = %+v", i, c)
		}
	}
	// The folded UID is unfolded into the key.
	if !strings.HasPrefix(prog[0].Key, "040000008200E00074C5B7101A82E0080000000010B2A5D8E1F0DC010000000000000000100000") ||
		!strings.HasSuffix(prog[0].Key, "/2026-09-01T07:45:00Z") {
		t.Errorf("key = %q", prog[0].Key)
	}

	// COUNT=16 Thursdays, two of them removed by one EXDATE with two values.
	db := byLine[lines[1]]
	if len(db) != 14 {
		t.Fatalf("practice: %d rows, want 14", len(db))
	}
	for _, r := range db {
		s := r.Content.Start.In(msk)
		if s.Weekday() != time.Thursday || s.Hour() != 14 || (s.Month() == 10 && s.Day() == 8) || (s.Month() == 11 && s.Day() == 5) {
			t.Errorf("practice occurrence %v", s)
		}
	}
	if s := db[len(db)-1].Content.Start; !s.Equal(time.Date(2026, 12, 17, 14, 0, 0, 0, msk)) {
		t.Errorf("16th occurrence = %v", s)
	}

	// "W. Europe Standard Time" is mapped to an IANA zone of Central Europe.
	guest := byLine[lines[2]]
	if len(guest) != 1 || !guest[0].Content.Start.Equal(time.Date(2026, 10, 20, 16, 0, 0, 0, ber)) || guest[0].Content.TimeZone != "Europe/Berlin" {
		t.Fatalf("guest lecture = %+v", guest)
	}
}

func TestUniversityPortalExport(t *testing.T) {
	yek := zone(t, "Asia/Yekaterinburg")
	src := fixture(t, "university_portal.ics")
	k := func(id string, t time.Time) string { return id + "/" + t.UTC().Format(time.RFC3339) }
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, yek) }
	pe := identity("", "Физическая культура")
	if !strings.HasPrefix(pe, idAutoPrefix) {
		t.Fatalf("identity without UID = %q", pe)
	}

	// Floating times are read in X-WR-TIMEZONE; line numbers count the folded line (the last event is on line 31).
	want := []wantRow{
		{7, "lesson-104512@schedule.example.edu", "Операционные системы (лекция); поток 1", at(5, 8, 30), 90 * time.Minute, "Asia/Yekaterinburg"},
		{16, "lesson-104513@schedule.example.edu", "Компьютерные сети (лабораторная работа, подгруппа 2, чётная неделя)", at(5, 10, 15), 90 * time.Minute, "Asia/Yekaterinburg"},
		{25, k(pe, at(6, 12, 0)), "Физическая культура", at(6, 12, 0), 90 * time.Minute, "Asia/Yekaterinburg"},
		{31, k("lesson-104520@schedule.example.edu", at(7, 8, 30)), "Интенсив: Python", at(7, 8, 30), 90 * time.Minute, "Asia/Yekaterinburg"},
		{31, k("lesson-104520@schedule.example.edu", at(8, 8, 30)), "Интенсив: Python", at(8, 8, 30), 90 * time.Minute, "Asia/Yekaterinburg"},
		{31, k("lesson-104520@schedule.example.edu", at(9, 8, 30)), "Интенсив: Python", at(9, 8, 30), 90 * time.Minute, "Asia/Yekaterinburg"},
	}
	p := newParser(t, at(1, 0, 0), at(31, 0, 0))
	checkRows(t, parseWith(t, p, src), want)

	// Without X-WR-TIMEZONE the floating times have no zone: every event is a row error, nothing is guessed.
	got := parseWith(t, p, strings.Replace(src, "X-WR-TIMEZONE:Asia/Yekaterinburg\n", "", 1))
	if len(got.Rows) != 0 || len(got.Errors) != 4 || got.Errors[0].Line != 6 || got.Errors[0].Field != propDTStart {
		t.Fatalf("floating without a calendar zone = %+v", got)
	}
}

func TestParseIsDeterministic(t *testing.T) {
	msk := zone(t, "Europe/Moscow")
	p := newParser(t, time.Date(2026, 9, 1, 0, 0, 0, 0, msk), time.Date(2027, 1, 1, 0, 0, 0, 0, msk))
	for _, name := range []string{"google.ics", "university_exchange.ics", "university_portal.ics"} {
		src := fixture(t, name)
		a, b := parseWith(t, p, src), parseWith(t, p, src)
		pa, err := importplan.Build(importplan.SourceICS, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		pb, err := importplan.Build(importplan.SourceICS, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !pa.Valid() || pa.Fingerprint == "" || pa.Fingerprint != pb.Fingerprint {
			t.Errorf("%s: plans differ or invalid: %+v", name, pa.Errors)
		}
	}
}

func TestParseStripsUTF8BOM(t *testing.T) {
	src := string([]byte{0xEF, 0xBB, 0xBF}) + "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:a\nSUMMARY:Math\n" +
		"DTSTART:20261005T060000Z\nDTEND:20261005T070000Z\nEND:VEVENT\nEND:VCALENDAR\n"
	p := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	got := parseWith(t, p, src)
	if len(got.Errors) != 0 || len(got.Rows) != 1 || got.Rows[0].Content.TimeZone != "UTC" || got.Rows[0].Line != 2 {
		t.Fatalf("parsed = %+v", got)
	}
}

func TestParseHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := newParser(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if _, err := p.Parse(ctx, []byte(fixture(t, "google.ics"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse with a cancelled context = %v", err)
	}
}
