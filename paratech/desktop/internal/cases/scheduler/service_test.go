package scheduler

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ Clock   = (*testenv.FakeClock)(nil)
	_ Sleeper = (*testenv.FakeClock)(nil)
)

// start of the lesson of most tests: 10:00 in Moscow.
var lessonStart = time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)

const (
	testLead = 10 * time.Minute
	testPoll = 30 * time.Second
)

func newService(t *testing.T, store *lessonStore, clock *testenv.FakeClock, events *eventLog) *Service {
	t.Helper()
	s, err := New(Deps{Lessons: store, Clock: clock, Sleeper: clock, Events: events},
		Config{Lead: testLead, PollInterval: testPoll})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// runFor runs s until the fake clock has advanced by d in poll steps, then stops it and checks that Run returned.
func runFor(t *testing.T, s *Service, clock *testenv.FakeClock, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	for passed := time.Duration(0); passed < d; passed += testPoll {
		if err := clock.AwaitSleepers(ctx, 1); err != nil {
			t.Fatalf("scheduler did not sleep: %v", err)
		}
		clock.Advance(testPoll)
	}
	if err := clock.AwaitSleepers(ctx, 1); err != nil {
		t.Fatalf("scheduler did not sleep: %v", err)
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

func TestNew(t *testing.T) {
	store, clock, events := newLessonStore(), testenv.NewFakeClock(lessonStart), &eventLog{}
	full := Deps{Lessons: store, Clock: clock, Sleeper: clock, Events: events}
	tests := []struct {
		name  string
		deps  func(Deps) Deps
		cfg   Config
		valid bool
	}{
		{"default config", func(d Deps) Deps { return d }, DefaultConfig(), true},
		{"zero lead", func(d Deps) Deps { return d }, Config{Lead: 0, PollInterval: time.Second}, true},
		{"max lead", func(d Deps) Deps { return d }, Config{Lead: lesson.MaxStartLead, PollInterval: time.Second}, true},
		{"lead too long", func(d Deps) Deps { return d }, Config{Lead: lesson.MaxStartLead + 1, PollInterval: time.Second}, false},
		{"negative lead", func(d Deps) Deps { return d }, Config{Lead: -time.Second, PollInterval: time.Second}, false},
		{"no poll interval", func(d Deps) Deps { return d }, Config{Lead: testLead}, false},
		{"no lessons", func(d Deps) Deps { d.Lessons = nil; return d }, DefaultConfig(), false},
		{"no clock", func(d Deps) Deps { d.Clock = nil; return d }, DefaultConfig(), false},
		{"no sleeper", func(d Deps) Deps { d.Sleeper = nil; return d }, DefaultConfig(), false},
		{"no events", func(d Deps) Deps { d.Events = nil; return d }, DefaultConfig(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.deps(full), tt.cfg)
			if tt.valid != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidConfig)) {
				t.Fatalf("New = %v, want valid %v", err, tt.valid)
			}
		})
	}
}

// Test step 2: polling from an hour before the start to after the end announces the lesson exactly once, within one
// poll interval after start-lead.
func TestRunAnnouncesExactlyOnce(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	store, events := newLessonStore(), &eventLog{}
	store.add(t, "math", lessonStart, 90*time.Minute, "Europe/Moscow")
	clock := testenv.NewFakeClock(lessonStart.Add(-time.Hour))

	runFor(t, newService(t, store, clock, events), clock, 3*time.Hour)

	got := events.all()
	if len(got) != 1 {
		t.Fatalf("events = %+v, want exactly one", got)
	}
	ev := got[0]
	if due := lessonStart.Add(-testLead); ev.AnnouncedAt.Before(due) || ev.AnnouncedAt.After(due.Add(testPoll)) {
		t.Fatalf("announced at %v, want within %v after %v", ev.AnnouncedAt, testPoll, due)
	}
	if ev.LessonID != "math" || ev.TimeZone != "Europe/Moscow" || !ev.Start.Equal(lessonStart) {
		t.Fatalf("event = %+v", ev)
	}
	if l := store.get(t, "math"); !l.StartNotifiedAt().Equal(ev.AnnouncedAt) {
		t.Fatalf("stored mark %v, want %v", l.StartNotifiedAt(), ev.AnnouncedAt)
	}
}

// Test step 3: a scheduler restarted during the lesson (a new Service on the same store) does not announce it again.
func TestRestartDuringLessonDoesNotDuplicate(t *testing.T) {
	tests := []struct {
		name      string
		firstRun  time.Duration // the first scheduler runs from start-lead for this long
		restartAt time.Duration // offset from the start where the second scheduler begins
	}{
		{"restart right after the announcement", time.Minute, -testLead + 2*time.Minute},
		{"restart at the start", 5 * time.Minute, 0},
		{"restart in the middle", 20 * time.Minute, 45 * time.Minute},
		{"restart just before the end", 20 * time.Minute, 89 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			store, events := newLessonStore(), &eventLog{}
			store.add(t, "math", lessonStart, 90*time.Minute, "Europe/Moscow")

			clock := testenv.NewFakeClock(lessonStart.Add(-testLead))
			runFor(t, newService(t, store, clock, events), clock, tt.firstRun)
			if n := len(events.all()); n != 1 {
				t.Fatalf("first run: %d events, want 1", n)
			}

			clock2 := testenv.NewFakeClock(lessonStart.Add(tt.restartAt))
			runFor(t, newService(t, store, clock2, events), clock2, 2*time.Hour)
			if n := len(events.all()); n != 1 {
				t.Fatalf("after restart: %d events, want still 1", n)
			}
		})
	}
}

// An app started in the middle of a lesson it has not announced yet still offers the prompt, once.
func TestStartInTheMiddleAnnouncesOnce(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	store, events := newLessonStore(), &eventLog{}
	store.add(t, "math", lessonStart, 90*time.Minute, "Europe/Moscow")
	clock := testenv.NewFakeClock(lessonStart.Add(40 * time.Minute))

	runFor(t, newService(t, store, clock, events), clock, time.Hour)

	if got := events.all(); len(got) != 1 || !got[0].AnnouncedAt.Equal(lessonStart.Add(40*time.Minute)) {
		t.Fatalf("events = %+v, want one at the first tick", got)
	}
}

func TestTick(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	tests := []struct {
		name  string
		setup func(t *testing.T, s *lessonStore)
		now   time.Time
		want  []shared.LessonID
	}{
		{
			name:  "nothing scheduled",
			setup: func(*testing.T, *lessonStore) {},
			now:   lessonStart,
		},
		{
			name: "too early",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
			},
			now: lessonStart.Add(-testLead - time.Second),
		},
		{
			name: "at start minus lead",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
			},
			now:  lessonStart.Add(-testLead),
			want: []shared.LessonID{"a"},
		},
		{
			name: "lesson over",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
			},
			now: lessonStart.Add(time.Hour),
		},
		{
			name: "long lesson still running",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, lesson.MaxDuration, "UTC")
			},
			now:  lessonStart.Add(lesson.MaxDuration - time.Second),
			want: []shared.LessonID{"a"},
		},
		{
			name: "deleted lesson",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
				s.update(t, "a", func(l *lesson.Lesson) error { return l.MarkDeleted(lessonStart.Add(-time.Hour)) })
			},
			now: lessonStart,
		},
		{
			name: "already announced",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
				s.update(t, "a", func(l *lesson.Lesson) error {
					_, err := l.AnnounceStart(lessonStart.Add(-testLead), testLead)
					return err
				})
			},
			now: lessonStart,
		},
		{
			name: "moved after the announcement is due again",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart.Add(-2*time.Hour), time.Hour, "UTC")
				s.update(t, "a", func(l *lesson.Lesson) error {
					if _, err := l.AnnounceStart(lessonStart.Add(-2*time.Hour), testLead); err != nil {
						return err
					}
					c := l.Content()
					c.Start, c.End = lessonStart, lessonStart.Add(time.Hour)
					_, err := l.ChangeContent(c, lessonStart.Add(-time.Hour))
					return err
				})
			},
			now:  lessonStart,
			want: []shared.LessonID{"a"},
		},
		{
			name: "several due, one not yet",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", lessonStart, time.Hour, "UTC")
				s.add(t, "b", lessonStart.Add(5*time.Minute), time.Hour, "Europe/Moscow")
				s.add(t, "c", lessonStart.Add(time.Hour), time.Hour, "UTC")
			},
			now:  lessonStart,
			want: []shared.LessonID{"a", "b"},
		},
		{
			// 10:00 in Moscow is 07:00 UTC; a clock that reads in another zone sees the same instant.
			name: "zone of the record and of the clock differ",
			setup: func(t *testing.T, s *lessonStore) {
				s.add(t, "a", time.Date(2026, 10, 14, 10, 0, 0, 0, moscow), time.Hour, "Europe/Moscow")
			},
			now:  time.Date(2026, 10, 14, 6, 50, 0, 0, time.UTC).In(time.FixedZone("PST", -8*3600)),
			want: []shared.LessonID{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, events := newLessonStore(), &eventLog{}
			tt.setup(t, store)
			s := newService(t, store, testenv.NewFakeClock(tt.now), events)

			n, err := s.Tick(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := events.ids(); n != len(tt.want) || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Tick = %d, events %v, want %v", n, got, tt.want)
			}

			// A second tick at the same time is silent.
			if n, err := s.Tick(context.Background()); n != 0 || err != nil || len(events.all()) != len(tt.want) {
				t.Fatalf("second Tick = %d, %v, events %v", n, err, events.ids())
			}
		})
	}
}

func TestTickFailures(t *testing.T) {
	errDisk := errors.New("disk failed")

	t.Run("list error", func(t *testing.T) {
		store, events := newLessonStore(), &eventLog{}
		store.listErr = errDisk
		_, err := newService(t, store, testenv.NewFakeClock(lessonStart), events).Tick(context.Background())
		if !errors.Is(err, errDisk) {
			t.Fatalf("Tick = %v, want %v", err, errDisk)
		}
	})

	t.Run("save error publishes nothing", func(t *testing.T) {
		store, events := newLessonStore(), &eventLog{}
		store.add(t, "a", lessonStart, time.Hour, "UTC")
		store.saveErr = errDisk
		s := newService(t, store, testenv.NewFakeClock(lessonStart), events)
		if n, err := s.Tick(context.Background()); n != 0 || !errors.Is(err, errDisk) {
			t.Fatalf("Tick = %d, %v, want 0, %v", n, err, errDisk)
		}
		if len(events.all()) != 0 || !store.get(t, "a").StartNotifiedAt().IsZero() {
			t.Fatal("an unsaved announcement was published or stored")
		}

		// The next tick retries.
		store.saveErr = nil
		if n, err := s.Tick(context.Background()); n != 1 || err != nil || len(events.all()) != 1 {
			t.Fatalf("retry Tick = %d, %v, events %v", n, err, events.ids())
		}
	})

	t.Run("version conflict is skipped and re-read", func(t *testing.T) {
		store, events := newLessonStore(), &eventLog{}
		store.add(t, "a", lessonStart, time.Hour, "UTC")
		store.beforeSave = func() {
			store.update(t, "a", func(l *lesson.Lesson) error {
				c := l.Content()
				c.Title = "Renamed"
				_, err := l.ChangeContent(c, lessonStart)
				return err
			})
		}
		s := newService(t, store, testenv.NewFakeClock(lessonStart), events)
		if n, err := s.Tick(context.Background()); n != 0 || err != nil || len(events.all()) != 0 {
			t.Fatalf("Tick = %d, %v, events %v; want a silent skip", n, err, events.ids())
		}
		if n, err := s.Tick(context.Background()); n != 1 || err != nil {
			t.Fatalf("next Tick = %d, %v", n, err)
		}
		if got := events.all(); len(got) != 1 || got[0].Title != "Renamed" {
			t.Fatalf("events = %+v, want the renamed lesson once", got)
		}
	})

	t.Run("publish error is reported and not repeated", func(t *testing.T) {
		store, events := newLessonStore(), &eventLog{err: errDisk}
		store.add(t, "a", lessonStart, time.Hour, "UTC")
		store.add(t, "b", lessonStart, time.Hour, "UTC")
		s := newService(t, store, testenv.NewFakeClock(lessonStart), events)
		if n, err := s.Tick(context.Background()); n != 2 || !errors.Is(err, errDisk) {
			t.Fatalf("Tick = %d, %v, want 2, %v", n, err, errDisk)
		}
		if n, err := s.Tick(context.Background()); n != 0 || err != nil || len(events.all()) != 2 {
			t.Fatalf("second Tick = %d, %v, events %v", n, err, events.ids())
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		store, events := newLessonStore(), &eventLog{}
		store.add(t, "a", lessonStart, time.Hour, "UTC")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		n, err := newService(t, store, testenv.NewFakeClock(lessonStart), events).Tick(ctx)
		if n != 0 || !errors.Is(err, context.Canceled) || len(events.all()) != 0 {
			t.Fatalf("Tick = %d, %v, events %v", n, err, events.ids())
		}
	})
}

// A failing tick does not stop Run; it tries again after the poll interval.
func TestRunKeepsPollingAfterErrors(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	store, events := newLessonStore(), &eventLog{}
	store.add(t, "a", lessonStart, time.Hour, "UTC")
	store.listErr = errors.New("busy")
	clock := testenv.NewFakeClock(lessonStart)
	s := newService(t, store, clock, events)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	if err := clock.AwaitSleepers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.listErr = nil
	store.mu.Unlock()
	clock.Advance(testPoll)
	if err := clock.AwaitSleepers(ctx, 1); err != nil {
		t.Fatal(err)
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if len(events.all()) != 1 {
		t.Fatalf("events = %v, want one after the store recovered", events.ids())
	}
}
