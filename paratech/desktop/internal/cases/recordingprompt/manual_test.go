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
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// TASK-081: a start from the main window is linked to the lesson in progress now, or to none.
func TestStartManualLinksTheLessonInProgress(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	tests := []struct {
		name    string
		lessons func(t *testing.T, f *fakeLessons)
		err     error
		want    shared.LessonID
	}{
		{name: "no schedule", lessons: func(*testing.T, *fakeLessons) {}},
		{name: "lesson in progress", lessons: func(t *testing.T, f *fakeLessons) {
			f.add(t, "history", t0, 90*time.Minute)
		}, want: "history"},
		{name: "starts right now", lessons: func(t *testing.T, f *fakeLessons) {
			f.add(t, "now", now, time.Hour)
		}, want: "now"},
		{name: "ended or not started", lessons: func(t *testing.T, f *fakeLessons) {
			f.add(t, "ended", t0.Add(-2*time.Hour), time.Hour)
			f.add(t, "ends-now", t0, 10*time.Minute)
			f.add(t, "later", now.Add(time.Millisecond), time.Hour)
		}},
		{name: "a long lesson started yesterday", lessons: func(t *testing.T, f *fakeLessons) {
			f.add(t, "long", now.Add(-lesson.MaxDuration+time.Minute), lesson.MaxDuration)
		}, want: "long"},
		{name: "overlapping: the later start", lessons: func(t *testing.T, f *fakeLessons) {
			f.add(t, "block", t0.Add(-time.Hour), 3*time.Hour)
			f.add(t, "history", t0, 90*time.Minute)
		}, want: "history"},
		{name: "schedule unreadable: recorded without a lesson", err: errors.New("database is locked")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.at(now)
			if tt.lessons != nil {
				tt.lessons(t, e.lessons)
			}
			e.lessons.err = tt.err

			got, err := e.svc.StartManual(context.Background(), values.WholeComputer())
			if err != nil || got != (Manual{Recording: "rec-1", Lesson: tt.want}) {
				t.Fatalf("StartManual = %+v, %v; want lesson %q", got, err, tt.want)
			}
			if started := e.recordings.started(); !slices.Equal(started, []shared.LessonID{tt.want}) {
				t.Fatalf("recordings started for %v, want %q", started, tt.want)
			}
			if !e.lessons.from.Equal(now.Add(-lesson.MaxDuration)) || !e.lessons.to.After(now) {
				t.Fatalf("schedule read [%v, %v), want [now-%v, after now)", e.lessons.from, e.lessons.to, lesson.MaxDuration)
			}
		})
	}
}

// TASK-081: a start that cannot run is refused with the popup's problem kinds and remembers nothing. While a
// recording runs the session refuses the second one as in progress.
func TestStartManualRefused(t *testing.T) {
	tests := []struct {
		name     string
		notReady bool
		err      error
		problem  prompt.Problem
		starts   int
	}{
		{name: "recovering", notReady: true, problem: prompt.ProblemRecovering},
		{name: "a recording runs", err: fmt.Errorf("start: %w", limits.ErrRecordingInProgress),
			problem: prompt.ProblemInProgress, starts: 1},
		{name: "app not running", err: fmt.Errorf("start: %w", recording.ErrSourceNotRunning),
			problem: prompt.ProblemSourceGone, starts: 1},
		{name: "no capture", err: fmt.Errorf("start: %w", recording.ErrCaptureUnavailable),
			problem: prompt.ProblemUnavailable, starts: 1},
		{name: "other", err: errors.New("disk full"), problem: prompt.ProblemFailed, starts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.at(t0.Add(time.Minute))
			e.lessons.add(t, "history", t0, 90*time.Minute)
			e.recordings.set(!tt.notReady, tt.err)

			got, err := e.svc.StartManual(context.Background(), mustSource(t, "zoom.exe"))
			if !errors.Is(err, ErrStartFailed) || got.Problem != tt.problem || got.Recording != "" {
				t.Fatalf("StartManual = %+v, %v; want problem %s", got, err, tt.problem)
			}
			if tt.notReady && !errors.Is(err, ErrNotReady) {
				t.Fatalf("StartManual = %v, want ErrNotReady", err)
			}
			if n := len(e.recordings.started()); n != tt.starts {
				t.Fatalf("%d starts, want %d", n, tt.starts)
			}
			if app, saves := e.settings.remembered(); app != "" || saves != 0 {
				t.Fatalf("remembered %q after %d saves", app, saves)
			}
		})
	}
}

// TASK-081: once a recording runs from the main window, a prompt on offer (lesson or ad hoc) ends as started and the
// popup hides; a failed start leaves it on offer.
func TestStartManualEndsTheOffer(t *testing.T) {
	ctx := context.Background()
	offers := map[string]func(e *env){
		"lesson": func(e *env) { e.offer("lesson-1") },
		"ad hoc": func(e *env) {
			if _, err := e.svc.OfferAdHoc(ctx); err != nil {
				e.t.Fatal(err)
			}
		},
	}
	for name, offer := range offers {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			offer(e)
			e.at(t0.Add(time.Minute))
			e.lessons.add(t, "lesson-1", t0, 90*time.Minute)

			e.recordings.set(true, fmt.Errorf("start: %w", limits.ErrRecordingInProgress))
			if _, err := e.svc.StartManual(ctx, values.WholeComputer()); !errors.Is(err, ErrStartFailed) {
				t.Fatalf("StartManual = %v, want ErrStartFailed", err)
			}
			if cur, ok := e.svc.Current(); !ok || cur.State != prompt.StateOffered || !e.popup.Visible() {
				t.Fatalf("after a failed start: current %+v (%v), popup visible %v", cur, ok, e.popup.Visible())
			}

			e.recordings.set(true, nil)
			if _, err := e.svc.StartManual(ctx, values.WholeComputer()); err != nil {
				t.Fatal(err)
			}
			if cur, ok := e.svc.Current(); ok {
				t.Fatalf("the prompt %+v must end", cur)
			}
			if e.popup.Visible() {
				t.Fatal("the popup must hide")
			}
		})
	}
}

// A popup start that runs is not touched by a manual start: it finishes on its own.
func TestStartManualLeavesAStartingPrompt(t *testing.T) {
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

	if _, err := e.svc.StartManual(context.Background(), values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if cur, ok := e.svc.Current(); !ok || cur.ID != v.ID || cur.State != prompt.StateStarting {
		t.Fatalf("current %+v (%v), want the starting prompt", cur, ok)
	}

	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.Current(); ok || e.popup.Visible() {
		t.Fatal("the prompt must end after its start")
	}
}

// TASK-081: one remembered source for the popup, the main window and the sound check (the settings).
func TestSourceIsShared(t *testing.T) {
	ctx := context.Background()
	zoom, teams := mustSource(t, "zoom.exe"), mustSource(t, "teams.exe")
	e := newEnv(t)
	e.apps.set(nil, values.AudioApp{App: "zoom.exe", Title: "Zoom"}, values.AudioApp{App: "teams.exe", Title: "Teams"})

	// Picked on the diagnostics page: the popup preselects it.
	if err := e.svc.SelectSource(ctx, zoom); err != nil {
		t.Fatal(err)
	}
	src, err := e.svc.Sources(ctx)
	if err != nil || src.Selected != zoom || src.Remembered != "zoom.exe" {
		t.Fatalf("Sources = %+v, %v; want zoom.exe", src, err)
	}

	// Started from the main window with another app: that one is remembered.
	if _, err := e.svc.StartManual(ctx, teams); err != nil {
		t.Fatal(err)
	}
	if got := e.recordings.sources; !slices.Equal(got, []values.Source{teams}) {
		t.Fatalf("recorded %v, want teams.exe", got)
	}
	if src, _ := e.svc.Sources(ctx); src.Selected != teams {
		t.Fatalf("Sources preselect %v, want teams.exe", src.Selected)
	}

	// The whole computer is a choice too.
	if err := e.svc.SelectSource(ctx, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if app, _ := e.settings.remembered(); app != "" {
		t.Fatalf("remembered %q, want the whole computer", app)
	}
}

func TestSelectSource(t *testing.T) {
	ctx := context.Background()
	zoom := mustSource(t, "zoom.exe")
	tests := []struct {
		name      string
		remember  shared.AppName // already remembered
		conflicts int
		saveErr   error
		loadErr   error
		wantErr   bool
		wantApp   shared.AppName
		wantSaves int
	}{
		{name: "saved", wantApp: "zoom.exe", wantSaves: 1},
		{name: "the same source saves nothing", remember: "zoom.exe", wantApp: "zoom.exe"},
		{name: "a version conflict reloads once", conflicts: 1, wantApp: "zoom.exe", wantSaves: 2},
		{name: "conflicts go on", conflicts: rememberAttempts, wantErr: true, wantSaves: rememberAttempts},
		{name: "save fails", saveErr: errors.New("disk full"), wantErr: true, wantSaves: 1},
		{name: "load fails", loadErr: errors.New("database is locked"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.settings.source, e.settings.conflicts, e.settings.saveErr, e.settings.err =
				tt.remember, tt.conflicts, tt.saveErr, tt.loadErr

			err := e.svc.SelectSource(ctx, zoom)
			if tt.wantErr != (err != nil) {
				t.Fatalf("SelectSource = %v, want error %v", err, tt.wantErr)
			}
			if app, saves := e.settings.remembered(); app != tt.wantApp || saves != tt.wantSaves {
				t.Fatalf("remembered %q after %d saves, want %q after %d", app, saves, tt.wantApp, tt.wantSaves)
			}
		})
	}
}
