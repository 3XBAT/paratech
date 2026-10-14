package tests

// Scenario tests of the ICS schedule import (PRD section 11.2, AC-16) on the real SQLite adapter, the real ICS and
// CSV parsers and the real use case; the clock (also the clock of the import dates) and the ids are fakes (ARCH-61).

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/icsschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/testenv"
)

var _ schedulesync.ScheduleParser = (*icsschedule.Parser)(nil)

func newICSScheduleEnv(t *testing.T) *scheduleEnv {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &scheduleEnv{t: t, repo: sqlite.NewLessonRepository(db), clock: testenv.NewFakeClock(schedT0)}
	ics, err := icsschedule.New(e.clock)
	if err != nil {
		t.Fatal(err)
	}
	e.svc, err = schedulesync.New(schedulesync.Deps{
		Lessons: e.repo,
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{importplan.SourceCSV: csvschedule.New(), importplan.SourceICS: ics},
		Clock:   e.clock, IDs: &lessonIDGen{}, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *scheduleEnv) importAs(src importplan.Source, file string) (schedulesync.Preview, schedulesync.CommitResult) {
	e.t.Helper()
	ctx := context.Background()
	p, err := e.svc.PreviewImport(ctx, schedulesync.ImportRequest{Source: src, Data: []byte(file)})
	if err != nil || !p.Plan.Valid() {
		e.t.Fatalf("PreviewImport: %v, %+v", err, p.Plan.Errors)
	}
	e.clock.Advance(time.Minute)
	res, err := e.svc.CommitImport(ctx, schedulesync.CommitRequest{Source: src, Data: []byte(file), ConfirmToken: p.ConfirmToken})
	if err != nil {
		e.t.Fatalf("CommitImport: %v", err)
	}
	return p, res
}

// A weekly class on Tuesdays 09:00 Moscow until the end of the year, and one single consultation in UTC.
const icsSemester = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-TIMEZONE:Europe/Moscow\r\n" +
	"BEGIN:VEVENT\r\nUID:algebra@uni.example\r\nSUMMARY:Algebra\r\n" +
	"DTSTART;TZID=Europe/Moscow:20260901T090000\r\nDTEND;TZID=Europe/Moscow:20260901T103000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=TU;UNTIL=20261231T205959Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:consult@uni.example\r\nSUMMARY:Consultation\r\n" +
	"DTSTART:20261016T120000Z\r\nDTEND:20261016T130000Z\r\nEND:VEVENT\r\n" +
	"%EXTRA%END:VCALENDAR\r\n"

func icsFile(extra string) string { return strings.Replace(icsSemester, "%EXTRA%", extra, 1) }

func (e *scheduleEnv) live(prefix string) map[string]lessonRow {
	out := map[string]lessonRow{}
	for _, r := range e.schedule() {
		if !r.Deleted && strings.HasPrefix(r.UID, prefix) {
			out[r.UID] = r
		}
	}
	return out
}

func algebraUID(day time.Time) string {
	return "ics:algebra@uni.example/" + day.UTC().Format(time.RFC3339)
}

// Test step 2 end to end: the weekly event becomes one lesson per Tuesday within the import dates, and imports
// with a later clock neither duplicate nor remove the lessons already past.
func TestScheduleImportICSWeeklyEventWithinImportDates(t *testing.T) {
	e := newICSScheduleEnv(t)
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 9, 0, 0, 0, msk) }

	// A CSV lesson must stay untouched by every ICS import.
	e.importAs(importplan.SourceCSV, "title,date,start,end,timezone,uid\nTutor,2026-10-07,18:00,19:00,Europe/Moscow,tutor-1\n")

	// The clock is 2026-10-01 08:00 UTC: the import dates start on 2026-09-30, so September is not imported.
	p, res := e.importAs(importplan.SourceICS, icsFile(""))
	var want []time.Time
	for d := at(10, 6); d.Before(at(12, 30)); d = d.AddDate(0, 0, 7) {
		want = append(want, d)
	}
	if res.Added != len(want)+1 || len(want) != 13 || !p.Plan.Window.From.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first import = %+v, window %v", res, p.Plan.Window)
	}
	lessons := e.live("ics:")
	for _, d := range want {
		l, ok := lessons[algebraUID(d)]
		if !ok || l.Title != "Algebra" || !l.Start.Equal(d) || l.End.Sub(l.Start) != 90*time.Minute || l.Zone != "Europe/Moscow" {
			t.Errorf("lesson on %v = %+v (found %v)", d, l, ok)
		}
	}
	if c := lessons["ics:consult@uni.example"]; c.Zone != "Europe/Moscow" || c.Title != "Consultation" {
		t.Errorf("single UTC event = %+v", c)
	}

	// Two weeks later the window starts after Oct 6 and 13; the same file changes nothing and removes nothing.
	before := e.schedule()
	e.clock.Advance(14 * 24 * time.Hour)
	p, res = e.importAs(importplan.SourceICS, icsFile(""))
	if res != (schedulesync.CommitResult{}) || len(p.Plan.Unchanged) != len(want)-2+1 {
		t.Fatalf("re-import = %+v, unchanged %d", res, len(p.Plan.Unchanged))
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatal("re-import with a later clock changed the schedule")
	}

	// The teacher cancels Nov 3 (EXDATE) and moves Nov 10 to 11:00 (RECURRENCE-ID): the cancelled occurrence is
	// removed, the moved one changed in place.
	moved := "BEGIN:VEVENT\r\nUID:algebra@uni.example\r\nSUMMARY:Algebra (moved)\r\n" +
		"RECURRENCE-ID;TZID=Europe/Moscow:20261110T090000\r\n" +
		"DTSTART;TZID=Europe/Moscow:20261110T110000\r\nDTEND;TZID=Europe/Moscow:20261110T123000\r\nEND:VEVENT\r\n"
	next := strings.Replace(icsFile(moved), "RRULE:", "EXDATE;TZID=Europe/Moscow:20261103T090000\r\nRRULE:", 1)
	movedID := lessons[algebraUID(at(11, 10))].ID
	p, res = e.importAs(importplan.SourceICS, next)
	if res != (schedulesync.CommitResult{Changed: 1, Removed: 1}) || p.Plan.Removed[0].UID != algebraUID(at(11, 3)) {
		t.Fatalf("change import = %+v, removed %+v", res, p.Plan.Removed)
	}
	after := e.live("ics:")
	if _, ok := after[algebraUID(at(11, 3))]; ok {
		t.Error("the cancelled occurrence is still live")
	}
	if m := after[algebraUID(at(11, 10))]; m.ID != movedID || m.Title != "Algebra (moved)" ||
		!m.Start.Equal(time.Date(2026, 11, 10, 11, 0, 0, 0, msk)) || m.Revision != 2 {
		t.Errorf("moved occurrence = %+v", m)
	}
	for _, d := range []time.Time{at(10, 6), at(10, 13)} {
		if _, ok := after[algebraUID(d)]; !ok {
			t.Errorf("past lesson %v outside the import dates was removed", d)
		}
	}
	if csv := e.live("csv:"); len(csv) != 1 {
		t.Errorf("CSV lessons = %+v", csv)
	}
}
