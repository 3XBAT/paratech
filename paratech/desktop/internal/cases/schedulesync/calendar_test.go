package schedulesync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeCalendar is the CalendarAdapter: the events of each calendar as the revision written per lesson.
type fakeCalendar struct {
	mu       sync.Mutex
	id       string
	fresh    bool
	n        int
	events   map[string]map[shared.LessonID]int64
	fail     map[string][]error        // queued errors per operation: ensure, confirm, upsert, delete
	refuse   map[shared.LessonID]error // every upsert of the lesson fails with it
	onUpsert func(l *lesson.Lesson)    // runs after a successful upsert, outside the lock
	calls    map[string]int
	upserted map[shared.LessonID]int   // upserts per lesson
	order    []string                  // operations in order
	ensureIn func(ctx context.Context) // checks the caller is outside a transaction
}

func newFakeCalendar() *fakeCalendar {
	c := &fakeCalendar{
		events: map[string]map[shared.LessonID]int64{}, fail: map[string][]error{}, refuse: map[shared.LessonID]error{},
		calls: map[string]int{}, upserted: map[shared.LessonID]int{},
	}
	c.replace()
	return c
}

// replace is a new app calendar, as when the old one is gone.
func (c *fakeCalendar) replace() {
	c.n++
	c.id = fmt.Sprintf("cal-%d", c.n)
	c.fresh = true
	c.events[c.id] = map[shared.LessonID]int64{}
}

func (c *fakeCalendar) failNext(op string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail[op] = append(c.fail[op], err)
}

// call records op and returns its queued error; c.mu is held.
func (c *fakeCalendar) call(ctx context.Context, op string) error {
	if c.ensureIn != nil {
		c.ensureIn(ctx)
	}
	c.calls[op]++
	c.order = append(c.order, op)
	if q := c.fail[op]; len(q) > 0 {
		c.fail[op] = q[1:]
		return q[0]
	}
	return nil
}

func (c *fakeCalendar) EnsureCalendar(ctx context.Context) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.call(ctx, "ensure"); err != nil {
		return "", false, err
	}
	return c.id, c.fresh, nil
}

func (c *fakeCalendar) ConfirmCalendar(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.call(ctx, "confirm"); err != nil {
		return err
	}
	if id == c.id {
		c.fresh = false
	}
	return nil
}

func (c *fakeCalendar) UpsertLesson(ctx context.Context, id string, l *lesson.Lesson) error {
	c.mu.Lock()
	err := c.call(ctx, "upsert")
	if err == nil {
		err = c.refuse[l.ID()]
	}
	if err == nil {
		if id != c.id || l.Deleted() {
			err = backoff.Permanent(errors.New("fake calendar: wrong upsert"))
		} else {
			c.events[id][l.ID()] = l.ContentRevision()
			c.upserted[l.ID()]++
		}
	}
	hook := c.onUpsert
	c.mu.Unlock()
	if err == nil && hook != nil {
		hook(l)
	}
	return err
}

func (c *fakeCalendar) DeleteEvent(ctx context.Context, id string, lessonID shared.LessonID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.call(ctx, "delete"); err != nil {
		return err
	}
	delete(c.events[id], lessonID)
	return nil
}

// current returns the events of the current calendar.
func (c *fakeCalendar) current() map[shared.LessonID]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[shared.LessonID]int64{}
	for k, v := range c.events[c.id] {
		out[k] = v
	}
	return out
}

func (c *fakeCalendar) count(op string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[op]
}

type fakeAccount struct {
	mu       sync.Mutex
	signedIn bool
	err      error
}

func (a *fakeAccount) SignedIn(context.Context) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.signedIn, a.err
}

func (a *fakeAccount) set(signedIn bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.signedIn, a.err = signedIn, err
}

// fakeJobs is the job store: unique keys, Persisted(1) on creation.
type fakeJobs struct {
	mu      sync.Mutex
	jobs    []*job.Job
	listErr error
}

func (f *fakeJobs) Enqueue(_ context.Context, j *job.Job) (shared.JobID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.jobs {
		if x.Key() == j.Key() {
			return x.ID(), false, nil
		}
	}
	j.Persisted(1)
	f.jobs = append(f.jobs, j)
	return j.ID(), true, nil
}

func (f *fakeJobs) ListByStatus(_ context.Context, statuses ...job.Status) ([]*job.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*job.Job
	for _, j := range f.jobs {
		if slices.Contains(statuses, j.Status()) {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeJobs) all() []*job.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*job.Job(nil), f.jobs...)
}

var (
	_ CalendarAdapter = (*fakeCalendar)(nil)
	_ CalendarAccount = (*fakeAccount)(nil)
	_ JobEnqueuer     = (*fakeJobs)(nil)
	_ SyncRequester   = (*CalendarSync)(nil)
)

type syncEnv struct {
	store   *lessonStore
	cal     *fakeCalendar
	account *fakeAccount
	jobs    *fakeJobs
	clock   *testenv.FakeClock
	ids     *seqIDs
	sync    *CalendarSync
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	e := &syncEnv{
		store: newLessonStore(t), cal: newFakeCalendar(), account: &fakeAccount{signedIn: true}, jobs: &fakeJobs{},
		clock: testenv.NewFakeClock(t0), ids: &seqIDs{},
	}
	// No calendar call holds the database write lock (Tx.Do fn performs no external I/O).
	e.cal.ensureIn = func(ctx context.Context) {
		if ctx.Value(txKey{}) != nil {
			t.Error("calendar called inside Tx.Do")
		}
	}
	var err error
	e.sync, err = NewCalendarSync(CalendarDeps{
		Lessons: e.store, Calendar: e.cal, Account: e.account, Jobs: e.jobs, Clock: e.clock, IDs: e.ids, Tx: e.store,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *syncEnv) seed(t *testing.T, id string, c lesson.Content) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: shared.LessonID(id), Content: c, SourceUID: "csv:" + id}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.store.put(l)
	return l
}

func syncSnapshot(t *testing.T, typ job.Type) job.Snapshot {
	t.Helper()
	j, err := job.New(job.Spec{ID: "sync-job", Type: typ, Key: "calendar_sync:sync-job", MaxAttempts: 5}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return j.Snapshot()
}

func (e *syncEnv) handle(t *testing.T) error {
	t.Helper()
	return e.sync.Handle(context.Background(), syncSnapshot(t, CalendarSyncJobType), nil)
}

func (e *syncEnv) mustSync(t *testing.T) {
	t.Helper()
	if err := e.handle(t); err != nil {
		t.Fatalf("Handle = %v", err)
	}
}

func (e *syncEnv) pending(t *testing.T) []shared.LessonID {
	t.Helper()
	ls, err := e.store.ListPendingCalendarSync(context.Background(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var ids []shared.LessonID
	for _, l := range ls {
		ids = append(ids, l.ID())
	}
	return ids
}

func TestNewCalendarSyncRequiresPorts(t *testing.T) {
	ok := func() CalendarDeps {
		s := newLessonStore(t)
		return CalendarDeps{
			Lessons: s, Calendar: newFakeCalendar(), Account: &fakeAccount{}, Jobs: &fakeJobs{},
			Clock: testenv.NewFakeClock(t0), IDs: &seqIDs{}, Tx: s,
		}
	}
	tests := []struct {
		name string
		edit func(*CalendarDeps)
	}{
		{"no lessons", func(d *CalendarDeps) { d.Lessons = nil }},
		{"no calendar", func(d *CalendarDeps) { d.Calendar = nil }},
		{"no account", func(d *CalendarDeps) { d.Account = nil }},
		{"no jobs", func(d *CalendarDeps) { d.Jobs = nil }},
		{"no clock", func(d *CalendarDeps) { d.Clock = nil }},
		{"no ids", func(d *CalendarDeps) { d.IDs = nil }},
		{"no tx", func(d *CalendarDeps) { d.Tx = nil }},
		{"negative attempts", func(d *CalendarDeps) { d.MaxAttempts = -1 }},
	}
	for _, tt := range tests {
		d := ok()
		tt.edit(&d)
		if _, err := NewCalendarSync(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: NewCalendarSync = %v, want ErrInvalidConfig", tt.name, err)
		}
	}
	if _, err := NewCalendarSync(ok()); err != nil {
		t.Fatal(err)
	}
}

// Test step 2 of TASK-051 on port fakes: two runs in a row leave the same events, and the second writes nothing.
func TestSyncTwiceWritesOnce(t *testing.T) {
	e := newSyncEnv(t)
	for i, id := range []string{"a", "b", "c"} {
		e.seed(t, id, content("Lesson "+id, at(i)))
	}

	e.mustSync(t)
	first := e.cal.current()
	if len(first) != 3 || len(e.pending(t)) != 0 || e.cal.count("confirm") != 1 {
		t.Fatalf("after the first run: events %v, pending %v, confirms %d", first, e.pending(t), e.cal.count("confirm"))
	}
	for id, st := range e.store.snapshot() {
		if st.CalendarSyncedRevision != 1 || !st.CalendarSyncedAt.Equal(t0) {
			t.Errorf("%s: synced %d at %v", id, st.CalendarSyncedRevision, st.CalendarSyncedAt)
		}
	}

	e.mustSync(t)
	if got := e.cal.current(); len(got) != len(first) || e.cal.count("upsert") != 3 || e.cal.count("confirm") != 1 {
		t.Fatalf("after the second run: events %v, upserts %d, confirms %d", got, e.cal.count("upsert"), e.cal.count("confirm"))
	}
	if st, _ := e.sync.Status(context.Background()); st.LastOutcome != OutcomeOK || !st.LastRunAt.Equal(t0) || st.Pending != 0 {
		t.Fatalf("status = %+v", st)
	}
}

// Test step 3 of TASK-051 on port fakes: a changed lesson updates its event, a deleted one loses it and its row.
func TestSyncUpdatesAndDeletes(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	e.seed(t, "b", content("Physics", at(1)))
	e.mustSync(t)

	a, _ := e.store.Get(ctx, "a")
	c := a.Content()
	c.Title, c.Start, c.End = "Algebra", at(5), at(6)
	if _, err := a.ChangeContent(c, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	b, _ := e.store.Get(ctx, "b")
	if err := b.MarkDeleted(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Do(ctx, func(ctx context.Context) error {
		return e.store.ApplyChanges(ctx, []*lesson.Lesson{a}, []*lesson.Lesson{b})
	}); err != nil {
		t.Fatal(err)
	}

	e.clock.Advance(time.Hour)
	e.mustSync(t)
	if got := e.cal.current(); len(got) != 1 || got["a"] != 2 {
		t.Fatalf("events = %v, want only a at revision 2", got)
	}
	rows := e.store.snapshot()
	if _, ok := rows["b"]; ok || e.store.purges != 1 {
		t.Fatalf("deleted lesson still stored (%d purges)", e.store.purges)
	}
	if st := rows["a"]; st.CalendarSyncedRevision != 2 || !st.CalendarSyncedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("changed lesson = %+v", st)
	}
	if e.cal.upserted["a"] != 2 || e.cal.count("delete") != 1 {
		t.Fatalf("upserts of a %d, deletes %d", e.cal.upserted["a"], e.cal.count("delete"))
	}
}

// A fresh calendar gets every live lesson again; a crash before ConfirmCalendar keeps it fresh, so the next run
// rewrites everything once more and only then confirms.
func TestSyncFreshCalendarRewritesEverything(t *testing.T) {
	e := newSyncEnv(t)
	for i, id := range []string{"a", "b", "c"} {
		e.seed(t, id, content("Lesson "+id, at(i)))
	}
	e.mustSync(t)

	e.cal.mu.Lock()
	e.cal.replace() // the user deleted the calendar at Google: the adapter made a new one
	e.cal.mu.Unlock()
	e.cal.failNext("confirm", backoff.Transient(errors.New("fake: store down")))
	if err := e.handle(t); err == nil || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("Handle with a failing confirm = %v, want a transient error", err)
	}
	if got := e.pending(t); len(got) != 3 || len(e.cal.current()) != 0 {
		t.Fatalf("after the crash: pending %v, events %v", got, e.cal.current())
	}

	e.mustSync(t)
	if got := e.cal.current(); len(got) != 3 || len(e.pending(t)) != 0 || e.cal.fresh {
		t.Fatalf("after the retry: events %v, pending %v, fresh %v", got, e.pending(t), e.cal.fresh)
	}
	// The order of the second run: ensure, confirm (after every forget), then the writes.
	e.cal.mu.Lock()
	order := slices.Clone(e.cal.order)
	e.cal.mu.Unlock()
	last := order[len(order)-5:]
	if !slices.Equal(last, []string{"ensure", "confirm", "upsert", "upsert", "upsert"}) {
		t.Fatalf("operations of the retry = %v", last)
	}
}

// A crash between the upsert and the mark leaves the lesson pending; the next run upserts the same event again.
func TestSyncCrashBetweenUpsertAndMark(t *testing.T) {
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	e.store.saveErr = errors.New("fake: disk full")

	if err := e.handle(t); err == nil || backoff.Classify(err) == backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want a transient error", err)
	}
	if got := e.pending(t); len(got) != 1 || len(e.cal.current()) != 1 {
		t.Fatalf("after the crash: pending %v, events %v", got, e.cal.current())
	}

	e.mustSync(t)
	if got := e.cal.current(); len(got) != 1 || e.cal.upserted["a"] != 2 || len(e.pending(t)) != 0 {
		t.Fatalf("after the retry: events %v, upserts %d, pending %v", got, e.cal.upserted["a"], e.pending(t))
	}
}

// A crash between the event deletion and the purge deletes again on the next run (gone is success) and then purges.
func TestSyncCrashBetweenDeleteAndPurge(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)
	l := e.seed(t, "a", content("Math", at(0)))
	e.mustSync(t)
	l, _ = e.store.Get(ctx, l.ID())
	if err := l.MarkDeleted(t0); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Do(ctx, func(ctx context.Context) error { return e.store.Save(ctx, l) }); err != nil {
		t.Fatal(err)
	}
	e.store.purgeErr = errors.New("fake: disk full")

	if err := e.handle(t); err == nil || backoff.Classify(err) == backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want a transient error", err)
	}
	if _, ok := e.store.snapshot()["a"]; !ok || len(e.cal.current()) != 0 {
		t.Fatal("the row was purged or the event kept")
	}
	e.mustSync(t)
	if _, ok := e.store.snapshot()["a"]; ok || e.cal.count("delete") != 2 {
		t.Fatalf("row kept, %d deletes", e.cal.count("delete"))
	}
}

// A lesson changed while its event was written stays pending and is written again in the same run; one whose version
// moved without a content change (the start announcement) is marked again.
func TestSyncLessonChangedDuringWrite(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*lesson.Lesson)
		wantUpserts int
		wantRev     int64
	}{
		{"content changed", func(l *lesson.Lesson) {
			c := l.Content()
			c.Title = "Algebra"
			_, _ = l.ChangeContent(c, t0.Add(time.Minute))
		}, 2, 2},
		{"announced", func(l *lesson.Lesson) { _, _ = l.AnnounceStart(at(0), time.Minute) }, 1, 1},
		{"deleted", func(l *lesson.Lesson) { _ = l.MarkDeleted(t0.Add(time.Minute)) }, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSyncEnv(t)
			e.seed(t, "a", content("Math", at(0)))
			var once sync.Once
			e.cal.onUpsert = func(l *lesson.Lesson) { once.Do(func() { e.store.mutate(l.ID(), tt.change) }) }

			e.mustSync(t)
			if e.cal.upserted["a"] != tt.wantUpserts || len(e.pending(t)) != 0 {
				t.Fatalf("upserts %d, pending %v", e.cal.upserted["a"], e.pending(t))
			}
			if tt.wantRev == 0 {
				if _, ok := e.store.snapshot()["a"]; ok || len(e.cal.current()) != 0 {
					t.Fatal("the deleted lesson kept its row or event")
				}
				return
			}
			if st := e.store.snapshot()["a"]; st.CalendarSyncedRevision != tt.wantRev || e.cal.current()["a"] != tt.wantRev {
				t.Fatalf("synced %d, event %d, want %d", st.CalendarSyncedRevision, e.cal.current()["a"], tt.wantRev)
			}
		})
	}
}

// Without a sign-in, or with one Google refused, the job ends without writing and says so; the lessons stay pending.
func TestSyncNotSignedIn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want SyncOutcome
	}{
		{"not signed in", fmt.Errorf("x: %w", ErrCalendarNotConnected), OutcomeNotSignedIn},
		{"sign-in expired", fmt.Errorf("x: %w", ErrCalendarSignInExpired), OutcomeSignInExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSyncEnv(t)
			e.seed(t, "a", content("Math", at(0)))
			e.cal.failNext("ensure", backoff.Permanent(tt.err))
			if err := e.handle(t); err != nil {
				t.Fatalf("Handle = %v, want a no-op success", err)
			}
			st, err := e.sync.Status(context.Background())
			if err != nil || st.LastOutcome != tt.want || st.Pending != 1 || e.cal.count("upsert") != 0 {
				t.Fatalf("status %+v, %v; upserts %d", st, err, e.cal.count("upsert"))
			}
		})
	}

	// Also in the middle of a run: the rest is not tried.
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	e.seed(t, "b", content("Physics", at(1)))
	e.cal.failNext("upsert", backoff.Permanent(ErrCalendarSignInExpired))
	if err := e.handle(t); err != nil || e.cal.count("upsert") != 1 || len(e.pending(t)) != 2 {
		t.Fatalf("Handle = %v, upserts %d, pending %v", err, e.cal.count("upsert"), e.pending(t))
	}
}

// Transient failures are retried by the runner (the network one waits for the network), permanent ones fail the job.
func TestSyncFailureClasses(t *testing.T) {
	network := fmt.Errorf("fake: %w", shared.ErrNetworkUnavailable)
	tests := []struct {
		name      string
		op        string
		err       error
		permanent bool
		wait      job.Status
	}{
		{"ensure unavailable", "ensure", backoff.Transient(errors.New("fake: 503")), false, ""},
		{"ensure no network", "ensure", backoff.Transient(network), false, job.StatusWaitingForNetwork},
		{"ensure refused", "ensure", backoff.Permanent(errors.New("fake: 403")), true, ""},
		{"upsert rate limited", "upsert", backoff.Transient(errors.New("fake: 429")), false, ""},
		{"upsert no network", "upsert", backoff.Transient(network), false, job.StatusWaitingForNetwork},
		{"delete unavailable", "delete", backoff.Transient(errors.New("fake: 500")), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSyncEnv(t)
			e.seed(t, "a", content("Math", at(0)))
			if tt.op == "delete" {
				e.store.mutate("a", func(l *lesson.Lesson) { _ = l.MarkDeleted(t0) })
			}
			e.cal.failNext(tt.op, tt.err)
			err := e.handle(t)
			if err == nil || (backoff.Classify(err) == backoff.ClassPermanent) != tt.permanent {
				t.Fatalf("Handle = %v, want permanent %v", err, tt.permanent)
			}
			if wait, _ := backoff.WaitStatus(err); wait != tt.wait {
				t.Fatalf("wait status %q, want %q", wait, tt.wait)
			}
			if st, _ := e.sync.Status(context.Background()); st.LastOutcome != OutcomeFailed || len(e.pending(t)) != 1 {
				t.Fatalf("status %+v, pending %v", st, e.pending(t))
			}
		})
	}
}

// A lesson the calendar refuses for good does not block the others; the job then fails with a fixed reason, and a run
// whose writes keep being refused stops early.
func TestSyncRefusedLessons(t *testing.T) {
	e := newSyncEnv(t)
	for i := range 4 {
		e.seed(t, fmt.Sprintf("l%d", i), content("Lesson", at(i)))
	}
	e.cal.refuse["l0"] = backoff.Permanent(errors.New("fake: 400"))
	err := e.handle(t)
	if !errors.Is(err, ErrLessonsRefused) || backoff.Classify(err) != backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want ErrLessonsRefused", err)
	}
	if got := e.pending(t); !slices.Equal(got, []shared.LessonID{"l0"}) || len(e.cal.current()) != 3 {
		t.Fatalf("pending %v, events %v", got, e.cal.current())
	}

	e = newSyncEnv(t)
	for i := range maxRefusedPerRun + 3 {
		id := fmt.Sprintf("l%02d", i)
		e.seed(t, id, content("Lesson", at(i)))
		e.cal.refuse[shared.LessonID(id)] = backoff.Permanent(errors.New("fake: 403"))
	}
	if err := e.handle(t); !errors.Is(err, ErrLessonsRefused) || e.cal.count("upsert") != maxRefusedPerRun {
		t.Fatalf("Handle = %v after %d upserts, want a stop after %d", err, e.cal.count("upsert"), maxRefusedPerRun)
	}
}

// A run writes at most maxLessonsPerRun lessons and asks for a follow-up job, which writes the rest.
func TestSyncBoundPerRun(t *testing.T) {
	e := newSyncEnv(t)
	for i := range maxLessonsPerRun + 2 {
		e.seed(t, fmt.Sprintf("l%04d", i), content("Lesson", t0.Add(time.Duration(i)*time.Minute)))
	}
	e.mustSync(t)
	if n := len(e.pending(t)); n != 2 || e.cal.count("upsert") != maxLessonsPerRun {
		t.Fatalf("pending %d, upserts %d", n, e.cal.count("upsert"))
	}
	jobs := e.jobs.all()
	if len(jobs) != 1 || jobs[0].Type() != CalendarSyncJobType || jobs[0].Status() != job.StatusQueued {
		t.Fatalf("follow-up jobs = %v", jobs)
	}
	e.mustSync(t)
	if n := len(e.pending(t)); n != 0 || len(e.jobs.all()) != 1 {
		t.Fatalf("pending %d, jobs %d", n, len(e.jobs.all()))
	}
}

func TestSyncRefusesOtherJobTypes(t *testing.T) {
	e := newSyncEnv(t)
	err := e.sync.Handle(context.Background(), syncSnapshot(t, "stt"), nil)
	if !errors.Is(err, ErrUnknownJobType) || backoff.Classify(err) != backoff.ClassPermanent || e.cal.count("ensure") != 0 {
		t.Fatalf("Handle(stt) = %v", err)
	}
}

// A cancelled job stops and records no outcome.
func TestSyncCancelled(t *testing.T) {
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	ctx, cancel := context.WithCancel(context.Background())
	e.cal.failNext("ensure", context.Canceled)
	cancel()
	if err := e.sync.Handle(ctx, syncSnapshot(t, CalendarSyncJobType), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Handle = %v", err)
	}
	if st, _ := e.sync.Status(context.Background()); st.LastOutcome != OutcomeNone {
		t.Fatalf("outcome %q after a cancel", st.LastOutcome)
	}
}

// Runs of one process never overlap: a second job waits until the first one ends.
func TestSyncRunsDoNotOverlap(t *testing.T) {
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	e.cal.onUpsert = func(*lesson.Lesson) {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	first := make(chan error, 1)
	go func() { first <- e.handle(t) }()
	<-entered

	second := make(chan error, 1)
	go func() { second <- e.handle(t) }()
	select {
	case err := <-second:
		t.Fatalf("the second run did not wait: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if e.cal.count("ensure") != 1 {
		t.Fatalf("ensures %d while the first run holds", e.cal.count("ensure"))
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if e.cal.upserted["a"] != 1 {
		t.Fatalf("upserts %d", e.cal.upserted["a"])
	}
}

func TestRequestSync(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)

	e.account.set(false, nil)
	if _, err := e.sync.RequestSync(ctx); !errors.Is(err, ErrCalendarNotConnected) || len(e.jobs.all()) != 0 {
		t.Fatalf("RequestSync without a sign-in = %v, %d jobs", err, len(e.jobs.all()))
	}
	storeDown := errors.New("fake: credential store down")
	e.account.set(false, storeDown)
	if _, err := e.sync.RequestSync(ctx); !errors.Is(err, storeDown) || len(e.jobs.all()) != 0 {
		t.Fatalf("RequestSync with a failing store = %v", err)
	}

	e.account.set(true, nil)
	first, err := e.sync.RequestSync(ctx)
	if err != nil || !first.Created {
		t.Fatalf("RequestSync = %+v, %v", first, err)
	}
	j := e.jobs.all()[0]
	if j.Type() != CalendarSyncJobType || j.Key() != "calendar_sync:"+first.JobID.String() || j.MaxAttempts() != backoff.DefaultSpec().MaxAttempts ||
		j.Status() != job.StatusQueued {
		t.Fatalf("job = %s %s %q, %d attempts", j.Type(), j.Status(), j.Key(), j.MaxAttempts())
	}

	// A job that has not started covers every later request, also while it waits for a retry or the network.
	again, err := e.sync.RequestSync(ctx)
	if err != nil || again != (SyncRequest{JobID: first.JobID}) {
		t.Fatalf("second RequestSync = %+v, %v", again, err)
	}
	if err := j.Claim(t0); err != nil {
		t.Fatal(err)
	}
	if err := j.WaitFor(job.StatusWaitingForNetwork, t0); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.sync.RequestSync(ctx); again.Created || again.JobID != first.JobID {
		t.Fatalf("RequestSync while the job waits for the network = %+v", again)
	}

	// While it runs, a request makes the next job: what the running one may miss is not lost.
	if err := j.Requeue(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := j.Claim(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	next, err := e.sync.RequestSync(ctx)
	if err != nil || !next.Created || next.JobID == first.JobID {
		t.Fatalf("RequestSync while the job runs = %+v, %v", next, err)
	}
	if again, _ := e.sync.RequestSync(ctx); again.Created || again.JobID != next.JobID {
		t.Fatalf("RequestSync with a running and a queued job = %+v", again)
	}

	e.jobs.listErr = errors.New("fake: database locked")
	if _, err := e.sync.RequestSync(ctx); err == nil {
		t.Fatal("RequestSync with a failing job store succeeded")
	}
}

func TestCalendarStatus(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)
	e.seed(t, "a", content("Math", at(0)))
	e.seed(t, "b", content("Physics", at(1)))
	e.mustSync(t)
	e.clock.Advance(time.Hour)
	e.seed(t, "c", content("Chemistry", at(2)))

	st, err := e.sync.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := CalendarStatus{SignedIn: true, Pending: 1, LastWrittenAt: t0, LastOutcome: OutcomeOK, LastRunAt: t0}
	if st != want {
		t.Fatalf("Status = %+v, want %+v", st, want)
	}

	// The job status: what runs first, then why one waits.
	add := func(status job.Status) {
		id := e.ids.NewJobID()
		j, err := job.New(job.Spec{ID: id, Type: CalendarSyncJobType, Key: "calendar_sync:" + id.String(), MaxAttempts: 5}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if status != job.StatusQueued {
			if err := j.Claim(t0); err != nil {
				t.Fatal(err)
			}
		}
		switch status {
		case job.StatusRetryWait:
			err = j.Retry(t0, t0.Add(time.Minute), "fake")
		case job.StatusWaitingForNetwork:
			err = j.WaitFor(status, t0)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.jobs.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	other, _ := job.New(job.Spec{ID: "stt-1", Type: "stt", Key: "stt:x", MaxAttempts: 5}, t0)
	_ = other.Claim(t0)
	_, _, _ = e.jobs.Enqueue(ctx, other)
	for _, step := range []struct {
		add  job.Status
		want job.Status
	}{
		{job.StatusQueued, job.StatusQueued},
		{job.StatusRetryWait, job.StatusRetryWait},
		{job.StatusWaitingForNetwork, job.StatusWaitingForNetwork},
		{job.StatusRunning, job.StatusRunning},
	} {
		add(step.add)
		if st, _ := e.sync.Status(ctx); st.Job != step.want {
			t.Fatalf("after adding a %s job: Job = %q, want %q", step.add, st.Job, step.want)
		}
	}

	e.account.set(false, nil)
	if st, _ := e.sync.Status(ctx); st.SignedIn {
		t.Fatal("signed in after the sign-out")
	}
	e.account.set(false, errors.New("fake: store down"))
	if _, err := e.sync.Status(ctx); err == nil {
		t.Fatal("Status with a failing store succeeded")
	}
}

func TestCalendarStatusCapsThePendingCount(t *testing.T) {
	e := newSyncEnv(t)
	for i := range MaxPendingShown + 1 {
		e.seed(t, fmt.Sprintf("l%04d", i), content("Lesson", t0.Add(time.Duration(i)*time.Minute)))
	}
	st, err := e.sync.Status(context.Background())
	if err != nil || st.Pending != MaxPendingShown || !st.PendingMore || !st.LastWrittenAt.IsZero() {
		t.Fatalf("Status = %+v, %v", st, err)
	}
}

// fakeSync records the requests of the import.
type fakeSync struct {
	calls int
	err   error
}

func (f *fakeSync) RequestSync(context.Context) (SyncRequest, error) {
	f.calls++
	return SyncRequest{JobID: "job", Created: true}, f.err
}

// A commit that changed the schedule asks for a sync after the transaction; a failed request does not fail the commit.
func TestCommitRequestsCalendarSync(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		err  error
	}{
		{"requested", nil},
		{"not signed in", fmt.Errorf("x: %w", ErrCalendarNotConnected)},
		{"request fails", errors.New("fake: database locked")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			sync := &fakeSync{err: tt.err}
			svc, err := New(Deps{
				Lessons: e.store, Parsers: map[importplan.Source]ScheduleParser{importplan.SourceCSV: e.parser},
				Clock: e.clock, IDs: &seqIDs{}, Tx: e.store, Sync: sync,
			})
			if err != nil {
				t.Fatal(err)
			}
			e.parser.files["f"] = rows(row(2, "a", content("Math", at(0))))
			commit := func() CommitResult {
				t.Helper()
				p, err := svc.PreviewImport(ctx, ImportRequest{Source: importplan.SourceCSV, Data: []byte("f")})
				if err != nil {
					t.Fatal(err)
				}
				res, err := svc.CommitImport(ctx, CommitRequest{Source: importplan.SourceCSV, Data: []byte("f"), ConfirmToken: p.ConfirmToken})
				if err != nil {
					t.Fatalf("CommitImport = %v", err)
				}
				return res
			}
			if res := commit(); res.Added != 1 || sync.calls != 1 {
				t.Fatalf("commit %+v, %d requests", res, sync.calls)
			}
			if res := commit(); res != (CommitResult{}) || sync.calls != 1 {
				t.Fatalf("unchanged commit %+v, %d requests", res, sync.calls)
			}
		})
	}
}
