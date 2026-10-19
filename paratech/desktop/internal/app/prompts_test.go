package app

// The recording prompt on the wired UI-mode core: real SQLite, the real scheduler, recording session and bindings;
// only the popup window and the recorder are fake (ARCH-61).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// buildPromptCore builds the core as UI mode does, with a fake popup as the window, and its lifecycle (not started).
func buildPromptCore(t *testing.T, e env) (*core, *lifecycle, *testenv.FakePopup, *wails.PromptService) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	c, err := buildCore(context.Background(), t.TempDir(), testProfile, e, log)
	if err != nil {
		t.Fatal(err)
	}
	popup := &testenv.FakePopup{}
	if err := c.wirePrompts(e, popup, log); err != nil {
		_ = c.close()
		t.Fatal(err)
	}
	life := c.newLifecycle(log, e.stopTimeout, closer{name: "storage", close: c.close})
	t.Cleanup(func() { // before TempDir removal: Windows cannot delete open files
		if err := life.shutdown(); err != nil {
			t.Error(err)
		}
	})
	return c, life, popup, wails.NewPromptService(c.ui.Prompt, log)
}

// startPromptCore builds the UI-mode core and starts it like the Started hook.
func startPromptCore(t *testing.T, e env, beforeStart func(c *core)) (*core, *testenv.FakePopup, *wails.PromptService) {
	t.Helper()
	c, life, popup, b := buildPromptCore(t, e)
	if beforeStart != nil {
		beforeStart(c)
	}
	if err := life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, popup, b
}

// startFromPopup clicks "Start recording" until the startup recovery has marked the interrupted recordings (a short
// wait right after the start) and returns the final answer.
func startFromPopup(t *testing.T, b *wails.PromptService, id string) wails.PromptView {
	t.Helper()
	var v wails.PromptView
	testenv.Eventually(t, "a start past the recovery", func() bool {
		var err error
		if v, err = b.Start(context.Background(), id, ""); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return v.Problem != "recovering"
	}, nil)
	return v
}

// Test step 3, AC-01: a lesson in the local schedule raises the popup through the scheduler, and "Start recording"
// takes the recording session through STARTING to RECORDING for that lesson; the popup hides.
func TestScheduledLessonPromptStartsARecording(t *testing.T) {
	ctx := context.Background()
	e, f := testEnv(t)
	c, popup, b := startPromptCore(t, e, func(c *core) {
		// The popup comes at the start (DefaultLead 0, TASK-076). Taken just before the core starts, so the first
		// tick falls within lesson.StartLateGrace; a future start would miss it and wait a whole poll interval.
		start := time.Now()
		l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
			Title: "History", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlite.NewLessonRepository(c.db).Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	})

	testenv.Eventually(t, "the scheduler shows the popup", popup.Visible, nil)
	cur := b.Current()
	if cur == nil || cur.Kind != "lesson" || cur.Title != "History" || cur.State != string(prompt.StateOffered) {
		t.Fatalf("current prompt %+v", cur)
	}

	v := startFromPopup(t, b, cur.ID)
	if v.State != string(prompt.StateStarted) || v.Problem != "" {
		t.Fatalf("Start = %+v, want STARTED", v)
	}
	if popup.Visible() || b.Current() != nil {
		t.Fatal("the popup must hide and the prompt end after the start")
	}

	info, ok := c.recordings.Active()
	if !ok || info.LessonID != "lesson-1" || info.Status != recording.StatusRecording {
		t.Fatalf("active recording %+v, %v; want RECORDING for the lesson", info, ok)
	}
	rec, err := sqlite.NewRecordingRepository(c.db).Get(ctx, info.ID)
	if err != nil || rec.Status() != recording.StatusRecording || rec.LessonID() != "lesson-1" {
		t.Fatalf("stored recording %v, %v", rec, err)
	}
	if !f.recorder.Running(info.ID) {
		t.Fatal("the capture does not run")
	}

	// TASK-076: «Записи и задачи» titles the row by the lesson, not by its start time.
	rows, err := wails.NewActivityService(c.ui.Activity, c.ui.Stopper, c.ui.Forcer, c.ui.Retrier, slog.New(slog.DiscardHandler)).
		ListRecordings(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListRecordings = %+v, %v; want the one recording", rows, err)
	}
	if r := rows[0]; r.ID != string(info.ID) || r.LessonID != "lesson-1" || r.Title != "History" || r.Stage != "recording" {
		t.Fatalf("row %+v, want the recording titled History in the recording stage", r)
	}
}

// With the recorder of a build without capture the popup stays up with the "unavailable" problem, the app keeps
// running, and the user can retry or skip.
func TestPromptStartWithoutARecorderShowsUnavailable(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	e.recorder = func(string, *filestore.Store, *slog.Logger) (recordingsession.Recorder, error) {
		return audiocapture.Unavailable{}, nil
	}
	c, popup, b := startPromptCore(t, e, nil)

	if _, err := c.prompts.OfferAdHoc(ctx); err != nil { // the tray's "Show recording popup"
		t.Fatal(err)
	}
	cur := b.Current()
	if cur == nil || cur.Kind != "adhoc" || !popup.Visible() {
		t.Fatalf("current prompt %+v, popup visible %v", cur, popup.Visible())
	}

	for range 2 { // a retry fails the same way
		v := startFromPopup(t, b, cur.ID)
		if v.State != string(prompt.StateOffered) || v.Problem != "unavailable" {
			t.Fatalf("Start = %+v, want OFFERED with the unavailable problem", v)
		}
		if !popup.Visible() {
			t.Fatal("the popup must stay up after a failed start")
		}
	}
	if _, ok := c.recordings.Active(); ok {
		t.Fatal("no recording may be in progress")
	}
	recs, err := c.ui.Activity.ListRecordings(ctx)
	if err != nil || len(recs) != 2 || recs[0].Status != recording.StatusFailed || recs[1].Status != recording.StatusFailed {
		t.Fatalf("recordings %+v, %v; want two FAILED starts", recs, err)
	}

	if v, err := b.Skip(ctx, cur.ID); err != nil || v.State != string(prompt.StateSkipped) || popup.Visible() {
		t.Fatalf("Skip = %+v, %v, visible %v", v, err, popup.Visible())
	}
}

// A start that fails before the recording recovery runs closes the recordings: the window stays (UI mode) and
// "Start recording" fails at once instead of answering "recovering" for good.
func TestPromptStartAfterAFailedStartFails(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	c, life, popup, b := buildPromptCore(t, e)
	life.runner = &fakeRunner{ev: &events{}, recoverErr: errors.New("database busy")}
	if err := life.start(ctx); err == nil {
		t.Fatal("start must fail")
	}

	if _, err := c.prompts.OfferAdHoc(ctx); err != nil {
		t.Fatal(err)
	}
	cur := b.Current()
	if cur == nil || !popup.Visible() {
		t.Fatalf("current prompt %+v, popup visible %v", cur, popup.Visible())
	}
	v, err := b.Start(ctx, cur.ID, "")
	if err != nil || v.State != string(prompt.StateOffered) || v.Problem != "failed" {
		t.Fatalf("Start = %+v, %v; want OFFERED with the failed problem", v, err)
	}
	if _, ok := c.recordings.Active(); ok {
		t.Fatal("no recording may be in progress")
	}
}

// Headless mode has no popup: neither the prompt nor the scheduler is wired, so no announcement is used up unseen.
func TestHeadlessCoreHasNoPrompt(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	core, err := a.openCore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if core.Prompt != nil || a.core.scheduler != nil || a.core.prompts != nil || len(a.life.loops) != 0 {
		t.Fatalf("headless core wires the prompt: %+v, loops %d", core, len(a.life.loops))
	}
}

// UI mode wires the prompt with the shell as its popup and runs both loops.
func TestUICoreWiresThePrompt(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.env, _ = testEnv(t)
	t.Cleanup(func() { _ = a.Close() })

	core, err := a.openCore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if core.Prompt == nil || core.Prompt != a.core.prompts || a.core.scheduler == nil {
		t.Fatal("UI mode must wire the prompt and the scheduler")
	}
	if len(a.life.loops) != 2 {
		t.Fatalf("%d loops, want the scheduler and the prompt", len(a.life.loops))
	}
}
