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
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func TestNew(t *testing.T) {
	e := newEnv(t)
	full := Deps{Settings: e.settings, Popup: e.popup, Recordings: e.recordings, Clock: e.clock, Sleeper: e.clock}
	tests := []struct {
		name string
		edit func(*Deps, *Config)
		ok   bool
	}{
		{name: "ok", edit: func(*Deps, *Config) {}, ok: true},
		{name: "no settings", edit: func(d *Deps, _ *Config) { d.Settings = nil }},
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
				ID: v.ID, Kind: prompt.KindLesson, State: prompt.StateOffered, CanSnooze: true,
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

	got, err := e.svc.Start(context.Background(), v.ID)
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
	if _, err := e.svc.Start(context.Background(), v.ID); !errors.Is(err, ErrNoPrompt) {
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
		{"generic failure", true, errors.New("device busy"), prompt.ProblemFailed},
		{"recordings not ready", false, nil, prompt.ProblemRecovering},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			v := e.offer("lesson-1")
			e.recordings.set(tt.ready, tt.err)

			got, err := e.svc.Start(context.Background(), v.ID)
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
			if got, err := e.svc.Start(context.Background(), v.ID); err != nil || got.State != prompt.StateStarted {
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
		"start":  func(id prompt.ID) (View, error) { return e.svc.Start(ctx, id) },
		"snooze": func(id prompt.ID) (View, error) { return e.svc.Snooze(ctx, id) },
		"skip":   func(id prompt.ID) (View, error) { return e.svc.Skip(ctx, id) },
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

// A double click while a start runs is refused; snooze and skip wait for the start too.
func TestOneStartAtATime(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t)
	v := e.offer("lesson-1")
	entered, release := e.recordings.hold()
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Start(context.Background(), v.ID)
		done <- err
	}()
	<-entered

	if cur, _ := e.svc.Current(); cur.State != prompt.StateStarting || cur.CanSnooze {
		t.Fatalf("current %+v, want STARTING without snooze", cur)
	}
	for name, act := range map[string]func() error{
		"start":  func() error { _, err := e.svc.Start(context.Background(), v.ID); return err },
		"snooze": func() error { _, err := e.svc.Snooze(context.Background(), v.ID); return err },
		"skip":   func() error { _, err := e.svc.Skip(context.Background(), v.ID); return err },
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
		v, err := e.svc.Start(context.Background(), old.ID)
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

// Snooze hides the popup for the delay; it comes back when due, then expires at the lesson end.
func TestSnoozeShowsAgainWhenDue(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	v := e.offer("lesson-1")
	got, err := e.svc.Snooze(ctx, v.ID)
	if err != nil || got.State != prompt.StateSnoozed || e.popup.Visible() {
		t.Fatalf("Snooze = %+v, %v, visible %v", got, err, e.popup.Visible())
	}

	e.clock.Advance(prompt.SnoozeDelay - time.Second)
	if err := e.svc.Tick(ctx); err != nil || e.popup.Visible() {
		t.Fatalf("early Tick = %v, visible %v", err, e.popup.Visible())
	}
	e.clock.Advance(time.Second)
	if err := e.svc.Tick(ctx); err != nil || !e.popup.Visible() {
		t.Fatalf("due Tick = %v, visible %v", err, e.popup.Visible())
	}
	if cur, ok := e.svc.Current(); !ok || cur.ID != v.ID || cur.State != prompt.StateOffered {
		t.Fatalf("current %+v, %v", cur, ok)
	}

	e.at(lessonEnd)
	if err := e.svc.Tick(ctx); err != nil || e.popup.Visible() {
		t.Fatalf("Tick at the end = %v, visible %v", err, e.popup.Visible())
	}
	if _, ok := e.svc.Current(); ok {
		t.Fatal("an expired prompt is over")
	}
}

// Owner decision: a snoozed prompt must not come back once prompts were turned off.
func TestSnoozedPromptStaysHiddenWhenPromptsTurnedOff(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	v := e.offer("lesson-1")
	if _, err := e.svc.Snooze(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	e.settings.setPrompts(false)

	e.clock.Advance(prompt.SnoozeDelay)
	if err := e.svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if e.popup.Visible() || e.popup.Shows() != 1 {
		t.Fatalf("visible %v, shows %d: the snoozed prompt came back", e.popup.Visible(), e.popup.Shows())
	}
	if _, ok := e.svc.Current(); ok {
		t.Fatal("the withdrawn prompt is over")
	}

	// Turning prompts on again does not bring it back either.
	e.settings.setPrompts(true)
	e.clock.Advance(prompt.SnoozeDelay)
	if err := e.svc.Tick(ctx); err != nil || e.popup.Visible() {
		t.Fatalf("Tick = %v, visible %v", err, e.popup.Visible())
	}
}

// A failed settings read keeps the snoozed prompt for the next tick.
func TestReshowWaitsForTheSettings(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	v := e.offer("lesson-1")
	if _, err := e.svc.Snooze(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("database locked")
	e.settings.fail(boom)
	e.clock.Advance(prompt.SnoozeDelay)
	if err := e.svc.Tick(ctx); !errors.Is(err, boom) || e.popup.Visible() {
		t.Fatalf("Tick = %v, visible %v", err, e.popup.Visible())
	}
	e.settings.fail(nil)
	if err := e.svc.Tick(ctx); err != nil || !e.popup.Visible() {
		t.Fatalf("Tick after the error = %v, visible %v", err, e.popup.Visible())
	}
}

func TestSnoozeRefusedNearTheEnd(t *testing.T) {
	e := newEnv(t)
	v := e.offer("lesson-1")
	e.at(lessonEnd.Add(-time.Minute))
	if cur, _ := e.svc.Current(); cur.CanSnooze {
		t.Fatal("snooze offered although the lesson ends first")
	}
	if _, err := e.svc.Snooze(context.Background(), v.ID); !errors.Is(err, prompt.ErrSnoozeNotAllowed) {
		t.Fatalf("Snooze = %v", err)
	}
	if !e.popup.Visible() {
		t.Fatal("a refused snooze must keep the popup")
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

// The tray's ad-hoc offer ignores the prompts setting, cannot be snoozed, never expires and records without a lesson.
func TestAdHocIgnoresTheSetting(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.settings.setPrompts(false)

	v, err := e.svc.OfferAdHoc(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != prompt.KindAdHoc || v.State != prompt.StateOffered || v.CanSnooze || v.Lesson != (prompt.Lesson{}) {
		t.Fatalf("view %+v", v)
	}
	if !e.popup.Visible() || e.settings.loads != 0 {
		t.Fatalf("visible %v, settings read %d times; want shown without reading the setting", e.popup.Visible(), e.settings.loads)
	}
	if _, err := e.svc.Snooze(ctx, v.ID); !errors.Is(err, prompt.ErrSnoozeNotAllowed) {
		t.Fatalf("Snooze = %v", err)
	}

	e.clock.Advance(48 * time.Hour)
	if err := e.svc.Tick(ctx); err != nil || !e.popup.Visible() {
		t.Fatalf("Tick = %v, visible %v: an ad-hoc prompt does not expire", err, e.popup.Visible())
	}
	if got, err := e.svc.Start(ctx, v.ID); err != nil || got.State != prompt.StateStarted {
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

	// A snoozed lesson prompt is hidden: the tray offers a new ad-hoc prompt.
	if _, err := e.svc.Snooze(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	got, err = e.svc.OfferAdHoc(ctx)
	if err != nil || got.ID == v.ID || got.Kind != prompt.KindAdHoc || !e.popup.Visible() {
		t.Fatalf("OfferAdHoc = %+v, %v", got, err)
	}
}

// Run polls on the Sleeper: a snoozed prompt comes back without anyone calling Tick, and Run ends with ctx.
func TestRunShowsTheSnoozedPromptAgain(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t)
	v := e.offer("lesson-1")
	if _, err := e.svc.Snooze(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.svc.Run(ctx) }()

	testenv.Eventually(t, "the snoozed prompt is shown again", func() bool { return e.popup.Visible() }, func() {
		if err := e.clock.AwaitSleepers(ctx, 1); err == nil {
			e.clock.Advance(DefaultPollInterval)
		}
	})
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}
