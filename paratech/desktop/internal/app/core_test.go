package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/transcribe"
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

// The UI-mode core: with a notifier, the job runner reports finished jobs to the notification use case. An STT job
// whose pinned model is not installed fails at once, and the user is notified exactly once.
func TestBuildCoreWithNotifierNotifiesFinishedJobs(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	notifier := &testenv.FakeNotifier{}
	log := slog.New(slog.DiscardHandler)

	c, err := buildCore(ctx, t.TempDir(), e, notifier, log)
	if err != nil {
		t.Fatal(err)
	}
	life := c.newLifecycle(log, e.stopTimeout, closer{name: "storage", close: c.close})
	t.Cleanup(func() { // before TempDir removal: Windows cannot delete open files
		if err := life.shutdown(); err != nil {
			t.Error(err)
		}
	})
	if err := life.start(ctx); err != nil {
		t.Fatal(err)
	}

	a := &App{core: c} // the helpers read only the core
	rec := completedRecording(t, a, "rec-1")
	enqueueSTT(t, a, "job-stt", rec, "m-gone") // a permanent failure: the model was removed

	jobs := sqlite.NewJobRepository(c.db)
	var failed *job.Job
	testenv.Eventually(t, "the STT job fails", func() bool {
		j, err := jobs.Get(ctx, "job-stt")
		if err != nil {
			t.Fatal(err)
		}
		failed = j
		return j.Status() == job.StatusFailed
	}, nil)
	if failed.LastError() != "permanent failure: "+transcribe.ErrModelNotInstalled.Error() || failed.Attempts() != 1 {
		t.Fatalf("job error %q after %d attempts, want the missing model on the first attempt", failed.LastError(),
			failed.Attempts())
	}
	testenv.Eventually(t, "the user is notified", func() bool { return len(notifier.Calls()) > 0 }, nil)

	// After the stop no later event can add a call.
	if err := life.shutdown(); err != nil {
		t.Fatal(err)
	}
	calls := notifier.Calls()
	if len(calls) != 1 || calls[0].Job() != "job-stt" || calls[0].Type() != transcribe.JobType ||
		calls[0].Status() != job.StatusFailed {
		t.Fatalf("notifications %+v, want one for the failed STT job", calls)
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
