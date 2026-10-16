package tests

// Scenario tests of the calendar synchronization (TASK-051; PRD sections 11.3 and 14 case 8, AC-17, AC-20) on the real
// SQLite adapter, the real CSV parser, the real schedulesync use cases and the real googlecal adapter against the fake
// Calendar API (testenv.FakeGoogleCalendar). Only the Google world, the authorization and the clock are fakes (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ schedulesync.CalendarAdapter = (*googlecal.Client)(nil)
	_ schedulesync.JobEnqueuer     = (*sqlite.JobRepository)(nil)
	_ googlecal.Store              = (*testenv.MemCredentials)(nil)
	_ jobrunner.Handler            = (*schedulesync.CalendarSync)(nil)
)

var calT0 = time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

// errFakeNotSignedIn is what the fake authorization's transport returns without a sign-in.
var errFakeNotSignedIn = errors.New("fake auth: not signed in")

// fakeCalendarAuth is the googlecal.Auth of the scenarios: the fake calendar's bearer token while signed in.
type fakeCalendarAuth struct {
	mu       sync.Mutex
	signedIn bool
}

func (a *fakeCalendarAuth) set(signedIn bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.signedIn = signedIn
}

func (a *fakeCalendarAuth) SignedIn(context.Context) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.signedIn, nil
}

func (a *fakeCalendarAuth) HTTPClient() *http.Client { return &http.Client{Transport: a} }
func (a *fakeCalendarAuth) ForgetAccessToken()       {}

func (a *fakeCalendarAuth) RoundTrip(req *http.Request) (*http.Response, error) {
	if ok, _ := a.SignedIn(req.Context()); !ok {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errFakeNotSignedIn
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+testenv.FakeCalendarAccessToken)
	return http.DefaultTransport.RoundTrip(r)
}

func classifyFakeAuth(err error) googlecal.AuthFailure {
	if errors.Is(err, errFakeNotSignedIn) {
		return googlecal.AuthNotSignedIn
	}
	return googlecal.AuthNone
}

// calendarPort maps the adapter's sign-in and lesson-only classes to the use case's sentinels, like the app's bridge.
type calendarPort struct{ c *googlecal.Client }

func calendarPortError(err error) error {
	switch {
	case errors.Is(err, googlecal.ErrReauthRequired):
		return fmt.Errorf("%w: %w", schedulesync.ErrCalendarSignInExpired, err)
	case errors.Is(err, googlecal.ErrNotSignedIn):
		return fmt.Errorf("%w: %w", schedulesync.ErrCalendarNotConnected, err)
	}
	return err
}

func lessonPortError(err error) error {
	err = calendarPortError(err)
	switch {
	case errors.Is(err, schedulesync.ErrCalendarNotConnected):
		return err
	case errors.Is(err, googlecal.ErrInvalidRequest), errors.Is(err, googlecal.ErrRejected),
		errors.Is(err, googlecal.ErrConflict), errors.Is(err, googlecal.ErrNotRestored):
		return fmt.Errorf("%w: %w", schedulesync.ErrLessonRejected, err)
	}
	return err
}

func (p calendarPort) EnsureCalendar(ctx context.Context) (string, bool, error) {
	id, fresh, err := p.c.EnsureCalendar(ctx)
	return id, fresh, calendarPortError(err)
}

func (p calendarPort) ConfirmCalendar(ctx context.Context, id string) error {
	return calendarPortError(p.c.ConfirmCalendar(ctx, id))
}

func (p calendarPort) UpsertLesson(ctx context.Context, id string, l *lesson.Lesson) error {
	return lessonPortError(p.c.UpsertLesson(ctx, id, l))
}

func (p calendarPort) DeleteEvent(ctx context.Context, id string, lessonID shared.LessonID) error {
	return lessonPortError(p.c.DeleteEvent(ctx, id, lessonID))
}

type calIDs struct {
	mu          sync.Mutex
	lessons, js int
}

func (g *calIDs) NewLessonID() shared.LessonID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lessons++
	return shared.LessonID(fmt.Sprintf("lesson-%d", g.lessons))
}

func (g *calIDs) NewJobID() shared.JobID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.js++
	return shared.JobID(fmt.Sprintf("job-%d", g.js))
}

type calendarEnv struct {
	t       *testing.T
	db      *sqlite.DB
	lessons *sqlite.LessonRepository
	jobs    *sqlite.JobRepository
	clock   *testenv.FakeClock
	google  *testenv.FakeGoogleCalendar
	auth    *fakeCalendarAuth
	store   *testenv.MemCredentials
	sync    *schedulesync.CalendarSync
	imports *schedulesync.Service
}

func newCalendarEnv(t *testing.T) *calendarEnv {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &calendarEnv{
		t: t, db: db, lessons: sqlite.NewLessonRepository(db), jobs: sqlite.NewJobRepository(db),
		clock: testenv.NewFakeClock(calT0), google: testenv.StartFakeGoogleCalendar(t), auth: &fakeCalendarAuth{signedIn: true},
		store: testenv.NewMemCredentials(),
	}
	client, err := googlecal.New(e.auth, e.store, classifyFakeAuth, googlecal.WithBaseURL(e.google.BaseURL()),
		googlecal.WithRequestTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ids := &calIDs{}
	e.sync, err = schedulesync.NewCalendarSync(schedulesync.CalendarDeps{
		Lessons: e.lessons, Calendar: calendarPort{client}, Account: e.auth, Jobs: e.jobs, Clock: e.clock, IDs: ids, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.imports, err = schedulesync.New(schedulesync.Deps{
		Lessons: e.lessons, Parsers: map[importplan.Source]schedulesync.ScheduleParser{importplan.SourceCSV: csvschedule.New()},
		Clock: e.clock, IDs: ids, Tx: db, Sync: e.sync,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if n := e.google.ForeignRequests(); n != 0 {
			t.Errorf("%d requests named a calendar the app did not create (AC-17)", n)
		}
	})
	return e
}

const calCSVHeader = "title,date,start,end,timezone,uid\n"

func (e *calendarEnv) importCSV(rows ...string) schedulesync.CommitResult {
	e.t.Helper()
	ctx := context.Background()
	data := []byte(calCSVHeader + strings.Join(rows, "\n") + "\n")
	p, err := e.imports.PreviewImport(ctx, schedulesync.ImportRequest{Source: importplan.SourceCSV, Data: data})
	if err != nil || !p.Plan.Valid() {
		e.t.Fatalf("PreviewImport = %+v, %v", p.Plan.Errors, err)
	}
	e.clock.Advance(time.Minute)
	res, err := e.imports.CommitImport(ctx, schedulesync.CommitRequest{Source: importplan.SourceCSV, Data: data, ConfirmToken: p.ConfirmToken})
	if err != nil {
		e.t.Fatalf("CommitImport = %v", err)
	}
	return res
}

func (e *calendarEnv) handle() error {
	e.t.Helper()
	j, err := job.New(job.Spec{ID: "run", Type: schedulesync.CalendarSyncJobType, Key: "calendar_sync:run", MaxAttempts: 5}, calT0)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.sync.Handle(context.Background(), j.Snapshot(), nil)
}

func (e *calendarEnv) mustSync() {
	e.t.Helper()
	if err := e.handle(); err != nil {
		e.t.Fatalf("sync = %v", err)
	}
}

// appCalendar returns the only live app calendar.
func (e *calendarEnv) appCalendar() testenv.FakeCalendarInfo {
	e.t.Helper()
	cals := e.google.Calendars()
	if len(cals) != 1 || cals[0].Summary != googlecal.CalendarSummary {
		e.t.Fatalf("calendars = %+v, want one «%s»", cals, googlecal.CalendarSummary)
	}
	return cals[0]
}

func (e *calendarEnv) pending() int {
	e.t.Helper()
	ls, err := e.lessons.ListPendingCalendarSync(context.Background(), 10000)
	if err != nil {
		e.t.Fatal(err)
	}
	return len(ls)
}

func summaries(events []testenv.FakeCalendarEvent) map[string]string {
	out := map[string]string{}
	for _, ev := range events {
		out[ev.ID] = ev.Summary
	}
	return out
}

// Test step 2 of TASK-051: two sync runs in a row give the same events (no duplicates), and the second writes nothing.
func TestCalendarSyncTwiceGivesTheSameEvents(t *testing.T) {
	e := newCalendarEnv(t)
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1", "Physics,2026-10-20,11:00,12:30,Europe/Moscow,p1",
		"History,2026-10-21,09:00,10:30,Europe/Moscow,h1")

	e.mustSync()
	cal := e.appCalendar()
	first := e.google.Events(cal.ID)
	if len(first) != 3 || e.pending() != 0 {
		t.Fatalf("after the first sync: %d events, %d pending", len(first), e.pending())
	}
	writes := len(e.google.Requests())

	e.mustSync()
	second := e.google.Events(cal.ID)
	if len(second) != 3 || fmt.Sprint(summaries(first)) != fmt.Sprint(summaries(second)) {
		t.Fatalf("after the second sync: %+v, want %+v", second, first)
	}
	for _, r := range e.google.Requests()[writes:] {
		if r.Method != http.MethodGet {
			t.Fatalf("the second sync wrote: %s %s", r.Method, r.Path)
		}
	}
	if len(e.google.Calendars()) != 1 {
		t.Fatalf("calendars = %+v", e.google.Calendars())
	}
}

// Test step 3 of TASK-051: a changed lesson updates its event instead of creating a new one; a removed lesson loses
// its event and its row.
func TestCalendarSyncChangedLessonUpdatesTheEvent(t *testing.T) {
	ctx := context.Background()
	e := newCalendarEnv(t)
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1", "Physics,2026-10-20,11:00,12:30,Europe/Moscow,p1")
	e.mustSync()
	cal := e.appCalendar()
	live, err := e.lessons.ListLive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mathEvent := googlecal.LessonEventID(live[0].ID())
	physicsEvent := googlecal.LessonEventID(live[1].ID())

	if res := e.importCSV("Algebra,2026-10-20,13:00,14:30,Europe/Moscow,m1", "Physics,2026-10-20,11:00,12:30,Europe/Moscow,p1"); res.Changed != 1 {
		t.Fatalf("re-import = %+v", res)
	}
	e.mustSync()
	events := e.google.Events(cal.ID)
	ev, ok := e.google.Event(cal.ID, mathEvent)
	if len(events) != 2 || !ok || ev.Summary != "Algebra" || ev.Start.DateTime != "2026-10-20T10:00:00Z" ||
		ev.End.DateTime != "2026-10-20T11:30:00Z" || ev.Private["revision"] != "2" {
		t.Fatalf("events = %+v, the changed one %+v", events, ev)
	}

	// Physics is left out of the next file: its event is cancelled and its row purged.
	if res := e.importCSV("Algebra,2026-10-20,13:00,14:30,Europe/Moscow,m1"); res.Removed != 1 {
		t.Fatalf("re-import = %+v", res)
	}
	if e.pending() != 1 {
		t.Fatalf("%d pending, want the deleted lesson", e.pending())
	}
	e.mustSync()
	if ev, ok := e.google.Event(cal.ID, physicsEvent); !ok || ev.Status != testenv.FakeEventCancelled {
		t.Fatalf("event of the removed lesson = %+v (found %v)", ev, ok)
	}
	if got := e.google.Events(cal.ID); len(got) != 1 || got[0].ID != mathEvent || e.pending() != 0 {
		t.Fatalf("events %+v, %d pending", got, e.pending())
	}
	if _, err := e.lessons.Get(ctx, live[1].ID()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of the purged lesson = %v", err)
	}
}

// A lost answer after Google applied the insert: the retry updates the event it created, still one event.
func TestCalendarSyncLostAnswerMakesOneEvent(t *testing.T) {
	e := newCalendarEnv(t)
	e.mustSync() // the calendar exists before the fault
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1")
	e.google.FailNext(testenv.FaultDropAfterWrite, http.MethodPost)

	err := e.handle()
	if wait, ok := backoff.WaitStatus(err); err == nil || !ok || wait != job.StatusWaitingForNetwork {
		t.Fatalf("sync with a lost answer = %v, want a wait for the network", err)
	}
	cal := e.appCalendar()
	if n := len(e.google.Events(cal.ID)); n != 1 || e.pending() != 1 {
		t.Fatalf("after the lost answer: %d events, %d pending", n, e.pending())
	}
	e.mustSync()
	if n := len(e.google.Events(cal.ID)); n != 1 || e.pending() != 0 {
		t.Fatalf("after the retry: %d events, %d pending", n, e.pending())
	}
}

// The user deleted the app calendar at Google: the next sync makes a new one and writes every lesson into it.
func TestCalendarSyncRewritesIntoAFreshCalendar(t *testing.T) {
	e := newCalendarEnv(t)
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1", "Physics,2026-10-20,11:00,12:30,Europe/Moscow,p1")
	e.mustSync()
	old := e.appCalendar()

	e.google.DeleteCalendar(old.ID)
	e.mustSync()
	fresh := e.appCalendar()
	if fresh.ID == old.ID || len(e.google.Events(fresh.ID)) != 2 || e.pending() != 0 {
		t.Fatalf("new calendar %+v with %d events, %d pending", fresh, len(e.google.Events(fresh.ID)), e.pending())
	}
	e.mustSync()
	if len(e.google.Calendars()) != 1 || len(e.google.Events(fresh.ID)) != 2 {
		t.Fatal("a third sync changed the calendars")
	}
}

// Without a sign-in nothing is requested or written and the lessons stay pending; after the sign-in they are written.
func TestCalendarSyncWaitsForTheSignIn(t *testing.T) {
	ctx := context.Background()
	e := newCalendarEnv(t)
	e.auth.set(false)
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1")

	if jobs, err := e.jobs.ListByStatus(ctx, job.Statuses()...); err != nil || len(jobs) != 0 {
		t.Fatalf("jobs after an import without a sign-in: %d, %v", len(jobs), err)
	}
	if _, err := e.sync.RequestSync(ctx); !errors.Is(err, schedulesync.ErrCalendarNotConnected) {
		t.Fatalf("RequestSync = %v", err)
	}
	if err := e.handle(); err != nil {
		t.Fatalf("a sync job without a sign-in = %v, want a no-op", err)
	}
	st, err := e.sync.Status(ctx)
	if err != nil || st.SignedIn || st.LastOutcome != schedulesync.OutcomeNotSignedIn || st.Pending != 1 ||
		len(e.google.Requests()) != 0 {
		t.Fatalf("status %+v, %v; %d requests", st, err, len(e.google.Requests()))
	}

	e.auth.set(true)
	req, err := e.sync.RequestSync(ctx)
	if err != nil || !req.Created {
		t.Fatalf("RequestSync after the sign-in = %+v, %v", req, err)
	}
	e.mustSync()
	if n := len(e.google.Events(e.appCalendar().ID)); n != 1 || e.pending() != 0 {
		t.Fatalf("%d events, %d pending", n, e.pending())
	}
	if st, _ := e.sync.Status(ctx); st.LastOutcome != schedulesync.OutcomeOK || !st.LastWrittenAt.Equal(e.clock.Now()) {
		t.Fatalf("status %+v", st)
	}
}

// The job store runs the sync: the import asks for it, a Google failure is retried with backoff, and the job ends
// COMPLETED with the lessons written (AC-20: the job store, retries).
func TestCalendarSyncRunsAsAJobWithRetries(t *testing.T) {
	ctx := context.Background()
	e := newCalendarEnv(t)
	runner, err := jobrunner.New(jobrunner.Deps{Jobs: e.jobs, Clock: e.clock, Sleeper: e.clock}, jobrunner.Config{
		Workers: 1, PollInterval: time.Second, DefaultTimeout: 10 * time.Minute,
		Backoff: backoff.Spec{Base: 5 * time.Second, Factor: 2, MaxDelay: time.Minute, MaxAttempts: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(jobrunner.Registration{
		Type: schedulesync.CalendarSyncJobType, Handler: e.sync, Recovery: jobrunner.RecoveryRequeue,
	}); err != nil {
		t.Fatal(err)
	}

	e.google.FailNext(testenv.FaultServerError, "")
	e.importCSV("Math,2026-10-20,09:00,10:30,Europe/Moscow,m1", "Physics,2026-10-20,11:00,12:30,Europe/Moscow,p1")
	jobs, err := e.jobs.ListByStatus(ctx, job.StatusQueued)
	if err != nil || len(jobs) != 1 || jobs[0].Type() != schedulesync.CalendarSyncJobType {
		t.Fatalf("queued jobs after the import = %v, %v", jobs, err)
	}
	id := jobs[0].ID()

	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = runner.Stop(stop)
	})
	testenv.Eventually(t, "the sync job completes", func() bool {
		j, err := e.jobs.Get(ctx, id)
		return err == nil && j.Status() == job.StatusCompleted
	}, func() { e.clock.Advance(time.Second) })

	j, _ := e.jobs.Get(ctx, id)
	if j.Attempts() != 2 || len(e.google.Events(e.appCalendar().ID)) != 2 || e.pending() != 0 {
		t.Fatalf("job after %d attempts, %d events, %d pending", j.Attempts(), len(e.google.Events(e.appCalendar().ID)), e.pending())
	}
}
