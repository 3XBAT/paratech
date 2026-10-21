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
	db, err := openSQLite(context.Background(), t, sqlite.Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// importSchedule imports dstSchedule into db through the real CSV parser and import use case.
func importSchedule(t *testing.T, db *sqlite.DB, clock *testenv.FakeClock) {
	t.Helper()
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
}

func newSQLiteScheduler(t *testing.T, db *sqlite.DB, clock *testenv.FakeClock, events *startEvents) *scheduler.Service {
	t.Helper()
	s, err := scheduler.New(scheduler.Deps{
		Lessons: sqlite.NewLessonRepository(db), Clock: clock, Sleeper: clock, Events: events,
	}, scheduler.Config{Lead: 10 * time.Minute, PollInterval: scheduler.DefaultPollInterval})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tickAt moves clock to at, ticks s once and checks how many lessons it announced.
func tickAt(t *testing.T, s *scheduler.Service, clock *testenv.FakeClock, at time.Time, want int) {
	t.Helper()
	clock.Advance(at.Sub(clock.Now()))
	if n, err := s.Tick(context.Background()); n != want || err != nil {
		t.Fatalf("Tick at %v = %d, %v; want %d", at, n, err, want)
	}
}

// The prompt window is [start-lead, start+lesson.StartLateGrace): an app that first looks at the schedule at the
// given moment prompts once or not at all, and a second look right after stays silent.
func TestSchedulerPromptWindowOnSQLite(t *testing.T) {
	london := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   time.Time
		want int
	}{
		{"at start minus lead", london.Add(-10 * time.Minute), 1},
		{"at start", london, 1},
		{"19 s after start", london.Add(19 * time.Second), 1},
		{"21 s after start", london.Add(21 * time.Second), 0},
		{"mid-lesson", london.Add(45 * time.Minute), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openScheduleDB(t, filepath.Join(t.TempDir(), "app.db"))
			t.Cleanup(func() { _ = db.Close() })
			clock := testenv.NewFakeClock(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			importSchedule(t, db, clock)

			events := &startEvents{}
			s := newSQLiteScheduler(t, db, clock, events)
			tickAt(t, s, clock, tt.at, tt.want)
			tickAt(t, s, clock, tt.at.Add(time.Second), 0)
			if n := events.count(); n != tt.want {
				t.Fatalf("%d events, want %d", n, tt.want)
			}
		})
	}
}

func TestSchedulerAnnouncesInRecordZoneOnceAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db := openScheduleDB(t, path)
	t.Cleanup(func() { _ = db.Close() }) // a second Close after the restart below is harmless
	clock := testenv.NewFakeClock(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	importSchedule(t, db, clock)

	events := &startEvents{}
	newScheduler := func(db *sqlite.DB, clock *testenv.FakeClock) *scheduler.Service {
		return newSQLiteScheduler(t, db, clock, events)
	}
	tick := func(s *scheduler.Service, clock *testenv.FakeClock, at time.Time, want int) {
		t.Helper()
		tickAt(t, s, clock, at, want)
	}

	s := newScheduler(db, clock)
	tick(s, clock, time.Date(2026, 10, 25, 7, 50, 0, 0, time.UTC), 0) // 08:50 London summer time: an hour early
	tick(s, clock, time.Date(2026, 10, 25, 8, 50, 0, 0, time.UTC), 1) // 08:50 GMT
	tick(s, clock, time.Date(2026, 11, 1, 12, 50, 0, 0, time.UTC), 0) // 08:50 New York summer time: an hour early
	tick(s, clock, time.Date(2026, 11, 1, 13, 50, 0, 0, time.UTC), 1) // 08:50 EST
	tick(s, clock, time.Date(2026, 11, 1, 13, 55, 0, 0, time.UTC), 0)

	// A real restart at the start of the New York lesson, inside the late grace: the database is closed and opened
	// again, and the stored mark keeps the restarted scheduler silent.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openScheduleDB(t, path)
	t.Cleanup(func() { _ = db2.Close() })
	clock2 := testenv.NewFakeClock(time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC))
	s2 := newScheduler(db2, clock2)
	tick(s2, clock2, time.Date(2026, 11, 1, 14, 0, 10, 0, time.UTC), 0)
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
