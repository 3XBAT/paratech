package tests

// Scenario test of the "lesson is starting" popup (PRD section 6.1, AC-01, AC-02) on the real SQLite adapter: the
// schedule comes through the real CSV import, the setting through the real preferences use case, and the scheduler
// feeds the real recording prompt; only the clock, the popup window and the recording session are fakes (ARCH-61).

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var _ recordingprompt.SettingsReader = (*sqlite.SettingsRepository)(nil)

// London lesson of dstSchedule: 09:00-10:30 UTC on 2026-10-25.
var londonStart = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

// startedLessons stands for the recording session behind the prompt.
type startedLessons struct {
	mu      sync.Mutex
	lessons []shared.LessonID
}

func (s *startedLessons) Ready() bool { return true }

func (s *startedLessons) StartRecording(_ context.Context, l shared.LessonID) (shared.RecordingID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lessons = append(s.lessons, l)
	return "rec-1", nil
}

func (s *startedLessons) all() []shared.LessonID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lessons)
}

type promptScenario struct {
	t          *testing.T
	clock      *testenv.FakeClock
	popup      *testenv.FakePopup
	prefs      *preferences.Service
	prompts    *recordingprompt.Service
	recordings *startedLessons
	sched      *scheduler.Service
}

func newPromptScenario(t *testing.T) *promptScenario {
	t.Helper()
	db := openScheduleDB(t, filepath.Join(t.TempDir(), "app.db"))
	t.Cleanup(func() { _ = db.Close() })
	clock := testenv.NewFakeClock(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
	importSchedule(t, db, clock)

	settings := sqlite.NewSettingsRepository(db)
	prefs, err := preferences.New(settings, clock)
	if err != nil {
		t.Fatal(err)
	}
	sc := &promptScenario{t: t, clock: clock, popup: &testenv.FakePopup{}, prefs: prefs, recordings: &startedLessons{}}
	sc.prompts, err = recordingprompt.New(recordingprompt.Deps{
		Settings: settings, Popup: sc.popup, Recordings: sc.recordings, Clock: clock, Sleeper: clock,
	}, recordingprompt.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	sc.sched, err = scheduler.New(scheduler.Deps{
		Lessons: sqlite.NewLessonRepository(db), Clock: clock, Sleeper: clock, Events: sc.prompts,
	}, scheduler.Config{Lead: 10 * time.Minute, PollInterval: scheduler.DefaultPollInterval})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// tickAt moves the clock to at and runs one scheduler tick and one prompt check.
func (sc *promptScenario) tickAt(at time.Time) {
	sc.t.Helper()
	sc.clock.Advance(at.Sub(sc.clock.Now()))
	if _, err := sc.sched.Tick(context.Background()); err != nil {
		sc.t.Fatal(err)
	}
	if err := sc.prompts.Tick(context.Background()); err != nil {
		sc.t.Fatal(err)
	}
}

func (sc *promptScenario) setPrompts(on bool) {
	sc.t.Helper()
	if _, err := sc.prefs.SetRecordingPromptsEnabled(context.Background(), on); err != nil {
		sc.t.Fatal(err)
	}
}

// Test step 2 on SQLite (AC-01, AC-02): with prompts on the lesson start shows the popup and "Start recording"
// records that lesson; with prompts off nothing is shown, and turning them back on during the lesson does not bring
// the prompt back (owner decision 2026-10-14).
func TestLessonPromptFollowsTheStoredSetting(t *testing.T) {
	t.Run("prompts on", func(t *testing.T) {
		sc := newPromptScenario(t)
		sc.tickAt(londonStart.Add(-10 * time.Minute))
		v, ok := sc.prompts.Current()
		if !ok || !sc.popup.Visible() || v.Lesson.Title != "English" || v.Lesson.TimeZone != "Europe/London" {
			t.Fatalf("current %+v, %v, visible %v", v, ok, sc.popup.Visible())
		}
		got, err := sc.prompts.Start(context.Background(), v.ID)
		if err != nil || got.State != prompt.StateStarted || sc.popup.Visible() {
			t.Fatalf("Start = %+v, %v, visible %v", got, err, sc.popup.Visible())
		}
		if starts := sc.recordings.all(); len(starts) != 1 || starts[0] != v.Lesson.ID {
			t.Fatalf("starts %v, want the lesson", starts)
		}
	})

	t.Run("prompts off", func(t *testing.T) {
		sc := newPromptScenario(t)
		sc.setPrompts(false)
		sc.tickAt(londonStart.Add(-10 * time.Minute))
		sc.setPrompts(true)
		sc.tickAt(londonStart)
		sc.tickAt(londonStart.Add(10 * time.Second))
		if _, ok := sc.prompts.Current(); ok || sc.popup.Shows() != 0 {
			t.Fatalf("prompt shown %d times although prompts were off at the start", sc.popup.Shows())
		}
	})
}

// A snoozed prompt comes back after the delay, but not once prompts were turned off; the lesson end hides it.
func TestSnoozedLessonPromptOnSQLite(t *testing.T) {
	sc := newPromptScenario(t)
	sc.tickAt(londonStart)
	v, _ := sc.prompts.Current()
	if _, err := sc.prompts.Snooze(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	sc.tickAt(londonStart.Add(prompt.SnoozeDelay))
	if !sc.popup.Visible() {
		t.Fatal("the snoozed prompt must come back")
	}

	if _, err := sc.prompts.Snooze(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	sc.setPrompts(false)
	sc.tickAt(londonStart.Add(2 * prompt.SnoozeDelay))
	if sc.popup.Visible() {
		t.Fatal("a snoozed prompt must not come back once prompts are off")
	}
	if _, ok := sc.prompts.Current(); ok {
		t.Fatal("the withdrawn prompt is over")
	}
}

// Ignored, the popup hides at the lesson end.
func TestIgnoredLessonPromptExpiresOnSQLite(t *testing.T) {
	sc := newPromptScenario(t)
	sc.tickAt(londonStart)
	if !sc.popup.Visible() {
		t.Fatal("no popup at the start")
	}
	sc.tickAt(londonStart.Add(90 * time.Minute))
	if sc.popup.Visible() {
		t.Fatal("the popup must hide at the lesson end")
	}
}
