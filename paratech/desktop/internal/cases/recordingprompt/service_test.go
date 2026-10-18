package recordingprompt

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func TestNew(t *testing.T) {
	e := newEnv(t)
	full := Deps{Settings: e.settings, Apps: e.apps, Popup: e.popup, Recordings: e.recordings, Clock: e.clock, Sleeper: e.clock}
	tests := []struct {
		name string
		edit func(*Deps, *Config)
		ok   bool
	}{
		{name: "ok", edit: func(*Deps, *Config) {}, ok: true},
		{name: "no settings", edit: func(d *Deps, _ *Config) { d.Settings = nil }},
		{name: "no apps", edit: func(d *Deps, _ *Config) { d.Apps = nil }},
		{name: "no popup", edit: func(d *Deps, _ *Config) { d.Popup = nil }},
		{name: "no recordings", edit: func(d *Deps, _ *Config) { d.Recordings = nil }},
		{name: "no clock", edit: func(d *Deps, _ *Config) { d.Clock = nil }},
		{name: "no sleeper", edit: func(d *Deps, _ *Config) { d.Sleeper = nil }},
		{name: "zero poll", edit: func(_ *Deps, c *Config) { c.PollInterval = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, c := full, DefaultConfig()
			tt.edit(&d, &c)
			_, err := New(d, c)
			if tt.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidConfig)) {
				t.Fatalf("New = %v, ok %v", err, tt.ok)
			}
		})
	}
}

// Test step 2, AC-01, AC-02: a scheduler event shows the popup when prompts are on and shows nothing when they are
// off; the dropped event leaves no prompt behind.
func TestLessonStartingFollowsThePromptsSetting(t *testing.T) {
	tests := []struct {
		name      string
		promptsOn bool
	}{
		{name: "prompts on", promptsOn: true},
		{name: "prompts off", promptsOn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.settings.setPrompts(tt.promptsOn)
			if err := e.svc.LessonStarting(context.Background(), announcement("lesson-1")); err != nil {
				t.Fatal(err)
			}

			v, ok := e.svc.Current()
			if !tt.promptsOn {
				if ok || e.popup.Shows() != 0 || e.popup.Hides() != 0 {
					t.Fatalf("prompts off: current %v, popup shows %d hides %d; want no prompt and no popup command",
						ok, e.popup.Shows(), e.popup.Hides())
				}
				return
			}
			if !ok || e.popup.Shows() != 1 || !e.popup.Visible() {
				t.Fatalf("prompts on: current %v, popup shows %d; want one shown prompt", ok, e.popup.Shows())
			}
			want := View{
				ID: v.ID, Kind: prompt.KindLesson, State: prompt.StateOffered,
				Lesson: prompt.Lesson{ID: "lesson-1", Title: "History <script>", Start: t0, End: lessonEnd, TimeZone: "Europe/Moscow"},
			}
			if v != want || v.ID.Validate() != nil {
				t.Fatalf("view %+v, want %+v", v, want)
			}
		})
	}
}

func TestLessonStartingErrors(t *testing.T) {
	boom := errors.New("database locked")

	e := newEnv(t)
	e.settings.fail(boom)
	if err := e.svc.LessonStarting(context.Background(), announcement("lesson-1")); !errors.Is(err, boom) {
		t.Fatalf("settings error: %v", err)
	}
	if _, ok := e.svc.Current(); ok || e.popup.Shows() != 0 {
		t.Fatal("a failed settings read must not show a prompt")
	}

	// The shell does not run: the prompt stays current, so the page reads it once the popup can show.
	e = newEnv(t)
	e.popup.Fail(boom)
	if err := e.svc.LessonStarting(context.Background(), announcement("lesson-1")); !errors.Is(err, boom) {
		t.Fatalf("popup error: %v", err)
	}
	if _, ok := e.svc.Current(); !ok {
		t.Fatal("the prompt must stay current when the popup fails")
	}

	// A lesson that already ended is not offered.
	e = newEnv(t)
	e.at(lessonEnd)
	if err := e.svc.LessonStarting(context.Background(), announcement("lesson-1")); !errors.Is(err, prompt.ErrInvalidPrompt) {
		t.Fatalf("ended lesson: %v", err)
	}
}

// Test step 3 on fakes: Start starts the recording for the lesson, hides the popup and ends the prompt.
func TestStartRecordsTheLessonAndHides(t *testing.T) {
	e := newEnv(t)
	v := e.offer("lesson-1")

	got, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != prompt.StateStarted || got.Problem != prompt.ProblemNone {
		t.Fatalf("view %+v, want STARTED without a problem", got)
	}
	if !slices.Equal(e.recordings.started(), []shared.LessonID{"lesson-1"}) {
		t.Fatalf("starts %v, want one for the lesson", e.recordings.started())
	}
	if e.popup.Visible() || e.popup.Hides() != 1 {
		t.Fatalf("popup visible %v hides %d, want hidden once", e.popup.Visible(), e.popup.Hides())
	}
	if _, ok := e.svc.Current(); ok {
		t.Fatal("the prompt is over after a start")
	}
	if _, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer()); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("a second click on the same popup: %v", err)
	}
}

// A failed start keeps the popup up with a fixed problem; the user can retry.
func TestFailedStartKeepsThePopup(t *testing.T) {
	tests := []struct {
		name  string
		ready bool
		err   error
		want  prompt.Problem
	}{
		{"recorder unavailable in this build", true, fmt.Errorf("start: %w", recording.ErrCaptureUnavailable), prompt.ProblemUnavailable},
		{"another recording in progress", true, fmt.Errorf("start: %w: r0", limits.ErrRecordingInProgress), prompt.ProblemInProgress},
		{"the chosen app is closed", true, fmt.Errorf("start: %w: zoom.exe", recording.ErrSourceNotRunning), prompt.ProblemSourceGone},
		{"generic failure", true, errors.New("device busy"), prompt.ProblemFailed},
		{"recordings not ready", false, nil, prompt.ProblemRecovering},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			v := e.offer("lesson-1")
			e.recordings.set(tt.ready, tt.err)

			got, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer())
			if !errors.Is(err, ErrStartFailed) {
				t.Fatalf("Start = %v, want ErrStartFailed", err)
			}
			if !tt.ready && (!errors.Is(err, ErrNotReady) || len(e.recordings.started()) != 0) {
				t.Fatalf("not ready: err %v, starts %v; want ErrNotReady and no start", err, e.recordings.started())
			}
			if got.State != prompt.StateOffered || got.Problem != tt.want {
				t.Fatalf("view %+v, want OFFERED with %q", got, tt.want)
			}
			if cur, ok := e.svc.Current(); !ok || cur.Problem != tt.want {
				t.Fatalf("current %+v, %v", cur, ok)
			}
			if !e.popup.Visible() || e.popup.Hides() != 0 {
				t.Fatal("the popup must stay up after a failed start")
			}

			e.recordings.set(true, nil)
			if got, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer()); err != nil || got.State != prompt.StateStarted {
				t.Fatalf("retry = %+v, %v", got, err)
			}
		})
	}
}

// A click on an old popup never acts on a newer prompt.
func TestStaleIDIsRefused(t *testing.T) {
	e := newEnv(t)
	old := e.offer("lesson-1")
	cur := e.offer("lesson-2")
	if old.ID == cur.ID {
		t.Fatal("prompt IDs must differ")
	}

	ctx := context.Background()
	actions := map[string]func(prompt.ID) (View, error){
		"start": func(id prompt.ID) (View, error) { return e.svc.Start(ctx, id, values.WholeComputer()) },
		"skip":  func(id prompt.ID) (View, error) { return e.svc.Skip(ctx, id) },
	}
	for name, act := range actions {
		for _, id := range []prompt.ID{old.ID, "", "prompt-999"} {
			if _, err := act(id); !errors.Is(err, ErrNoPrompt) {
				t.Errorf("%s(%q) = %v, want ErrNoPrompt", name, id, err)
			}
		}
	}
	if len(e.recordings.started()) != 0 {
		t.Fatal("a stale start reached the recorder")
	}
	if v, ok := e.svc.Current(); !ok || v.ID != cur.ID || v.State != prompt.StateOffered || !e.popup.Visible() {
		t.Fatalf("current %+v, %v: the newer prompt must be untouched", v, ok)
	}
}

// A double click while a start runs is refused; skip waits for the start too.
func TestOneStartAtATime(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t)
	v := e.offer("lesson-1")
	entered, release := e.recordings.hold()
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer())
		done <- err
	}()
	<-entered

	if cur, _ := e.svc.Current(); cur.State != prompt.StateStarting {
		t.Fatalf("current %+v, want STARTING", cur)
	}
	for name, act := range map[string]func() error{
		"start": func() error { _, err := e.svc.Start(context.Background(), v.ID, values.WholeComputer()); return err },
		"skip":  func() error { _, err := e.svc.Skip(context.Background(), v.ID); return err },
	} {
		if err := act(); !errors.Is(err, prompt.ErrInvalidTransition) {
			t.Errorf("%s during the start = %v, want ErrInvalidTransition", name, err)
		}
	}
	// The lesson ends while the start runs: the start is not cut off.
	e.at(lessonEnd.Add(time.Minute))
	if err := e.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := len(e.recordings.started()); n != 1 {
		t.Fatalf("%d starts, want 1", n)
	}
	if e.popup.Visible() {
		t.Fatal("the popup must hide after the start")
	}
}

// A newer lesson that arrives during a start replaces the prompt; the finished start does not hide the new popup.
func TestNewLessonDuringAStart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t)
	old := e.offer("lesson-1")
	entered, release := e.recordings.hold()
	defer release()

	done := make(chan View, 1)
	go func() {
		v, err := e.svc.Start(context.Background(), old.ID, values.WholeComputer())
		if err != nil {
			t.Error(err)
		}
		done <- v
	}()
	<-entered
	cur := e.offer("lesson-2")
	release()

	if v := <-done; v.State != prompt.StateStarted {
		t.Fatalf("old prompt %+v, want STARTED", v)
	}
	if v, ok := e.svc.Current(); !ok || v.ID != cur.ID || !e.popup.Visible() {
		t.Fatalf("current %+v, %v, popup visible %v: the new prompt must stay shown", v, ok, e.popup.Visible())
	}
}

func TestSkipHides(t *testing.T) {
	e := newEnv(t)
	v := e.offer("lesson-1")
	got, err := e.svc.Skip(context.Background(), v.ID)
	if err != nil || got.State != prompt.StateSkipped {
		t.Fatalf("Skip = %+v, %v", got, err)
	}
	if e.popup.Visible() {
		t.Fatal("the popup must hide")
	}
	if _, ok := e.svc.Current(); ok {
		t.Fatal("a skipped prompt is over")
	}

	// Nothing brings it back: the lesson is not offered again.
	e.at(t0.Add(time.Hour))
	if err := e.svc.Tick(context.Background()); err != nil || e.popup.Visible() || e.popup.Shows() != 1 {
		t.Fatalf("Tick = %v, shows %d", err, e.popup.Shows())
	}
}

// Ignoring the popup: it hides by itself at the lesson end.
func TestExpiryHides(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.offer("lesson-1")

	e.at(lessonEnd.Add(-time.Second))
	if err := e.svc.Tick(ctx); err != nil || !e.popup.Visible() {
		t.Fatalf("Tick before the end = %v, visible %v", err, e.popup.Visible())
	}
	e.at(lessonEnd)
	if err := e.svc.Tick(ctx); err != nil || e.popup.Visible() {
		t.Fatalf("Tick at the end = %v, visible %v", err, e.popup.Visible())
	}
	if _, ok := e.svc.Current(); ok {
		t.Fatal("an expired prompt is over")
	}
}

// The tray's ad-hoc offer ignores the prompts setting, never expires and records without a lesson.
func TestAdHocIgnoresTheSetting(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.settings.setPrompts(false)

	v, err := e.svc.OfferAdHoc(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != prompt.KindAdHoc || v.State != prompt.StateOffered || v.Lesson != (prompt.Lesson{}) {
		t.Fatalf("view %+v", v)
	}
	if !e.popup.Visible() || e.settings.loads != 0 {
		t.Fatalf("visible %v, settings read %d times; want shown without reading the setting", e.popup.Visible(), e.settings.loads)
	}
	e.clock.Advance(48 * time.Hour)
	if err := e.svc.Tick(ctx); err != nil || !e.popup.Visible() {
		t.Fatalf("Tick = %v, visible %v: an ad-hoc prompt does not expire", err, e.popup.Visible())
	}
	if got, err := e.svc.Start(ctx, v.ID, values.WholeComputer()); err != nil || got.State != prompt.StateStarted {
		t.Fatalf("Start = %+v, %v", got, err)
	}
	if !slices.Equal(e.recordings.started(), []shared.LessonID{""}) {
		t.Fatalf("starts %v, want one without a lesson", e.recordings.started())
	}
}

// The tray item on a shown prompt shows the same prompt again instead of replacing it.
func TestOfferAdHocKeepsAShownPrompt(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	v := e.offer("lesson-1")
	got, err := e.svc.OfferAdHoc(ctx)
	if err != nil || got.ID != v.ID || got.Kind != prompt.KindLesson || e.popup.Shows() != 2 {
		t.Fatalf("OfferAdHoc = %+v, %v, shows %d", got, err, e.popup.Shows())
	}

	// Once the lesson prompt is skipped, the tray offers a new ad-hoc prompt.
	if _, err := e.svc.Skip(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	got, err = e.svc.OfferAdHoc(ctx)
	if err != nil || got.ID == v.ID || got.Kind != prompt.KindAdHoc || !e.popup.Visible() {
		t.Fatalf("OfferAdHoc = %+v, %v", got, err)
	}
}

// Run polls on the Sleeper: a prompt whose lesson ended hides without anyone calling Tick, and Run ends with ctx.
// (Before TASK-076 this test brought back a snoozed prompt; snooze is gone, expiry is the remaining poll duty.)
func TestRunHidesTheExpiredPrompt(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t)
	e.offer("lesson-1")
	e.at(lessonEnd.Add(-DefaultPollInterval))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.svc.Run(ctx) }()

	testenv.Eventually(t, "the expired prompt hides", func() bool { return !e.popup.Visible() }, func() {
		if err := e.clock.AwaitSleepers(ctx, 1); err == nil {
			e.clock.Advance(DefaultPollInterval)
		}
	})
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

func mustSource(t *testing.T, app shared.AppName) values.Source {
	t.Helper()
	s, err := values.SourceOf(app)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func appNames(apps []values.AudioApp) []shared.AppName {
	out := make([]shared.AppName, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.App)
	}
	return out
}

// TASK-059: the picker lists the apps that play first and preselects the remembered app when it is open.
func TestSourcesSortsAndPreselects(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		remembered shared.AppName
		selected   values.Source
	}{
		{name: "remembered app is open", remembered: "zoom.exe", selected: mustSource(t, "zoom.exe")},
		{name: "remembered app is closed", remembered: "ms-teams.exe", selected: values.WholeComputer()},
		{name: "whole computer remembered", remembered: "", selected: values.WholeComputer()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.settings.source = tt.remembered
			e.apps.set(nil,
				values.AudioApp{App: "zoom.exe", Title: "Zoom Meeting", PID: 10},
				values.AudioApp{App: "chrome.exe", Title: "Lecture - YouTube", PID: 20, Playing: true},
				values.AudioApp{App: "notepad.exe", Title: "Notes", PID: 30},
			)
			got, err := e.svc.Sources(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if want := []shared.AppName{"chrome.exe", "notepad.exe", "zoom.exe"}; !slices.Equal(appNames(got.Apps), want) {
				t.Fatalf("apps %v, want %v", appNames(got.Apps), want)
			}
			if got.Selected != tt.selected || got.Remembered != tt.remembered {
				t.Fatalf("selected %v, remembered %q; want %v, %q", got.Selected, got.Remembered, tt.selected, tt.remembered)
			}
		})
	}
}

// When the apps cannot be listed the popup still offers the whole computer; a settings error only loses the
// remembered app.
func TestSourcesFailures(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("enumeration timed out")

	e := newEnv(t)
	e.settings.source = "zoom.exe"
	e.apps.set(boom)
	got, err := e.svc.Sources(ctx)
	if !errors.Is(err, boom) || len(got.Apps) != 0 || !got.Selected.IsWholeComputer() || got.Remembered != "zoom.exe" {
		t.Fatalf("Sources = %+v, %v; want the error, the whole computer and the remembered app", got, err)
	}

	e = newEnv(t)
	e.settings.fail(errors.New("database locked"))
	e.apps.set(nil, values.AudioApp{App: "zoom.exe", Title: "Zoom"})
	got, err = e.svc.Sources(ctx)
	if err != nil || len(got.Apps) != 1 || !got.Selected.IsWholeComputer() || got.Remembered != "" {
		t.Fatalf("Sources with a settings error = %+v, %v", got, err)
	}
}

// TASK-059: a started recording remembers its source (by app, not by process) for the next prompt; a failed start
// does not, and a failure to remember never fails the start.
func TestStartRemembersTheSource(t *testing.T) {
	ctx := context.Background()
	zoom := mustSource(t, "zoom.exe")

	t.Run("remembered after the start", func(t *testing.T) {
		e := newEnv(t)
		v := e.offer("lesson-1")
		if _, err := e.svc.Start(ctx, v.ID, zoom); err != nil {
			t.Fatal(err)
		}
		if src := e.recordings.sources; len(src) != 1 || src[0] != zoom {
			t.Fatalf("recordings got %v, want %v", src, zoom)
		}
		if app, saves := e.settings.remembered(); app != "zoom.exe" || saves != 1 {
			t.Fatalf("remembered %q after %d saves", app, saves)
		}

		// The same source again: nothing to save.
		v = e.offer("lesson-2")
		if _, err := e.svc.Start(ctx, v.ID, zoom); err != nil {
			t.Fatal(err)
		}
		if _, saves := e.settings.remembered(); saves != 1 {
			t.Fatalf("%d saves, want no second save of the same source", saves)
		}

		// The whole computer is remembered too.
		v = e.offer("lesson-3")
		if _, err := e.svc.Start(ctx, v.ID, values.WholeComputer()); err != nil {
			t.Fatal(err)
		}
		if app, _ := e.settings.remembered(); app != "" {
			t.Fatalf("remembered %q, want the whole computer", app)
		}
	})

	t.Run("a version conflict reloads once", func(t *testing.T) {
		e := newEnv(t)
		e.settings.conflicts = 1
		v := e.offer("lesson-1")
		if _, err := e.svc.Start(ctx, v.ID, zoom); err != nil {
			t.Fatal(err)
		}
		if app, saves := e.settings.remembered(); app != "zoom.exe" || saves != 2 {
			t.Fatalf("remembered %q after %d saves, want zoom.exe after the retry", app, saves)
		}
	})

	t.Run("a failure to remember keeps the start", func(t *testing.T) {
		e := newEnv(t)
		e.settings.conflicts = rememberAttempts
		v := e.offer("lesson-1")
		got, err := e.svc.Start(ctx, v.ID, zoom)
		if err != nil || got.State != prompt.StateStarted {
			t.Fatalf("Start = %+v, %v", got, err)
		}
		if app, saves := e.settings.remembered(); app != "" || saves != rememberAttempts {
			t.Fatalf("remembered %q after %d saves", app, saves)
		}

		e.settings.saveErr = errors.New("disk full")
		v = e.offer("lesson-2")
		if got, err := e.svc.Start(ctx, v.ID, zoom); err != nil || got.State != prompt.StateStarted {
			t.Fatalf("Start = %+v, %v", got, err)
		}
	})

	t.Run("a failed start remembers nothing", func(t *testing.T) {
		e := newEnv(t)
		e.recordings.set(true, fmt.Errorf("start: %w", recording.ErrSourceNotRunning))
		v := e.offer("lesson-1")
		got, err := e.svc.Start(ctx, v.ID, zoom)
		if !errors.Is(err, ErrStartFailed) || got.Problem != prompt.ProblemSourceGone {
			t.Fatalf("Start = %+v, %v", got, err)
		}
		if app, saves := e.settings.remembered(); app != "" || saves != 0 {
			t.Fatalf("remembered %q after %d saves", app, saves)
		}
	})
}
