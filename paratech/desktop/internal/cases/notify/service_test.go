package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

const (
	typeStt  job.Type = "stt"
	typeMail job.Type = "send_email"
)

// fakeSettings returns settings with the notification switch as configured.
type fakeSettings struct {
	mu     sync.Mutex
	notify bool
	err    error
	stored bool // false: the defaults of a fresh installation
}

func (f *fakeSettings) Load(context.Context) (*settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if !f.stored {
		return settings.New(), nil
	}
	return settings.Restore(settings.State{Version: 1, NotifyOnProcessingComplete: f.notify, UpdatedAt: t0})
}

func (f *fakeSettings) set(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored, f.notify = true, on
}

// memNotices is an in-memory NoticeRepository with the at-most-once contract of the sqlite adapter.
type memNotices struct {
	mu   sync.Mutex
	rows map[string]notice.State
	err  error
}

func (m *memNotices) Record(_ context.Context, n *notice.Notice) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	if m.rows == nil {
		m.rows = map[string]notice.State{}
	}
	key := string(n.Job()) + "/" + string(n.Status())
	if _, ok := m.rows[key]; ok {
		return false, nil
	}
	m.rows[key] = n.State()
	return true, nil
}

func (m *memNotices) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

type env struct {
	settings *fakeSettings
	notices  *memNotices
	notifier *testenv.FakeNotifier
	clock    *testenv.FakeClock
	svc      *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{settings: &fakeSettings{}, notices: &memNotices{}, notifier: &testenv.FakeNotifier{}, clock: testenv.NewFakeClock(t0)}
	svc, err := New(Deps{Settings: e.settings, Notices: e.notices, Notifier: e.notifier, Clock: e.clock},
		Config{Types: []job.Type{typeStt}})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// snapshot returns a stored job of type typ in status.
func snapshot(t *testing.T, id shared.JobID, typ job.Type, status job.Status) job.Snapshot {
	t.Helper()
	st := job.State{
		Version: 2, ID: id, Type: typ, Status: status, Key: string(id), Payload: "{}", Attempts: 1, MaxAttempts: 3,
		NextRunAt: t0, CreatedAt: t0, StartedAt: t0, UpdatedAt: t0,
	}
	if status.IsTerminal() {
		st.FinishedAt = t0
	}
	if status == job.StatusQueued || status.IsWaiting() {
		st.StartedAt = time.Time{}
	}
	j, err := job.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	return j.Snapshot()
}

func TestNewValidates(t *testing.T) {
	ok := Deps{Settings: &fakeSettings{}, Notices: &memNotices{}, Notifier: &testenv.FakeNotifier{}, Clock: testenv.NewFakeClock(t0)}
	tests := []struct {
		name string
		edit func(*Deps, *Config)
	}{
		{"no settings", func(d *Deps, _ *Config) { d.Settings = nil }},
		{"no notices", func(d *Deps, _ *Config) { d.Notices = nil }},
		{"no notifier", func(d *Deps, _ *Config) { d.Notifier = nil }},
		{"no clock", func(d *Deps, _ *Config) { d.Clock = nil }},
		{"no job types", func(_ *Deps, c *Config) { c.Types = nil }},
		{"blank job type", func(_ *Deps, c *Config) { c.Types = []job.Type{""} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, c := ok, Config{Types: []job.Type{typeStt}}
			tt.edit(&d, &c)
			if _, err := New(d, c); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := New(ok, Config{Types: []job.Type{typeStt}}); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

// TestThreeIdenticalEventsNotifyOnce is test step 2 of TASK-053: three identical state events, one notification.
func TestThreeIdenticalEventsNotifyOnce(t *testing.T) {
	e := newEnv(t)
	snap := snapshot(t, "job-1", typeStt, job.StatusCompleted)
	for i := range 3 {
		if err := e.svc.JobFinished(context.Background(), snap); err != nil {
			t.Fatalf("event %d: %v", i+1, err)
		}
		e.clock.Advance(time.Minute)
	}

	calls := e.notifier.Calls()
	if len(calls) != 1 {
		t.Fatalf("%d notifications, want 1", len(calls))
	}
	n := calls[0]
	if n.Job() != "job-1" || n.Type() != typeStt || n.Status() != job.StatusCompleted || !n.NotifiedAt().Equal(t0) {
		t.Fatalf("notice = %+v, want job-1 stt COMPLETED at the first event", n.State())
	}
}

// TestDisabledNotificationsNeverCallTheNotifier is test step 3 of TASK-053.
func TestDisabledNotificationsNeverCallTheNotifier(t *testing.T) {
	e := newEnv(t)
	e.settings.set(false)
	for _, s := range []job.Status{job.StatusCompleted, job.StatusFailed, job.StatusCompleted} {
		if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", typeStt, s)); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.notifier.Calls()); n != 0 {
		t.Fatalf("%d notifier calls while disabled, want 0", n)
	}
	if n := e.notices.count(); n != 0 {
		t.Fatalf("%d notices recorded while disabled, want 0: turning it on later must still notify", n)
	}

	// Turned on again: the same state now notifies, because nothing was recorded while off.
	e.settings.set(true)
	if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", typeStt, job.StatusCompleted)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.notifier.Calls()); n != 1 {
		t.Fatalf("%d notifier calls after enabling, want 1", n)
	}
}

func TestDefaultSettingsNotify(t *testing.T) {
	e := newEnv(t) // nothing stored: settings.DefaultNotifyOnProcessingComplete
	if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", typeStt, job.StatusFailed)); err != nil {
		t.Fatal(err)
	}
	if got, want := len(e.notifier.Calls()) == 1, settings.DefaultNotifyOnProcessingComplete; got != want {
		t.Fatalf("notified = %v, want the default %v", got, want)
	}
}

func TestOnlyNotifiableEventsNotify(t *testing.T) {
	tests := []struct {
		name   string
		typ    job.Type
		status job.Status
		want   bool
	}{
		{"completed", typeStt, job.StatusCompleted, true},
		{"failed", typeStt, job.StatusFailed, true},
		{"cancelled by the user", typeStt, job.StatusCancelled, false},
		{"still running", typeStt, job.StatusRunning, false},
		{"waiting for power", typeStt, job.StatusWaitingForPower, false},
		{"retry wait", typeStt, job.StatusRetryWait, false},
		{"type not configured", typeMail, job.StatusCompleted, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", tt.typ, tt.status)); err != nil {
				t.Fatal(err)
			}
			if got := len(e.notifier.Calls()) == 1; got != tt.want {
				t.Fatalf("notified = %v, want %v", got, tt.want)
			}
			if got := e.notices.count() == 1; got != tt.want {
				t.Fatalf("recorded = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEachJobAndStatusNotifiesOnce(t *testing.T) {
	e := newEnv(t)
	events := []job.Snapshot{
		snapshot(t, "job-1", typeStt, job.StatusCompleted),
		snapshot(t, "job-2", typeStt, job.StatusFailed),
		snapshot(t, "job-1", typeStt, job.StatusCompleted),
		snapshot(t, "job-2", typeStt, job.StatusFailed),
		snapshot(t, "job-3", typeStt, job.StatusCompleted),
	}
	for _, ev := range events {
		if err := e.svc.JobFinished(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, n := range e.notifier.Calls() {
		got = append(got, string(n.Job())+" "+string(n.Status()))
	}
	want := []string{"job-1 COMPLETED", "job-2 FAILED", "job-3 COMPLETED"}
	if len(got) != len(want) {
		t.Fatalf("notifications = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("notifications = %v, want %v", got, want)
		}
	}
}

func TestConcurrentEventsNotifyOnce(t *testing.T) {
	e := newEnv(t)
	snap := snapshot(t, "job-1", typeStt, job.StatusCompleted)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := e.svc.JobFinished(context.Background(), snap); err != nil {
				t.Errorf("JobFinished: %v", err)
			}
		})
	}
	wg.Wait()
	if n := len(e.notifier.Calls()); n != 1 {
		t.Fatalf("%d notifications, want 1", n)
	}
}

func TestNotifierErrorIsReturnedAndNotRetried(t *testing.T) {
	e := newEnv(t)
	boom := errors.New("toast failed")
	e.notifier.Fail(boom)
	snap := snapshot(t, "job-1", typeStt, job.StatusCompleted)

	if err := e.svc.JobFinished(context.Background(), snap); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the notifier error", err)
	}
	e.notifier.Fail(nil)
	if err := e.svc.JobFinished(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := len(e.notifier.Calls()); n != 1 {
		t.Fatalf("%d notifier calls, want 1: the notice is recorded before the call, so a failure is not repeated", n)
	}
}

func TestNotReadyNotifierRecordsNothing(t *testing.T) {
	e := newEnv(t)
	snap := snapshot(t, "job-1", typeStt, job.StatusCompleted)
	e.notifier.SetReady(false)
	if err := e.svc.JobFinished(context.Background(), snap); err != nil {
		t.Fatalf("not ready: %v", err)
	}
	if len(e.notifier.Calls()) != 0 || e.notices.count() != 0 {
		t.Fatalf("not ready: %d notifier calls, %d notices; want none", len(e.notifier.Calls()), e.notices.count())
	}

	// Ready later (the shell started): the same state notifies, once.
	e.notifier.SetReady(true)
	for range 2 {
		if err := e.svc.JobFinished(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.notifier.Calls()); n != 1 || e.notices.count() != 1 {
		t.Fatalf("ready: %d notifier calls, %d notices; want 1 and 1", n, e.notices.count())
	}
}

// Events the policy never notifies about must not cost a settings read (most finished jobs are such).
func TestUnconcernedEventsSkipSettings(t *testing.T) {
	for _, snap := range []job.Snapshot{
		snapshot(t, "job-1", typeStt, job.StatusCancelled),
		snapshot(t, "job-2", typeMail, job.StatusCompleted),
	} {
		e := newEnv(t)
		e.settings.err = errors.New("settings must not be read")
		if err := e.svc.JobFinished(context.Background(), snap); err != nil {
			t.Fatalf("%s %s: %v", snap.Type(), snap.Status(), err)
		}
	}
}

func TestStorageErrorsNotifyNothing(t *testing.T) {
	boom := errors.New("disk")
	t.Run("settings", func(t *testing.T) {
		e := newEnv(t)
		e.settings.err = boom
		if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", typeStt, job.StatusCompleted)); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if len(e.notifier.Calls()) != 0 || e.notices.count() != 0 {
			t.Fatal("notified or recorded without settings")
		}
	})
	t.Run("notices", func(t *testing.T) {
		e := newEnv(t)
		e.notices.err = boom
		if err := e.svc.JobFinished(context.Background(), snapshot(t, "job-1", typeStt, job.StatusCompleted)); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if len(e.notifier.Calls()) != 0 {
			t.Fatal("notified although the notice was not recorded")
		}
	})
}
