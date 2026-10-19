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
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var _ recordingprompt.SettingsStore = (*sqlite.SettingsRepository)(nil)

// London lesson of dstSchedule: 09:00-10:30 UTC on 2026-10-25.
var londonStart = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

// startedLessons stands for the recording session behind the prompt.
type startedLessons struct {
	mu      sync.Mutex
	lessons []shared.LessonID
	sources []values.Source
}

func (s *startedLessons) Ready() bool { return true }

func (s *startedLessons) StartRecording(_ context.Context, l shared.LessonID, source values.Source) (shared.RecordingID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lessons = append(s.lessons, l)
	s.sources = append(s.sources, source)
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
	apps       *testenv.FakeAudioApps
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
	sc := &promptScenario{
		t: t, clock: clock, popup: &testenv.FakePopup{}, prefs: prefs, apps: &testenv.FakeAudioApps{}, recordings: &startedLessons{},
	}
	lessons := sqlite.NewLessonRepository(db)
	sc.prompts, err = recordingprompt.New(recordingprompt.Deps{
		Settings: settings, Apps: sc.apps, Popup: sc.popup, Recordings: sc.recordings, Lessons: lessons, Clock: clock,
		Sleeper: clock,
	}, recordingprompt.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	sc.sched, err = scheduler.New(scheduler.Deps{
		Lessons: lessons, Clock: clock, Sleeper: clock, Events: sc.prompts,
	}, scheduler.DefaultConfig())
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
// the prompt back (owner decision 2026-10-14). With the default config the popup comes at the start, not before it
// (owner decision 2026-10-18; TASK-076 moved these ticks from 10 minutes before the start to the start).
func TestLessonPromptFollowsTheStoredSetting(t *testing.T) {
	t.Run("prompts on", func(t *testing.T) {
		sc := newPromptScenario(t)
		sc.tickAt(londonStart.Add(-time.Minute))
		if _, ok := sc.prompts.Current(); ok || sc.popup.Shows() != 0 {
			t.Fatal("the popup must not come before the lesson start")
		}
		sc.tickAt(londonStart)
		v, ok := sc.prompts.Current()
		if !ok || !sc.popup.Visible() || v.Lesson.Title != "English" || v.Lesson.TimeZone != "Europe/London" {
			t.Fatalf("current %+v, %v, visible %v", v, ok, sc.popup.Visible())
		}
		got, err := sc.prompts.Start(context.Background(), v.ID, values.WholeComputer())
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
		sc.tickAt(londonStart)
		sc.setPrompts(true)
		sc.tickAt(londonStart.Add(10 * time.Second))
		if _, ok := sc.prompts.Current(); ok || sc.popup.Shows() != 0 {
			t.Fatalf("prompt shown %d times although prompts were off at the start", sc.popup.Shows())
		}
	})
}

// "Do not record" hides the popup for good: the lesson is not offered again, even within the late-start window.
// (Replaces the snooze scenario of TASK-022: snooze was removed by the owner on 2026-10-18.)
func TestSkippedLessonPromptOnSQLite(t *testing.T) {
	sc := newPromptScenario(t)
	sc.tickAt(londonStart)
	v, ok := sc.prompts.Current()
	if !ok {
		t.Fatal("no prompt at the start")
	}
	if _, err := sc.prompts.Skip(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	sc.tickAt(londonStart.Add(10 * time.Second))
	if _, ok := sc.prompts.Current(); ok || sc.popup.Visible() || sc.popup.Shows() != 1 {
		t.Fatalf("shows %d, visible %v: a skipped lesson must not be offered again", sc.popup.Shows(), sc.popup.Visible())
	}
	if len(sc.recordings.all()) != 0 {
		t.Fatal("a skipped prompt must not record")
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

// The source picked for a recording is remembered in SQLite by its executable name (TASK-059): the next picker
// preselects it while the app is open, and keeps it remembered while it is not.
func TestPickedSourceIsRememberedOnSQLite(t *testing.T) {
	sc := newPromptScenario(t)
	ctx := context.Background()
	sc.apps.Set(values.AudioApp{App: "zoom.exe", Title: "Zoom Meeting", PID: 300}, values.AudioApp{App: "chrome.exe", PID: 200, Playing: true})

	src, err := sc.prompts.Sources(ctx)
	if err != nil || !src.Selected.IsWholeComputer() || len(src.Apps) != 2 || src.Apps[0].App != "chrome.exe" {
		t.Fatalf("first Sources = %+v, %v; want the playing app first and the whole computer selected", src, err)
	}
	zoom, err := values.SourceOf("zoom.exe")
	if err != nil {
		t.Fatal(err)
	}
	v, err := sc.prompts.OfferAdHoc(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.prompts.Start(ctx, v.ID, zoom); err != nil {
		t.Fatal(err)
	}

	if src, err = sc.prompts.Sources(ctx); err != nil || src.Selected != zoom || src.Remembered != "zoom.exe" {
		t.Fatalf("Sources after the start = %+v, %v; want zoom preselected", src, err)
	}
	sc.apps.Set(values.AudioApp{App: "chrome.exe", PID: 200})
	if src, err = sc.prompts.Sources(ctx); err != nil || !src.Selected.IsWholeComputer() || src.Remembered != "zoom.exe" {
		t.Fatalf("Sources without zoom = %+v, %v; want the whole computer selected and zoom still remembered", src, err)
	}
}

// TASK-081 on SQLite: «Начать запись» of the main window links the recording to the lesson in progress, start
// included and end excluded at the store's precision. The other lesson of dstSchedule is a week away, so any lesson
// here is the London one.
func TestManualStartLinksTheLessonOnSQLite(t *testing.T) {
	tests := []struct {
		name   string
		at     time.Duration // from londonStart
		linked bool
	}{
		{name: "just before the start", at: -time.Millisecond},
		{name: "at the start", linked: true},
		{name: "in the middle", at: 45 * time.Minute, linked: true},
		{name: "just before the end", at: 90*time.Minute - time.Millisecond, linked: true},
		{name: "at the end", at: 90 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := newPromptScenario(t)
			sc.clock.Advance(londonStart.Add(tt.at).Sub(sc.clock.Now()))
			m, err := sc.prompts.StartManual(context.Background(), values.WholeComputer())
			if err != nil || m.Recording != "rec-1" || (m.Lesson != "") != tt.linked {
				t.Fatalf("StartManual = %+v, %v; want linked %v", m, err, tt.linked)
			}
			if starts := sc.recordings.all(); !slices.Equal(starts, []shared.LessonID{m.Lesson}) {
				t.Fatalf("starts %v, want %q", starts, m.Lesson)
			}
		})
	}
}

// TASK-081: a start from the main window while the lesson popup is on offer records that lesson and ends the popup.
func TestManualStartEndsTheLessonPopupOnSQLite(t *testing.T) {
	sc := newPromptScenario(t)
	sc.tickAt(londonStart)
	v, ok := sc.prompts.Current()
	if !ok || !sc.popup.Visible() {
		t.Fatal("no popup at the start")
	}
	sc.clock.Advance(time.Minute)
	m, err := sc.prompts.StartManual(context.Background(), values.WholeComputer())
	if err != nil || m.Lesson != v.Lesson.ID {
		t.Fatalf("StartManual = %+v, %v; want the lesson %s", m, err, v.Lesson.ID)
	}
	if _, ok := sc.prompts.Current(); ok || sc.popup.Visible() {
		t.Fatal("the popup must end after a start from the main window")
	}
	sc.tickAt(londonStart.Add(2 * time.Minute))
	if sc.popup.Shows() != 1 {
		t.Fatalf("shows %d: the lesson must not be offered again", sc.popup.Shows())
	}
}
