package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
	"paratech/desktop/internal/domain/shared"
)

func newTestNotice(t *testing.T, id string, status job.Status, at time.Time) *notice.Notice {
	t.Helper()
	n, err := notice.New(shared.JobID(id), "stt", status, at)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNoticeRecordIsFirstOnlyOnce(t *testing.T) {
	db := openTest(t, nil)
	ctx, r := context.Background(), NewNoticeRepository(db)
	enqueueJob(t, NewJobRepository(db), jobSpec("j1", "stt"))

	steps := []struct {
		name   string
		notice *notice.Notice
		first  bool
	}{
		{"first completion", newTestNotice(t, "j1", job.StatusCompleted, jobT0), true},
		{"same job and status again", newTestNotice(t, "j1", job.StatusCompleted, jobT0.Add(time.Minute)), false},
		{"same job, other status", newTestNotice(t, "j1", job.StatusFailed, jobT0.Add(time.Hour)), true},
	}
	for _, s := range steps {
		first, err := r.Record(ctx, s.notice)
		if err != nil || first != s.first {
			t.Fatalf("%s: first=%v err=%v, want first=%v", s.name, first, err, s.first)
		}
	}

	// The first notice stays as it was: a repeat does not move its time.
	got, err := r.Get(ctx, "j1", job.StatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	if got.State() != steps[0].notice.State() {
		t.Fatalf("stored %+v, want the first %+v", got.State(), steps[0].notice.State())
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM job_notices"); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
}

func TestNoticeOfUnknownJobIsConflict(t *testing.T) {
	r := NewNoticeRepository(openTest(t, nil))
	_, err := r.Record(context.Background(), newTestNotice(t, "nope", job.StatusCompleted, jobT0))
	if !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict (foreign key)", err)
	}
	if _, err := r.Record(context.Background(), nil); err == nil {
		t.Fatal("a nil notice was recorded")
	}
}

func TestNoticeGetMissingIsNotFound(t *testing.T) {
	r := NewNoticeRepository(openTest(t, nil))
	if _, err := r.Get(context.Background(), "j1", job.StatusCompleted); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNoticeSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db := openAt(t, path, nil)
	enqueueJob(t, NewJobRepository(db), jobSpec("j1", "stt"))
	if first, err := NewNoticeRepository(db).Record(context.Background(), newTestNotice(t, "j1", job.StatusFailed, jobT0)); !first || err != nil {
		t.Fatalf("Record: first=%v err=%v", first, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	r := NewNoticeRepository(openAt(t, path, nil))
	first, err := r.Record(context.Background(), newTestNotice(t, "j1", job.StatusFailed, jobT0.Add(time.Hour)))
	if first || err != nil {
		t.Fatalf("Record after restart: first=%v err=%v, want false", first, err)
	}
}

func TestConcurrentRecordsHaveOneFirst(t *testing.T) {
	db := openTest(t, func(o *Options) { o.MaxOpenConns = 4 })
	enqueueJob(t, NewJobRepository(db), jobSpec("j1", "stt"))
	r, n := NewNoticeRepository(db), newTestNotice(t, "j1", job.StatusCompleted, jobT0)

	const callers = 8
	var firsts atomic.Int32
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			first, err := r.Record(context.Background(), n)
			if err != nil {
				t.Errorf("Record: %v", err)
			}
			if first {
				firsts.Add(1)
			}
		})
	}
	wg.Wait()
	if n := firsts.Load(); n != 1 {
		t.Fatalf("%d callers were first, want exactly 1", n)
	}
}

func TestNoticeSchemaRejectsOtherStatuses(t *testing.T) {
	db := openTest(t, nil)
	enqueueJob(t, NewJobRepository(db), jobSpec("j1", "stt"))
	for _, s := range job.Statuses() {
		_, err := db.sql.ExecContext(context.Background(),
			"INSERT INTO job_notices (job_id, status, job_type, notified_at) VALUES ('j1', ?, 'stt', 0)", string(s))
		if want := notice.Notifiable(s); want != (err == nil) {
			t.Errorf("status %s: insert err = %v, want accepted %v", s, err, want)
		}
	}
}
