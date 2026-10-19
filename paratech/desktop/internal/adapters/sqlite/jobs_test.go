package sqlite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

var jobT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func jobSpec(id, typ string) job.Spec {
	return job.Spec{ID: shared.JobID(id), Type: job.Type(typ), Key: typ + ":" + id, MaxAttempts: 3}
}

func newTestJob(t *testing.T, s job.Spec) *job.Job {
	t.Helper()
	j, err := job.New(s, jobT0)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func enqueueJob(t *testing.T, r *JobRepository, s job.Spec) *job.Job {
	t.Helper()
	j := newTestJob(t, s)
	if _, created, err := r.Enqueue(context.Background(), j); err != nil || !created {
		t.Fatalf("Enqueue %s: created=%v err=%v", s.ID, created, err)
	}
	return j
}

func jobFilter(t *testing.T, types, heavy []job.Type, heavyAllowed bool) job.ClaimFilter {
	t.Helper()
	f, err := job.NewClaimFilter(types, heavy, heavyAllowed)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func anyJobFilter(t *testing.T, types ...job.Type) job.ClaimFilter {
	return jobFilter(t, types, nil, true)
}

func countJobs(t *testing.T, db *DB) int64 { return scanInt(t, db.sql, "SELECT count(*) FROM jobs") }

func TestEnqueueIsIdempotentByKey(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()

	first := newTestJob(t, jobSpec("j1", "stt"))
	id, created, err := r.Enqueue(ctx, first)
	if err != nil || !created || id != "j1" || first.Version() != 1 {
		t.Fatalf("first Enqueue: id=%q created=%v version=%d err=%v", id, created, first.Version(), err)
	}

	again := jobSpec("j2", "stt")
	again.Key = first.Key() // the same request with a fresh id, like a double click on Generate
	dup := newTestJob(t, again)
	id, created, err = r.Enqueue(ctx, dup)
	if err != nil || created || id != "j1" || dup.Version() != 0 {
		t.Fatalf("repeated Enqueue: id=%q created=%v version=%d err=%v", id, created, dup.Version(), err)
	}
	if n := countJobs(t, db); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	got, err := r.Get(ctx, id)
	if err != nil || got.ID() != "j1" {
		t.Fatalf("Get existing: %v %v", got, err)
	}
}

func TestEnqueueRejectsReusedIDAndStoredJob(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	enqueueJob(t, r, jobSpec("j1", "stt"))

	other := jobSpec("j1", "summarize") // same id, other key
	if _, _, err := r.Enqueue(ctx, newTestJob(t, other)); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("id collision error = %v, want ErrVersionConflict", err)
	}
	stored := newTestJob(t, jobSpec("j3", "stt"))
	stored.Persisted(1)
	if _, _, err := r.Enqueue(ctx, stored); err == nil {
		t.Fatal("Enqueue accepted a job that is already stored")
	}
	if n := countJobs(t, db); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestEnqueueConcurrentSameKey(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	created, ids := 0, map[shared.JobID]bool{}
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := jobSpec(fmt.Sprintf("j%d", i), "stt")
			s.Key = "generate:lesson-1"
			id, c, err := r.Enqueue(ctx, newTestJob(t, s))
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[id] = true
			if c {
				created++
			}
		}()
	}
	wg.Wait()
	if created != 1 || len(ids) != 1 || countJobs(t, db) != 1 {
		t.Fatalf("created=%d distinct ids=%d rows=%d, want 1 each", created, len(ids), countJobs(t, db))
	}
}

func TestGetMissing(t *testing.T) {
	r := NewJobRepository(openTest(t, nil))
	if _, err := r.Get(context.Background(), "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

func TestClaimMovesJobToRunning(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	enqueueJob(t, r, jobSpec("j1", "stt"))

	now := jobT0.Add(time.Second)
	j, ok, err := r.Claim(ctx, now, anyJobFilter(t, "stt"))
	if err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if j.Status() != job.StatusRunning || j.Attempts() != 1 || j.Version() != 2 || !j.StartedAt().Equal(now) {
		t.Fatalf("claimed job: %s attempts=%d version=%d started=%v", j.Status(), j.Attempts(), j.Version(), j.StartedAt())
	}
	stored, err := r.Get(ctx, "j1")
	if err != nil || stored.Status() != job.StatusRunning || stored.Version() != 2 || stored.Attempts() != 1 {
		t.Fatalf("stored job: %v %v", stored, err)
	}
	// The claimed instance can be saved at once, as the runner does after a progress report.
	p, _ := job.NewProgress(30, "chunk=3")
	if err := j.ReportProgress(p, now); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, j); err != nil || j.Version() != 3 {
		t.Fatalf("Save after Claim: %v version=%d", err, j.Version())
	}
	if _, ok, err := r.Claim(ctx, now, anyJobFilter(t, "stt")); ok || err != nil {
		t.Fatalf("second Claim: ok=%v err=%v", ok, err)
	}
}

func TestClaimMatchesFilterSemantics(t *testing.T) {
	types := []job.Type{"stt", "summarize"}
	heavy := []job.Type{"stt"}
	now := jobT0.Add(time.Hour)
	cases := []struct {
		name         string
		typ          string
		runNow       bool
		runAt        time.Time
		heavyAllowed bool
		filterTypes  []job.Type
		want         bool
	}{
		{"light on battery", "summarize", false, jobT0, false, types, true},
		{"heavy on ac", "stt", false, jobT0, true, types, true},
		{"heavy on battery", "stt", false, jobT0, false, types, false},
		{"heavy run now on battery", "stt", true, jobT0, false, types, true},
		{"unregistered type", "email", true, jobT0, true, types, false},
		{"not due", "summarize", false, now.Add(time.Second), true, types, false},
		{"due exactly now", "summarize", false, now, true, types, true},
		{"type not in filter", "summarize", false, jobT0, true, []job.Type{"stt"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewJobRepository(openTest(t, nil))
			s := jobSpec("j1", tc.typ)
			s.RunNow, s.RunAt = tc.runNow, tc.runAt
			enqueueJob(t, r, s)
			var f job.ClaimFilter
			if len(tc.filterTypes) == len(types) {
				f = jobFilter(t, tc.filterTypes, heavy, tc.heavyAllowed)
			} else {
				f = jobFilter(t, tc.filterTypes, nil, tc.heavyAllowed)
			}
			// The reference semantics of the domain agree with the expectation.
			if tc.runAt.Before(now.Add(time.Nanosecond)) && f.Admits(job.Type(tc.typ), tc.runNow) != tc.want {
				t.Fatalf("test expectation disagrees with ClaimFilter.Admits")
			}
			_, ok, err := r.Claim(context.Background(), now, f)
			if err != nil || ok != tc.want {
				t.Fatalf("Claim ok=%v err=%v, want ok=%v", ok, err, tc.want)
			}
		})
	}
}

func TestClaimOrderAndZeroFilter(t *testing.T) {
	r, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	late := jobSpec("late", "stt")
	late.RunAt = jobT0.Add(time.Minute)
	enqueueJob(t, r, late)
	enqueueJob(t, r, jobSpec("early", "stt"))
	now := jobT0.Add(time.Hour)

	if _, ok, err := r.Claim(ctx, now, job.ClaimFilter{}); ok || err != nil {
		t.Fatalf("zero filter: ok=%v err=%v", ok, err)
	}
	for _, want := range []shared.JobID{"early", "late"} {
		j, ok, err := r.Claim(ctx, now, anyJobFilter(t, "stt"))
		if err != nil || !ok || j.ID() != want {
			t.Fatalf("Claim = %v ok=%v err=%v, want %s", j, ok, err, want)
		}
	}
}

func TestCorruptHeadRowDoesNotStarveTheQueue(t *testing.T) {
	cases := []struct{ name, corrupt string }{
		{"restore fails", "UPDATE jobs SET pinned_model_id = 'a/b' WHERE id = 'a-bad'"},
		{"version zero", "UPDATE jobs SET version = 0 WHERE id = 'a-bad'"},
		{"attempts exhausted", "UPDATE jobs SET attempts = max_attempts WHERE id = 'a-bad'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			r, ctx := NewJobRepository(db), context.Background()
			enqueueJob(t, r, jobSpec("a-bad", "stt"))
			enqueueJob(t, r, jobSpec("b-good", "stt"))
			enqueueJob(t, r, jobSpec("c-good", "stt"))
			if _, err := db.sql.ExecContext(ctx, tc.corrupt); err != nil {
				t.Fatal(err)
			}
			now := jobT0.Add(time.Second)
			for _, want := range []shared.JobID{"b-good", "c-good"} {
				j, ok, err := r.Claim(ctx, now, anyJobFilter(t, "stt"))
				if err != nil || !ok || j.ID() != want {
					t.Fatalf("Claim = %v ok=%v err=%v, want %s", j, ok, err, want)
				}
			}
			if _, ok, err := r.Claim(ctx, now, anyJobFilter(t, "stt")); ok || err != nil {
				t.Fatalf("Claim with only the corrupt row left: ok=%v err=%v", ok, err)
			}
			if got := scanInt(t, db.sql, "SELECT count(*) FROM jobs WHERE id = 'a-bad' AND status = 'QUEUED'"); got != 1 {
				t.Fatalf("the corrupt row was changed")
			}
		})
	}
}

// An idle poll must not take the write slot: it has to work while another writer holds it.
func TestIdleClaimDoesNotWaitForTheWriteSlot(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, db *DB, r *JobRepository)
	}{
		{"empty queue", func(*testing.T, *DB, *JobRepository) {}},
		{"only a corrupt row", func(t *testing.T, db *DB, r *JobRepository) {
			enqueueJob(t, r, jobSpec("bad", "stt"))
			if _, err := db.sql.Exec("UPDATE jobs SET version = 0"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			r := NewJobRepository(db)
			tc.setup(t, db, r)

			held, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- db.Do(context.Background(), func(context.Context) error {
					close(held)
					<-release
					return nil
				})
			}()
			<-held
			defer func() {
				close(release)
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			if _, ok, err := r.Claim(ctx, jobT0, anyJobFilter(t, "stt")); ok || err != nil {
				t.Fatalf("Claim: ok=%v err=%v", ok, err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Fatalf("idle Claim took %v while another writer held the slot", d)
			}
		})
	}
}

func TestOnlyOneOfManyClaimersTakesTheJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db1 := openAt(t, path, nil)
	db2 := openAt(t, path, nil) // a second handle on the same file behaves like another process
	enqueueJob(t, NewJobRepository(db1), jobSpec("only", "stt"))

	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := db1
			if i%2 == 1 {
				db = db2
			}
			_, ok, err := NewJobRepository(db).Claim(context.Background(), jobT0, anyJobFilter(t, "stt"))
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d claimers got the job, want exactly 1", won)
	}
}

func TestConcurrentClaimersGetDistinctJobs(t *testing.T) {
	db := openTest(t, nil)
	r := NewJobRepository(db)
	const jobs, workers = 12, 20
	for i := range jobs {
		enqueueJob(t, r, jobSpec(fmt.Sprintf("j%02d", i), "stt"))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[shared.JobID]int{}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, ok, err := r.Claim(context.Background(), jobT0, anyJobFilter(t, "stt"))
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				got[j.ID()]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(got) != jobs {
		t.Fatalf("%d distinct jobs claimed, want %d", len(got), jobs)
	}
	for id, c := range got {
		if c != 1 {
			t.Errorf("job %s claimed %d times", id, c)
		}
	}
	running, _ := r.ListByStatus(context.Background(), job.StatusRunning)
	if len(running) != jobs {
		t.Fatalf("%d RUNNING rows, want %d", len(running), jobs)
	}
}

func TestStaleSaveIsVersionConflict(t *testing.T) {
	r, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	enqueueJob(t, r, jobSpec("j1", "stt"))
	a, _ := r.Get(ctx, "j1")
	b, _ := r.Get(ctx, "j1") // the user cancels while the runner works on its copy

	if err := b.Cancel(jobT0); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := a.Claim(jobT0); err != nil {
		t.Fatal(err)
	}
	err := r.Save(ctx, a)
	if !errors.Is(err, shared.ErrVersionConflict) || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("stale Save error = %v, want ErrVersionConflict", err)
	}
	if a.Version() != 1 {
		t.Fatalf("a failed Save changed the version to %d", a.Version())
	}
	stored, _ := r.Get(ctx, "j1")
	if stored.Status() != job.StatusCancelled {
		t.Fatalf("stored status = %s, want CANCELLED", stored.Status())
	}
}

func TestSaveNewAndPurgedJob(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()

	fresh := newTestJob(t, jobSpec("j1", "stt"))
	if err := r.Save(ctx, fresh); err != nil || fresh.Version() != 1 {
		t.Fatalf("first Save: %v version=%d", err, fresh.Version())
	}
	if err := r.Save(ctx, newTestJob(t, jobSpec("j1", "other"))); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("same id error = %v, want ErrVersionConflict", err)
	}
	sameKey := jobSpec("j2", "stt")
	sameKey.Key = fresh.Key()
	if err := r.Save(ctx, newTestJob(t, sameKey)); !errors.Is(err, shared.ErrDuplicate) || errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("same key error = %v, want ErrDuplicate", err)
	}
	mustExec(t, db, "DELETE FROM jobs WHERE id = 'j1'")
	if err := r.Save(ctx, fresh); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Save after purge error = %v, want ErrVersionConflict", err)
	}
	if n := countJobs(t, db); n != 0 {
		t.Fatalf("a stored job was inserted again: %d rows", n)
	}
}

// jobIn builds a job that reached status by legal moves, with every optional field set where the status allows.
func jobInStatus(t *testing.T, s job.Status) *job.Job {
	t.Helper()
	sp := jobSpec("j-"+string(s), "stt")
	sp.Payload = `{"recording":"r1"}`
	sp.PinnedModelID = "model-1"
	j := newTestJob(t, sp)
	p, _ := job.NewProgress(40, "chunk=4")
	later := jobT0.Add(time.Minute)
	steps := map[job.Status][]func() error{
		job.StatusQueued:    nil,
		job.StatusRunning:   {func() error { return j.Claim(later) }, func() error { return j.ReportProgress(p, later) }},
		job.StatusCompleted: {func() error { return j.Claim(later) }, func() error { return j.Complete(later) }},
		job.StatusRetryWait: {func() error { return j.Claim(later) }, func() error { return j.Retry(later, later.Add(time.Hour), "net") }},
		job.StatusCancelled: {func() error { return j.Cancel(later) }},
		job.StatusFailed:    {func() error { return j.Claim(later) }, func() error { return j.Fail(later, "bad input") }},
		job.StatusWaitingForNetwork: {
			func() error { return j.Claim(later) }, func() error { return j.WaitFor(job.StatusWaitingForNetwork, later) },
		},
		job.StatusWaitingForQuota: {
			func() error { return j.Claim(later) }, func() error { return j.WaitFor(job.StatusWaitingForQuota, later) },
		},
		job.StatusWaitingForPower: {func() error { return j.WaitFor(job.StatusWaitingForPower, later) }},
	}
	for _, step := range steps[s] {
		if err := step(); err != nil {
			t.Fatalf("reaching %s: %v", s, err)
		}
	}
	if j.Status() != s {
		t.Fatalf("reached %s, want %s", j.Status(), s)
	}
	return j
}

func requireSameJob(t *testing.T, got, want *job.Job) {
	t.Helper()
	g, w := got.Progress(), want.Progress()
	switch {
	case got.ID() != want.ID(), got.Type() != want.Type(), got.Status() != want.Status(), got.Key() != want.Key(),
		got.Payload() != want.Payload(), got.PinnedModelID() != want.PinnedModelID(),
		got.Attempts() != want.Attempts(), got.MaxAttempts() != want.MaxAttempts(), got.RunNow() != want.RunNow(),
		g != w, got.LastError() != want.LastError(), got.Cause() != want.Cause(), got.Version() != want.Version(),
		!got.NextRunAt().Equal(want.NextRunAt()), !got.CreatedAt().Equal(want.CreatedAt()),
		!got.StartedAt().Equal(want.StartedAt()), !got.FinishedAt().Equal(want.FinishedAt()),
		!got.UpdatedAt().Equal(want.UpdatedAt()):
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, want)
	}
}

// Every job.Cause is accepted by the CHECK of jobs.cause and round-trips through Save; another value is refused
// (TASK-083).
func TestAllCausesRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	for _, c := range job.Causes() {
		t.Run("cause "+string(c), func(t *testing.T) {
			j := jobInStatus(t, job.StatusRunning)
			if _, created, err := r.Enqueue(ctx, j); err != nil || !created {
				t.Fatalf("Enqueue: created=%v err=%v", created, err)
			}
			if err := j.WaitForCause(job.StatusWaitingForNetwork, c, jobT0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := r.Save(ctx, j); err != nil {
				t.Fatal(err)
			}
			got, err := r.Get(ctx, j.ID())
			if err != nil {
				t.Fatal(err)
			}
			requireSameJob(t, got, j)
			mustExec(t, db, "DELETE FROM jobs WHERE id = '"+string(j.ID())+"'")
		})
	}

	j := jobInStatus(t, job.StatusRetryWait)
	if _, _, err := r.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE jobs SET cause = 'bogus' WHERE id = ?", string(j.ID())); err == nil {
		t.Fatal("the CHECK of jobs.cause accepted an unknown cause")
	}
}

func TestAllStatusesRoundTrip(t *testing.T) {
	r, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	for _, s := range job.Statuses() {
		t.Run(s.String(), func(t *testing.T) {
			j := jobInStatus(t, s)
			if _, created, err := r.Enqueue(ctx, j); err != nil || !created {
				t.Fatalf("Enqueue: created=%v err=%v", created, err)
			}
			got, err := r.Get(ctx, j.ID())
			if err != nil {
				t.Fatal(err)
			}
			requireSameJob(t, got, j)
			if s.IsTerminal() != !got.FinishedAt().IsZero() {
				t.Fatalf("finishedAt %v for %s", got.FinishedAt(), s)
			}

			// A change of the stored job round-trips too.
			if !s.IsTerminal() {
				if err := got.Cancel(jobT0.Add(time.Hour)); err == nil {
					if err := r.Save(ctx, got); err != nil {
						t.Fatal(err)
					}
					again, _ := r.Get(ctx, j.ID())
					requireSameJob(t, again, got)
				}
			}
		})
	}
}

func TestListByStatus(t *testing.T) {
	r, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	for _, s := range []job.Status{job.StatusQueued, job.StatusRunning, job.StatusWaitingForPower, job.StatusCompleted} {
		if _, _, err := r.Enqueue(ctx, jobInStatus(t, s)); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name     string
		statuses []job.Status
		want     int
	}{
		{"none", nil, 0},
		{"one", []job.Status{job.StatusRunning}, 1},
		{"active", job.ActiveStatuses(), 3},
		{"terminal and queued", []job.Status{job.StatusCompleted, job.StatusQueued}, 2},
		{"absent", []job.Status{job.StatusFailed}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.ListByStatus(ctx, tc.statuses...)
			if err != nil || len(got) != tc.want {
				t.Fatalf("ListByStatus = %d jobs, %v; want %d", len(got), err, tc.want)
			}
		})
	}
}

func TestListByStatusTolerantReportsCorruptRows(t *testing.T) {
	var logs bytes.Buffer
	db := openTest(t, func(o *Options) { o.Logger = slog.New(slog.NewTextHandler(&logs, nil)) })
	r, ctx := NewJobRepository(db), context.Background()
	for _, id := range []string{"a-good", "b-pin", "c-scan", "d-nostart", "e-queued", "f-good", "g-noid", "h-noid"} {
		enqueueJob(t, r, jobSpec(id, "stt"))
	}
	mustExec(t, db, "UPDATE jobs SET status = 'RUNNING', attempts = 1, started_at = created_at WHERE id <> 'e-queued'")
	mustExec(t, db, "UPDATE jobs SET pinned_model_id = 'a/b' WHERE id = 'b-pin'")
	mustExec(t, db, "UPDATE jobs SET next_run_at = 'not a number' WHERE id = 'c-scan'")
	mustExec(t, db, "UPDATE jobs SET started_at = NULL WHERE id = 'd-nostart'")
	mustExec(t, db, "UPDATE jobs SET id = NULL WHERE id IN ('g-noid', 'h-noid')")
	logs.Reset()

	if _, err := r.ListByStatus(ctx, job.StatusRunning); err == nil {
		t.Fatal("ListByStatus accepted corrupt rows; the tolerant variant would be pointless")
	}

	jobs, corrupt, err := r.ListByStatusTolerant(ctx, job.StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	var got []shared.JobID
	for _, j := range jobs {
		got = append(got, j.ID())
	}
	if fmt.Sprint(got) != "[a-good f-good]" || fmt.Sprint(corrupt) != "[b-pin c-scan d-nostart]" {
		t.Fatalf("jobs = %v, corrupt = %v", got, corrupt)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM jobs WHERE status = 'RUNNING'"); n != 7 {
		t.Fatalf("RUNNING rows = %d after listing, want 7 (rows are reported, not changed)", n)
	}
	if got := strings.Count(logs.String(), "without an id"); got != 1 || !strings.Contains(logs.String(), "count=2") {
		t.Fatalf("NULL-id rows must be logged once with their count:\n%s", logs.String())
	}
}

func TestListByStatusTolerant(t *testing.T) {
	db := openTest(t, nil)
	r := NewJobRepository(db)
	enqueueJob(t, r, jobSpec("queued", "stt"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name     string
		ctx      context.Context
		statuses []job.Status
		want     int
		wantErr  error
	}{
		{"no statuses", context.Background(), nil, 0, nil},
		{"absent status", context.Background(), []job.Status{job.StatusRunning}, 0, nil},
		{"present status", context.Background(), []job.Status{job.StatusQueued}, 1, nil},
		{"ended context fails the whole call", cancelled, []job.Status{job.StatusQueued}, 0, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs, corrupt, err := r.ListByStatusTolerant(tc.ctx, tc.statuses...)
			if !errors.Is(err, tc.wantErr) || len(jobs) != tc.want || len(corrupt) != 0 {
				t.Fatalf("jobs=%d corrupt=%v err=%v; want %d jobs, err %v", len(jobs), corrupt, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestQueueSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db := openAt(t, path, nil)
	r, ctx := NewJobRepository(db), context.Background()
	enqueueJob(t, r, jobSpec("queued", "stt"))
	enqueueJob(t, r, jobSpec("running", "summarize"))
	if _, ok, err := r.Claim(ctx, jobT0, anyJobFilter(t, "summarize")); !ok || err != nil {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	r2 := NewJobRepository(openAt(t, path, nil))
	queued, err := r2.Get(ctx, "queued")
	if err != nil || queued.Status() != job.StatusQueued {
		t.Fatalf("queued job after restart: %v %v", queued, err)
	}
	stale, err := r2.ListByStatus(ctx, job.StatusRunning)
	if err != nil || len(stale) != 1 || stale[0].ID() != "running" {
		t.Fatalf("RUNNING jobs after restart: %v %v", stale, err)
	}
	// The recovery of a stale job is a domain transition saved with the stored version.
	if err := stale[0].Requeue(jobT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r2.Save(ctx, stale[0]); err != nil {
		t.Fatal(err)
	}
	if j, ok, err := r2.Claim(ctx, jobT0.Add(time.Hour), anyJobFilter(t, "stt", "summarize")); !ok || err != nil || j.Attempts() == 0 {
		t.Fatalf("Claim after recovery: %v ok=%v err=%v", j, ok, err)
	}
}

func TestJobWritesJoinTheOuterTransaction(t *testing.T) {
	db := openTest(t, nil)
	r := NewJobRepository(db)
	sentinel := errors.New("rollback")
	err := db.Do(context.Background(), func(ctx context.Context) error {
		if _, _, err := r.Enqueue(ctx, newTestJob(t, jobSpec("j1", "stt"))); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || countJobs(t, db) != 0 {
		t.Fatalf("err=%v rows=%d, want rollback", err, countJobs(t, db))
	}
}

// Version 10 adds jobs.cause: stored jobs get no cause, and going down removes the column and keeps the rows.
func TestMigrationToVersion10AddsJobCause(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 9); err != nil {
		t.Fatalf("up to 9: %v", err)
	}
	mustExec(t, db, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
		VALUES ('j1', 'summarize', 'WAITING_FOR_NETWORK', 'k1', 3, 0, 0, 0)`)

	if _, err := p.UpTo(ctx, 10); err != nil {
		t.Fatalf("migrate to 10: %v", err)
	}
	if v, err := db.SchemaVersion(ctx); err != nil || v != 10 {
		t.Fatalf("SchemaVersion = %d, %v; want 10", v, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM jobs WHERE id = 'j1' AND cause = ''"); n != 1 {
		t.Fatal("a stored job did not get the empty cause")
	}

	if _, err := p.DownTo(ctx, 9); err != nil {
		t.Fatalf("down to 9: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('jobs') WHERE name = 'cause'"); n != 0 {
		t.Fatal("jobs.cause still exists at version 9")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM jobs"); n != 1 {
		t.Fatalf("jobs = %d, want the job kept", n)
	}
}
