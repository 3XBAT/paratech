package tests

// Scenario test of the "lesson is starting" scheduler (PRD section 6.1, AC-01) on the real SQLite adapter, with the
// schedule imported through the real CSV parser and import use case; only the clock is a fake (ARCH-61).

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/testenv"
)

var _ scheduler.Lessons = (*sqlite.LessonRepository)(nil)

// Both lessons start on the day daylight saving ends in their zone: 09:00 New York is 14:00 UTC on 2026-11-01 (EST),
// 09:00 London is 09:00 UTC on 2026-10-25 (GMT).
const dstSchedule = `title,date,start,end,timezone,uid
History,2026-11-01,09:00,10:00,America/New_York,ny-1
English,2026-10-25,09:00,10:30,Europe/London,uk-1
`

type startEvents struct {
	mu     sync.Mutex
	events []lesson.StartAnnounced
}

func (e *startEvents) LessonStarting(_ context.Context, ev lesson.StartAnnounced) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *startEvents) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

func openScheduleDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSchedulerAnnouncesInRecordZoneOnceAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db := openScheduleDB(t, path)
	t.Cleanup(func() { _ = db.Close() }) // a second Close after the restart below is harmless
	clock := testenv.NewFakeClock(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))

	imp, err := schedulesync.New(schedulesync.Deps{
		Lessons: sqlite.NewLessonRepository(db),
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{importplan.SourceCSV: csvschedule.New()},
		Clock:   clock, IDs: &lessonIDGen{}, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := imp.PreviewImport(context.Background(), schedulesync.ImportRequest{Source: importplan.SourceCSV, Data: []byte(dstSchedule)})
	if err != nil || !p.Plan.Valid() {
		t.Fatalf("preview: %v %+v", err, p.Plan.Errors)
	}
	if _, err := imp.CommitImport(context.Background(), schedulesync.CommitRequest{
		Source: importplan.SourceCSV, Data: []byte(dstSchedule), ConfirmToken: p.ConfirmToken,
	}); err != nil {
		t.Fatal(err)
	}

	events := &startEvents{}
	newScheduler := func(db *sqlite.DB, clock *testenv.FakeClock) *scheduler.Service {
		s, err := scheduler.New(scheduler.Deps{
			Lessons: sqlite.NewLessonRepository(db), Clock: clock, Sleeper: clock, Events: events,
		}, scheduler.Config{Lead: 10 * time.Minute, PollInterval: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	tick := func(s *scheduler.Service, clock *testenv.FakeClock, at time.Time, want int) {
		t.Helper()
		clock.Advance(at.Sub(clock.Now()))
		if n, err := s.Tick(context.Background()); n != want || err != nil {
			t.Fatalf("Tick at %v = %d, %v; want %d", at, n, err, want)
		}
	}

	s := newScheduler(db, clock)
	tick(s, clock, time.Date(2026, 10, 25, 7, 50, 0, 0, time.UTC), 0) // 08:50 London summer time: an hour early
	tick(s, clock, time.Date(2026, 10, 25, 8, 50, 0, 0, time.UTC), 1) // 08:50 GMT
	tick(s, clock, time.Date(2026, 11, 1, 12, 50, 0, 0, time.UTC), 0) // 08:50 New York summer time: an hour early
	tick(s, clock, time.Date(2026, 11, 1, 13, 50, 0, 0, time.UTC), 1) // 08:50 EST
	tick(s, clock, time.Date(2026, 11, 1, 13, 55, 0, 0, time.UTC), 0)

	// A real restart in the middle of the New York lesson: the database is closed and opened again.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openScheduleDB(t, path)
	t.Cleanup(func() { _ = db2.Close() })
	clock2 := testenv.NewFakeClock(time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC))
	s2 := newScheduler(db2, clock2)
	tick(s2, clock2, time.Date(2026, 11, 1, 14, 30, 0, 0, time.UTC), 0)
	tick(s2, clock2, time.Date(2026, 11, 1, 14, 59, 0, 0, time.UTC), 0)

	if n := events.count(); n != 2 {
		t.Fatalf("%d events, want 2 (one per lesson)", n)
	}
	for _, ev := range events.events {
		want := map[string]time.Time{
			"America/New_York": time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC),
			"Europe/London":    time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC),
		}[ev.TimeZone]
		if !ev.Start.Equal(want) {
			t.Fatalf("event %+v: start %v, want %v", ev, ev.Start, want)
		}
	}
}
