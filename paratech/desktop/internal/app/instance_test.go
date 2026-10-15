package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/instancelock"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
)

// startCore opens and starts the core of a, as the headless Run does, and returns what the pages bind to.
func startCore(t *testing.T, a *App) wails.Core {
	t.Helper()
	core, err := a.openCore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	life, ok := a.claimStart(false)
	if !ok {
		t.Fatal("no lifecycle to start")
	}
	if err := life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return core
}

// plantRunningJob stores, through its own connection, an STT job that a live process is running: RUNNING since a
// claim. Its run time is in the future, so the runner of the live instance never claims or parks it itself.
func plantRunningJob(t *testing.T, dataDir string, id shared.JobID) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dataDir, dbFile)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	req, err := values.NewSTTRequest("rec-live")
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(24 * time.Hour)
	j, err := job.New(job.Spec{ID: id, Type: transcribe.JobType, Key: "stt:live", Payload: req.Payload(), MaxAttempts: 3}, later)
	if err != nil {
		t.Fatal(err)
	}
	jobs := sqlite.NewJobRepository(db)
	if _, _, err := jobs.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	filter, err := job.NewClaimFilter([]job.Type{transcribe.JobType}, []job.Type{transcribe.JobType}, true)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := jobs.Claim(ctx, later, filter)
	if err != nil || !ok || claimed.ID() != id {
		t.Fatalf("Claim = %v, %v, %v; want the planted job", claimed, ok, err)
	}
}

func readJob(t *testing.T, dataDir string, id shared.JobID) *job.Job {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(dataDir, dbFile)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	j, err := sqlite.NewJobRepository(db).Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// Test step 3 of TASK-056: a second instance never runs the recovery. A RUNNING job of the live instance would be
// requeued by a second Recover; the lock stops the second instance before it opens the database.
func TestSecondInstanceNeverRecovers(t *testing.T) {
	dir := t.TempDir()
	first, _ := newTestAppWith(t, dir)
	startCore(t, first)
	plantRunningJob(t, dir, "job-live")
	before := readJob(t, dir, "job-live")

	second, _ := newTestAppWith(t, dir)
	_, err := second.openCore(context.Background())
	if !errors.Is(err, instancelock.ErrHeld) {
		t.Fatalf("second openCore = %v, want ErrHeld", err)
	}
	if second.core != nil || second.life != nil {
		t.Fatal("the second instance built a core")
	}
	if _, ok := second.claimStart(false); ok {
		t.Fatal("the second instance has a lifecycle to start")
	}

	after := readJob(t, dir, "job-live")
	if after.Status() != job.StatusRunning || after.Version() != before.Version() || after.Attempts() != before.Attempts() {
		t.Fatalf("live job: %s v%d attempts %d, want it untouched (RUNNING v%d attempts %d)",
			after.Status(), after.Version(), after.Attempts(), before.Version(), before.Attempts())
	}

	// Once the first instance is gone, the next start recovers the job (the crash case).
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, f := newTestAppWith(t, dir)
	f.power.Set(false) // on battery the requeued STT job waits instead of running into its missing recording
	startCore(t, third)
	if got := readJob(t, dir, "job-live"); got.Status() != job.StatusQueued && got.Status() != job.StatusWaitingForPower {
		t.Fatalf("after the first instance stopped, the job is %s: the next start must requeue it", got.Status())
	}
}

// The guard comes first: a held lock leaves a fresh data directory without a database, headless Run included.
func TestHeldLockOpensNothing(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	var dbExisted bool
	a.env.lock = func(dataDir string) (instanceLock, error) {
		_, err := os.Stat(filepath.Join(dataDir, dbFile))
		dbExisted = !errors.Is(err, fs.ErrNotExist)
		return nil, instancelock.ErrHeld
	}

	if _, err := a.openCore(context.Background()); !errors.Is(err, instancelock.ErrHeld) {
		t.Fatalf("openCore = %v, want ErrHeld", err)
	}
	if err := a.Run(context.Background()); !errors.Is(err, instancelock.ErrHeld) {
		t.Fatalf("headless Run = %v, want ErrHeld", err)
	}
	if dbExisted {
		t.Fatal("the database existed when the lock was taken")
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("database file: %v, want none", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// A failed build releases the lock, so the next attempt is not refused.
func TestFailedOpenReleasesTheLock(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	a.env.runner.Workers = 0 // the job runner refuses it
	if _, err := a.openCore(context.Background()); err == nil {
		t.Fatal("openCore with an invalid runner configuration must fail")
	}

	b, _ := newTestAppWith(t, dir)
	if _, err := b.openCore(context.Background()); err != nil {
		t.Fatalf("openCore after a failed one: %v", err)
	}
}
