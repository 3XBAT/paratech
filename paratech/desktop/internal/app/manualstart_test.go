package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// startRecordingNow presses «Начать запись» of the main window until the startup recovery has marked the interrupted
// recordings and returns the final answer.
func startRecordingNow(t *testing.T, b *wails.PromptService, app string) wails.RecordingStart {
	t.Helper()
	var r wails.RecordingStart
	testenv.Eventually(t, "a start past the recovery", func() bool {
		var err error
		if r, err = b.StartRecording(context.Background(), app); err != nil {
			t.Fatalf("StartRecording: %v", err)
		}
		return r.Problem != "recovering"
	}, nil)
	return r
}

// TASK-081 on the wired core: a start from the main window is linked to the lesson in progress in the real schedule
// (one that started 10 minutes ago, so no popup came), and a second start while it records is refused as in progress.
func TestManualStartLinksTheLessonInProgress(t *testing.T) {
	ctx := context.Background()
	e, f := testEnv(t)
	c, popup, b := startPromptCore(t, e, func(c *core) {
		start := time.Now().Add(-10 * time.Minute)
		l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
			Title: "History", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		}}, start)
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlite.NewLessonRepository(c.db).Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	})

	r := startRecordingNow(t, b, "")
	if !r.Started || r.Problem != "" || !r.Linked || r.RecordingID == "" {
		t.Fatalf("StartRecording = %+v, want started and linked", r)
	}
	info, ok := c.recordings.Active()
	if !ok || string(info.ID) != r.RecordingID || info.LessonID != "lesson-1" || info.Status != recording.StatusRecording {
		t.Fatalf("active recording %+v, %v; want RECORDING for the lesson", info, ok)
	}
	if !f.recorder.Running(info.ID) {
		t.Fatal("the capture does not run")
	}
	if popup.Visible() {
		t.Fatal("a start from the main window shows no popup")
	}

	if again, err := b.StartRecording(ctx, ""); err != nil || again.Started || again.Problem != "in_progress" {
		t.Fatalf("second StartRecording = %+v, %v; want in_progress", again, err)
	}
}

// TASK-081: the source picked on the diagnostics page is the remembered source of the popup and the sound check.
func TestSelectSourceIsTheRememberedSource(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	c, _, b := startPromptCore(t, e, nil)

	if err := b.SelectSource(ctx, "zoom.exe"); err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.NewSettingsRepository(c.db).Load(ctx)
	if err != nil || st.RecordingSource() != "zoom.exe" {
		t.Fatalf("settings %v, %v; want zoom.exe remembered", st, err)
	}
	if src, err := b.Sources(ctx); err != nil || src.Remembered != "zoom.exe" {
		t.Fatalf("Sources = %+v, %v; want zoom.exe remembered", src, err)
	}
	page := wails.NewDiagnosticsService(wails.DiagnosticsDeps{Sound: c.ui.Sound, Bundle: c.ui.Bundle}, slog.New(slog.DiscardHandler))
	if got, err := page.StartSoundCheck(ctx); err != nil || got.Source != "zoom.exe" || got.WholeComputer {
		t.Fatalf("StartSoundCheck = %+v, %v; want the check of zoom.exe", got, err)
	}
	waitSound(t, page)
	if err := b.SelectSource(ctx, "zoom"); err == nil {
		t.Fatal("an invalid source must be refused")
	}
}
