package tests

// Scenario tests of the "processing complete" notification (PRD section 15, TASK-053) on the real SQLite adapter and
// the real preferences and job runner use cases; only the clock and the notifier are fakes (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/notify"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	transcription "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

var (
	_ notify.NoticeRepository = (*sqlite.NoticeRepository)(nil)
	_ notify.SettingsReader   = (*sqlite.SettingsRepository)(nil)
	_ notify.Notifier         = (*wails.Shell)(nil)
	_ notify.Notifier         = (*testenv.FakeNotifier)(nil)
	_ jobrunner.Observer      = (*notify.Service)(nil)
)

var notifyT0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// notifyTypes are the job types the app lets notify: the user-visible processing steps, not email delivery.
var notifyTypes = []job.Type{
	transcription.STTJobType, values.SummarizeJobType, values.ExtractHomeworkJobType, values.HomeworkDraftJobType,
}

func newNotifyService(t *testing.T, db *sqlite.DB, clock *testenv.FakeClock, n *testenv.FakeNotifier) *notify.Service {
	t.Helper()
	svc, err := notify.New(notify.Deps{
		Settings: sqlite.NewSettingsRepository(db), Notices: sqlite.NewNoticeRepository(db), Notifier: n, Clock: clock,
	}, notify.Config{Types: notifyTypes})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// finishedJob stores a job of type typ and moves it to status (COMPLETED or FAILED) through the aggregate.
func finishedJob(t *testing.T, db *sqlite.DB, id shared.JobID, typ job.Type, status job.Status, now time.Time) job.Snapshot {
	t.Helper()
	ctx, repo := context.Background(), sqlite.NewJobRepository(db)
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: string(id), MaxAttempts: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := repo.Enqueue(ctx, j); err != nil || !created {
		t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
	if err := j.Claim(now); err != nil {
		t.Fatal(err)
	}
	switch status {
	case job.StatusCompleted:
		err = j.Complete(now)
	case job.StatusFailed:
		err = j.Fail(now, "broken input")
	default:
		t.Fatalf("status %s", status)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, j); err != nil {
		t.Fatal(err)
	}
	return j.Snapshot()
}

func jobFinished(t *testing.T, svc *notify.Service, snap job.Snapshot) {
	t.Helper()
	if err := svc.JobFinished(context.Background(), snap); err != nil {
		t.Fatalf("JobFinished %s %s: %v", snap.ID(), snap.Status(), err)
	}
}

// One job state is notified once: three identical events, then a restart of the app with the same event again.
func TestNotificationIsSentOnceAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	clock := testenv.NewFakeClock(notifyT0)

	db := openRunnerDB(t, path)
	before := &testenv.FakeNotifier{}
	svc := newNotifyService(t, db, clock, before)
	snap := finishedJob(t, db, "job-1", transcription.STTJobType, job.StatusCompleted, clock.Now())
	for range 3 {
		jobFinished(t, svc, snap)
		clock.Advance(time.Minute)
	}
	if n := len(before.Calls()); n != 1 {
		t.Fatalf("%d notifications for three identical events, want 1", n)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openRunnerDB(t, path)
	t.Cleanup(func() { _ = db.Close() })
	after := &testenv.FakeNotifier{}
	jobFinished(t, newNotifyService(t, db, clock, after), snap)
	if n := len(after.Calls()); n != 0 {
		t.Fatalf("%d notifications after the restart, want 0: the state was notified before", n)
	}
	got, err := sqlite.NewNoticeRepository(db).Get(context.Background(), "job-1", job.StatusCompleted)
	if err != nil || !got.NotifiedAt().Equal(notifyT0) {
		t.Fatalf("stored notice = %v, %v; want the first one at %v", got, err, notifyT0)
	}
}

// The switch on the settings page decides: off means no notifier call and nothing recorded, so a state that finishes
// while it is off is still notified if it is reported again after the switch is turned on.
func TestDisabledNotificationsReachNoNotifier(t *testing.T) {
	ctx := context.Background()
	db := openRunnerDB(t, filepath.Join(t.TempDir(), "app.db"))
	t.Cleanup(func() { _ = db.Close() })
	clock := testenv.NewFakeClock(notifyT0)
	prefs, err := preferences.New(sqlite.NewSettingsRepository(db), clock)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &testenv.FakeNotifier{}
	svc := newNotifyService(t, db, clock, notifier)

	if _, err := prefs.SetNotifyOnProcessingComplete(ctx, false); err != nil {
		t.Fatal(err)
	}
	completed := finishedJob(t, db, "job-1", values.SummarizeJobType, job.StatusCompleted, clock.Now())
	failed := finishedJob(t, db, "job-2", transcription.STTJobType, job.StatusFailed, clock.Now())
	for range 2 {
		jobFinished(t, svc, completed)
		jobFinished(t, svc, failed)
	}
	if n := len(notifier.Calls()); n != 0 {
		t.Fatalf("%d notifier calls while notifications are off, want 0", n)
	}
	if _, err := sqlite.NewNoticeRepository(db).Get(ctx, "job-1", job.StatusCompleted); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a notice was recorded while off: %v", err)
	}

	if _, err := prefs.SetNotifyOnProcessingComplete(ctx, true); err != nil {
		t.Fatal(err)
	}
	jobFinished(t, svc, completed)
	jobFinished(t, svc, failed)
	calls := notifier.Calls()
	if len(calls) != 2 || calls[0].Job() != "job-1" || calls[1].Job() != "job-2" || calls[1].Status() != job.StatusFailed {
		t.Fatalf("after turning notifications on: %d calls %v, want job-1 COMPLETED and job-2 FAILED", len(calls), calls)
	}
}

// Each job and status is notified on its own; email delivery and cancelled jobs are not notified.
func TestNotificationsPerJobAndStatus(t *testing.T) {
	db := openRunnerDB(t, filepath.Join(t.TempDir(), "app.db"))
	t.Cleanup(func() { _ = db.Close() })
	clock := testenv.NewFakeClock(notifyT0)
	notifier := &testenv.FakeNotifier{}
	svc := newNotifyService(t, db, clock, notifier)

	events := []job.Snapshot{
		finishedJob(t, db, "stt-1", transcription.STTJobType, job.StatusCompleted, clock.Now()),
		finishedJob(t, db, "sum-1", values.SummarizeJobType, job.StatusFailed, clock.Now()),
		finishedJob(t, db, "hw-1", values.ExtractHomeworkJobType, job.StatusCompleted, clock.Now()),
		finishedJob(t, db, "draft-1", values.HomeworkDraftJobType, job.StatusCompleted, clock.Now()),
		finishedJob(t, db, "mail-1", "send_email", job.StatusCompleted, clock.Now()),
	}
	for _, ev := range append(events, events...) {
		jobFinished(t, svc, ev)
	}

	var got []string
	for _, n := range notifier.Calls() {
		got = append(got, fmt.Sprintf("%s %s", n.Job(), n.Status()))
	}
	want := []string{"stt-1 COMPLETED", "sum-1 FAILED", "hw-1 COMPLETED", "draft-1 COMPLETED"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("notifications = %v, want %v", got, want)
	}
}

// The job runner feeds the notification: a job that completes or fails is notified once, retries and the job of a
// type that does not notify are not.
func TestJobRunnerNotifiesFinishedJobs(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	db := openRunnerDB(t, filepath.Join(t.TempDir(), "app.db"))
	clock := testenv.NewFakeClock(notifyT0)
	notifier := &testenv.FakeNotifier{}
	repo := sqlite.NewJobRepository(db)
	cfg := runnerConfig(1)
	gate := newPollGate(clock, cfg.PollInterval)

	runner, err := jobrunner.New(jobrunner.Deps{
		Jobs: repo, Clock: clock, Sleeper: gate, Observer: newNotifyService(t, db, clock, notifier),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runner.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	tries := map[shared.JobID]int{}
	register := func(typ job.Type) {
		t.Helper()
		h := handlerFn(func(_ context.Context, snap job.Snapshot, _ progressFn) error {
			tries[snap.ID()]++ // one worker: no concurrent access
			switch {
			case snap.ID() == "bad":
				return backoff.Permanent(errors.New("unreadable audio"))
			case snap.ID() == "flaky" && tries[snap.ID()] == 1:
				return errors.New("transient")
			}
			return nil
		})
		if err := runner.Register(jobrunner.Registration{Type: typ, Handler: h, Recovery: jobrunner.RecoveryRequeue}); err != nil {
			t.Fatal(err)
		}
	}
	register(transcription.STTJobType)
	register("send_email")

	for _, s := range []struct {
		id  shared.JobID
		typ job.Type
	}{{"ok", transcription.STTJobType}, {"bad", transcription.STTJobType}, {"flaky", transcription.STTJobType}, {"mail", "send_email"}} {
		j, err := job.New(job.Spec{ID: s.id, Type: s.typ, Key: string(s.id), MaxAttempts: 3}, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.Enqueue(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The deadline only turns a hang into a failure; the steps are counted in polls.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tick := func() {
		t.Helper()
		if err := gate.tick(ctx, 2); err != nil { // the worker and the keeper
			t.Fatalf("runner did not go idle: %v", err)
		}
	}
	allFinished := func() bool {
		for _, id := range []shared.JobID{"ok", "bad", "flaky", "mail"} {
			j, err := repo.Get(context.Background(), id)
			if err != nil || !j.Status().IsTerminal() {
				return false
			}
		}
		return true
	}
	// The flaky job needs its backoff (5 s) to elapse; 60 polls are far more than enough.
	for step := 0; !allFinished(); step++ {
		if step == 60 {
			t.Fatal("jobs not finished after 60 polls")
		}
		tick()
	}
	// A few more polls change nothing.
	for range 5 {
		tick()
	}
	// Stop waits for the worker, including an observer call in flight, so every notification is in.
	if err := runner.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := map[string]int{}
	for _, n := range notifier.Calls() {
		got[fmt.Sprintf("%s %s", n.Job(), n.Status())]++
	}
	want := map[string]int{"ok COMPLETED": 1, "bad FAILED": 1, "flaky COMPLETED": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("notifications = %v, want %v", got, want)
	}
}

// pollGate is a runner Sleeper that moves the fake clock only while the runner is idle. A poll sleep (the worker or
// the keeper waiting for the next poll) waits for tick; any other sleep (the attempt and observer bounds) goes to the
// clock. Advancing the clock in real time instead lets the 5 s observer bound expire during a slow SQLite call under
// -race, which cancels the call and loses its notification.
type pollGate struct {
	clock *testenv.FakeClock
	poll  time.Duration

	mu      sync.Mutex
	waiting map[chan struct{}]struct{}
	changed chan struct{} // closed and replaced whenever a poll sleep starts
}

func newPollGate(clock *testenv.FakeClock, poll time.Duration) *pollGate {
	return &pollGate{clock: clock, poll: poll, waiting: map[chan struct{}]struct{}{}, changed: make(chan struct{})}
}

func (g *pollGate) Sleep(ctx context.Context, d time.Duration) error {
	if d != g.poll {
		return g.clock.Sleep(ctx, d)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	wake := make(chan struct{})
	g.mu.Lock()
	g.waiting[wake] = struct{}{}
	close(g.changed)
	g.changed = make(chan struct{})
	g.mu.Unlock()

	select {
	case <-wake:
		return nil
	case <-ctx.Done():
		g.mu.Lock()
		delete(g.waiting, wake)
		g.mu.Unlock()
		return ctx.Err()
	}
}

// tick waits until n goroutines sleep for a poll, then advances the clock by one poll interval and wakes them. With
// every runner goroutine in a poll sleep, no attempt or observer call is in flight, so no bound sees the clock move.
func (g *pollGate) tick(ctx context.Context, n int) error {
	for {
		g.mu.Lock()
		if len(g.waiting) >= n {
			g.clock.Advance(g.poll)
			for wake := range g.waiting {
				close(wake)
				delete(g.waiting, wake)
			}
			g.mu.Unlock()
			return nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
