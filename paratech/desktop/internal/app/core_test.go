package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/testenv"
)

func newTestApp(t *testing.T, dataDir string) *App {
	t.Helper()
	a, _ := newTestAppWith(t, dataDir)
	return a
}

// newTestAppWith is a headless app on the test environment, closed when the test ends.
func newTestAppWith(t *testing.T, dataDir string) (*App, *fakes) {
	t.Helper()
	cfg := config.Default()
	cfg.Headless = true
	cfg.DataDir = dataDir
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var f *fakes
	a.env, f = testEnv(t)
	t.Cleanup(func() { _ = a.Close() }) // before TempDir removal: Windows cannot delete open files
	return a, f
}

// The settings page path end to end without a window: the core the shell binds to stores toggles in SQLite, and
// they survive a restart.
func TestOpenCoreStoresSettings(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data") // created by openCore

	a := newTestApp(t, dir)
	core, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.openCore(ctx); err == nil {
		t.Error("a second openCore must fail")
	}
	if _, err := core.Settings.SetProcessOnlyOnACPower(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	b := newTestApp(t, dir)
	core, err = b.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := core.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessOnlyOnACPower || !got.RecordingPromptsEnabled {
		t.Fatalf("after restart: %+v, want AC restriction off and prompts on (default)", got)
	}
}

// The recordings and jobs page reads SQLite through the core, and its actions reach the recording session and the
// job runner.
func TestOpenCoreListsActivity(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, t.TempDir())
	core, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if core.Activity == nil || core.Stopper == nil || core.Forcer == nil {
		t.Fatalf("core = %+v: want the activity use case and both actions", core)
	}
	if core.Stopper != a.core.recordings || core.Forcer != a.core.runner {
		t.Fatal("the actions must be the recording session and the job runner")
	}

	j, err := job.New(job.Spec{ID: "j1", Type: "stt", Key: "stt:r1", MaxAttempts: 3}, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sqlite.NewJobRepository(a.core.db).Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	jobs, err := core.Activity.ListJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "j1" {
		t.Fatalf("ListJobs = %+v, %v; want the stored job", jobs, err)
	}
	recs, err := core.Activity.ListRecordings(ctx)
	if err != nil || len(recs) != 0 {
		t.Fatalf("ListRecordings = %+v, %v; want none", recs, err)
	}
}

// TASK-065, test step 3 (ADR-0009 "No completion notifications"): on the wired UI-mode core processing jobs that fail
// (an STT job pinned to a removed model) and complete (an STT job and its Hub follow-ups) reach no user-visible surface.
// The popup is the only one the shell gives the core; it is never shown or hidden.
func TestFinishedJobsShowNothing(t *testing.T) {
	ctx := context.Background()
	e, _, _, hubURL := pipelineEnv(t)
	u := openUICore(t, t.TempDir(), e)
	configureHub(t, u.c, time.Now(), hubURL)
	u.start(t)
	// Stored after the start, so the startup sweep enqueues no STT job of its own for the recordings.
	model := installModel(t, u.app, "m1", testenv.WhisperOK)
	enqueueSTT(t, u.app, "job-fails", completedRecording(t, u.app, "rec-1"), "m-gone")
	enqueueSTT(t, u.app, "job-completes", completedRecording(t, u.app, "rec-2"), model)

	jobs := sqlite.NewJobRepository(u.c.db)
	want := map[job.Type]job.Status{
		homeworkflow.SummarizeJobType: job.StatusCompleted, homeworkflow.ExtractHomeworkJobType: job.StatusCompleted,
	}
	var all []*job.Job
	testenv.Eventually(t, "every job ends", func() bool {
		var err error
		if all, _, err = jobs.ListActivity(ctx, 100); err != nil {
			t.Fatal(err)
		}
		return len(all) == 4 && !slices.ContainsFunc(all, func(j *job.Job) bool { return !j.Status().IsTerminal() })
	}, nil)
	// After the stop no later event can reach the popup.
	if err := u.life.shutdown(); err != nil {
		t.Fatal(err)
	}

	for _, j := range all {
		status, ok := want[j.Type()]
		switch {
		case j.ID() == "job-fails":
			status, ok = job.StatusFailed, true
		case j.ID() == "job-completes":
			status, ok = job.StatusCompleted, true
		}
		if !ok || j.Status() != status {
			t.Errorf("job %s (%s) ended %s, want %s", j.ID(), j.Type(), j.Status(), status)
		}
	}
	if shows, hides := u.popup.Shows(), u.popup.Hides(); shows != 0 || hides != 0 || u.prompt.Current() != nil {
		t.Fatalf("the popup got %d shows and %d hides, prompt %+v; want nothing user-visible", shows, hides,
			u.prompt.Current())
	}
}

// The log never names the data directory: it lies below the user's profile, and logs go into the support bundle.
func TestLogOmitsTheDataDir(t *testing.T) {
	const marker = "data-dir-marker"
	var buf syncBuffer
	a, _ := newTestAppWith(t, filepath.Join(t.TempDir(), marker))
	a.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	startCore(t, a)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); out == "" || strings.Contains(out, marker) {
		t.Fatalf("log names the data directory or is empty:\n%s", out)
	}
}

// syncBuffer is a log sink for the background workers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestOpenCoreNeedsDataDir(t *testing.T) {
	if _, err := newTestApp(t, "").openCore(context.Background()); err == nil {
		t.Fatal("openCore without a data directory must fail")
	}
}
