package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Terminal rows t0..t3 are created at T0+i minutes; t2 and t3 share a creation time, so the id breaks the tie.
// "old" is unfinished and older than all of them, "new" is unfinished and newer.
var activityOffsets = map[string]time.Duration{
	"old": -time.Hour, "t0": 0, "t1": time.Minute, "t2": 2 * time.Minute, "t3": 2 * time.Minute, "new": 3 * time.Minute,
}

// activityCases: an unfinished row is always listed and never takes a slot of the terminal bound.
var activityCases = []struct {
	name        string
	maxTerminal int
	want        string
}{
	{name: "all, newest first", maxTerminal: 10, want: "[new t3 t2 t1 t0 old]"},
	{name: "bounded terminal part keeps the old unfinished row", maxTerminal: 2, want: "[new t3 t2 old]"},
	{name: "zero lists only unfinished rows", maxTerminal: 0, want: "[new old]"},
	{name: "negative lists only unfinished rows", maxTerminal: -1, want: "[new old]"},
}

func TestRecordingListActivity(t *testing.T) {
	repo, ctx := NewRecordingRepository(openTest(t, nil)), context.Background()
	for id, off := range activityOffsets {
		at := recT0.Add(off)
		r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), MaxDuration: time.Hour}, at)
		if err != nil {
			t.Fatal(err)
		}
		if id != "old" && id != "new" {
			if err := r.Fail(at, "device lost"); err != nil {
				t.Fatal(err)
			}
		}
		saveRecording(t, repo, r)
	}

	for _, tt := range activityCases {
		t.Run(tt.name, func(t *testing.T) {
			got, unreadable, err := repo.ListActivity(ctx, tt.maxTerminal)
			if err != nil || len(unreadable) != 0 {
				t.Fatalf("ListActivity: unreadable = %v, err = %v", unreadable, err)
			}
			ids := make([]shared.RecordingID, 0, len(got))
			for _, r := range got {
				ids = append(ids, r.ID())
			}
			if fmt.Sprint(ids) != tt.want {
				t.Fatalf("ListActivity(%d) = %v, want %v", tt.maxTerminal, ids, tt.want)
			}
		})
	}
}

// One row that cannot be read does not hide the others; an unfinished one beyond the bound is still reported.
func TestRecordingListActivitySkipsUnreadableRows(t *testing.T) {
	db := openTest(t, nil)
	repo := NewRecordingRepository(db)
	good, err := recording.New(recording.Spec{ID: "good", MaxDuration: time.Hour}, recT0)
	if err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, good)
	ms := recT0.UnixMilli()
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, version, created_at, updated_at)
		VALUES ('bad-restore', 'FAILED', ?, 1000, 'secrets/bad', 1, ?, ?)`, ms, ms+2, ms+2)
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, version, created_at, updated_at)
		VALUES ('bad-scan', 'FAILED', 'not a number', 1000, 'recordings/bad-scan', 1, ?, ?)`, ms+1, ms+1)
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, version, created_at, updated_at)
		VALUES ('bad-old-active', 'RECORDING', 1, 1000, 'secrets/old', 1, 1, 1)`)

	got, unreadable, err := repo.ListActivity(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "good" {
		t.Fatalf("listed %d recordings, want only the readable one", len(got))
	}
	if fmt.Sprint(unreadable) != "[bad-restore bad-scan bad-old-active]" {
		t.Fatalf("unreadable = %v", unreadable)
	}
}

func TestRecordingListActivityFailsOnQueryError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewRecordingRepository(openTest(t, nil)).ListActivity(ctx, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestJobListActivity(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	for id, off := range activityOffsets {
		at := jobT0.Add(off)
		j, err := job.New(jobSpec(id, "stt"), at)
		if err != nil {
			t.Fatal(err)
		}
		switch id {
		case "old": // the old waiting job whose "Run now" must stay reachable (AC-22)
			err = j.WaitFor(job.StatusWaitingForPower, at)
		case "new":
		default:
			err = j.Cancel(at)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	for _, tt := range activityCases {
		t.Run(tt.name, func(t *testing.T) {
			got, unreadable, err := repo.ListActivity(ctx, tt.maxTerminal, nil)
			if err != nil || len(unreadable) != 0 {
				t.Fatalf("ListActivity: unreadable = %v, err = %v", unreadable, err)
			}
			ids := make([]shared.JobID, 0, len(got))
			for _, j := range got {
				ids = append(ids, j.ID())
			}
			if fmt.Sprint(ids) != tt.want {
				t.Fatalf("ListActivity(%d) = %v, want %v", tt.maxTerminal, ids, tt.want)
			}
		})
	}
}

// Every non-terminal status is listed beyond the bound; the terminal ones only within it.
func TestJobListActivityCoversEveryStatus(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	for _, s := range job.Statuses() {
		if _, _, err := repo.Enqueue(ctx, jobInStatus(t, s)); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := repo.ListActivity(ctx, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[job.Status]bool{}
	for _, j := range got {
		listed[j.Status()] = true
	}
	for _, s := range job.Statuses() {
		if listed[s] == s.IsTerminal() {
			t.Errorf("%s: listed = %v with no terminal slots", s, listed[s])
		}
	}
}

// The listed job is the stored aggregate, not a partial view.
func TestJobListActivityRoundTrip(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	stored := jobInStatus(t, job.StatusWaitingForPower)
	if _, _, err := repo.Enqueue(ctx, stored); err != nil {
		t.Fatal(err)
	}
	got, _, err := repo.ListActivity(ctx, 1, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListActivity(1) = %d jobs, %v", len(got), err)
	}
	requireSameJob(t, got[0], stored)
}

func TestJobListActivitySkipsUnreadableRows(t *testing.T) {
	db := openTest(t, nil)
	repo := NewJobRepository(db)
	for _, id := range []string{"a-good", "b-pin", "c-scan", "d-good"} {
		enqueueJob(t, repo, jobSpec(id, "stt"))
	}
	mustExec(t, db, "UPDATE jobs SET pinned_model_id = 'a/b' WHERE id = 'b-pin'")
	mustExec(t, db, "UPDATE jobs SET next_run_at = 'not a number' WHERE id = 'c-scan'")

	got, unreadable, err := repo.ListActivity(context.Background(), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]shared.JobID, 0, len(got))
	for _, j := range got {
		ids = append(ids, j.ID())
	}
	// Same creation time: newest first means the id descending.
	if fmt.Sprint(ids) != "[d-good a-good]" || fmt.Sprint(unreadable) != "[c-scan b-pin]" {
		t.Fatalf("jobs = %v, unreadable = %v", ids, unreadable)
	}
}

func TestJobListActivityFailsOnQueryError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewJobRepository(openTest(t, nil)).ListActivity(ctx, 10, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// Hidden types are left out of the unfinished and the finished part, and their finished jobs never use up the bound
// (TASK-074: a hundred finished syncs must not push the real jobs out of the list).
func TestJobListActivityLeavesOutHiddenTypes(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	add := func(id, typ string, at time.Time, finished bool) {
		t.Helper()
		j, err := job.New(jobSpec(id, typ), at)
		if err != nil {
			t.Fatal(err)
		}
		if finished {
			if err := j.Cancel(at); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := repo.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	add("stt-done", "stt", jobT0, true)
	add("send-done", "send_email", jobT0.Add(time.Minute), true)
	for i := range 5 { // newer than every real job
		add(fmt.Sprintf("sync-done-%d", i), "calendar_sync", jobT0.Add(time.Hour+time.Duration(i)*time.Minute), true)
	}
	add("sync-queued", "calendar_sync", jobT0.Add(2*time.Hour), false)
	add("stt-queued", "stt", jobT0.Add(-time.Hour), false)

	tests := []struct {
		name        string
		maxTerminal int
		hidden      []job.Type
		want        string
	}{
		{name: "hidden sync, bound of two", maxTerminal: 2, hidden: []job.Type{"calendar_sync"}, want: "[send-done stt-done stt-queued]"},
		{name: "hidden sync, bound of one", maxTerminal: 1, hidden: []job.Type{"calendar_sync"}, want: "[send-done stt-queued]"},
		{name: "two hidden types", maxTerminal: 10, hidden: []job.Type{"calendar_sync", "send_email"}, want: "[stt-done stt-queued]"},
		{name: "nothing hidden", maxTerminal: 2, want: "[sync-queued sync-done-4 sync-done-3 stt-queued]"},
		{name: "empty list hides nothing", maxTerminal: 1, hidden: []job.Type{}, want: "[sync-queued sync-done-4 stt-queued]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, unreadable, err := repo.ListActivity(ctx, tt.maxTerminal, tt.hidden)
			if err != nil || len(unreadable) != 0 {
				t.Fatalf("ListActivity: unreadable = %v, err = %v", unreadable, err)
			}
			ids := make([]shared.JobID, 0, len(got))
			for _, j := range got {
				ids = append(ids, j.ID())
			}
			if fmt.Sprint(ids) != tt.want {
				t.Fatalf("ListActivity(%d, %v) = %v, want %v", tt.maxTerminal, tt.hidden, ids, tt.want)
			}
		})
	}
}

// LastFinished is the finished job of the type changed last; unfinished jobs and other types do not count.
func TestJobLastFinished(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	if j, found, err := repo.LastFinished(ctx, "calendar_sync", nil); err != nil || found || j != nil {
		t.Fatalf("empty store: LastFinished = %v, %v, %v", j, found, err)
	}

	// Created in one order, finished in another: the last change decides.
	done := newTestJob(t, jobSpec("a-done", "calendar_sync"))
	failed := newTestJob(t, jobSpec("b-failed", "calendar_sync"))
	for _, step := range []error{
		done.Claim(jobT0), done.Complete(jobT0.Add(2 * time.Minute)),
		failed.Claim(jobT0), failed.Fail(jobT0.Add(time.Minute), "boom"),
	} {
		if step != nil {
			t.Fatal(step)
		}
	}
	running := newTestJob(t, jobSpec("c-running", "calendar_sync"))
	if err := running.Claim(jobT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	other := newTestJob(t, jobSpec("d-stt", "stt"))
	if err := other.Cancel(jobT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, j := range []*job.Job{done, failed, running, other} {
		if _, _, err := repo.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	j, found, err := repo.LastFinished(ctx, "calendar_sync", nil)
	if err != nil || !found {
		t.Fatalf("LastFinished = %v, %v", found, err)
	}
	requireSameJob(t, j, done)

	// A later failure becomes the last one.
	failed2 := newTestJob(t, jobSpec("e-failed", "calendar_sync"))
	if err := failed2.Claim(jobT0); err != nil {
		t.Fatal(err)
	}
	if err := failed2.Fail(jobT0.Add(3*time.Minute), "boom"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Enqueue(ctx, failed2); err != nil {
		t.Fatal(err)
	}
	if j, _, err := repo.LastFinished(ctx, "calendar_sync", nil); err != nil || j.ID() != "e-failed" || j.Attempts() != 1 {
		t.Fatalf("LastFinished = %v, %v; want e-failed after 1 attempt", j, err)
	}

	if j, found, err := repo.LastFinished(ctx, "stt", nil); err != nil || !found || j.ID() != "d-stt" {
		t.Fatalf("LastFinished(stt) = %v, %v, %v", j, found, err)
	}

	// The jobs of except are left out: the next one by its last change answers, none when all are left out.
	if j, _, err := repo.LastFinished(ctx, "calendar_sync", []shared.JobID{"e-failed", "x-unknown"}); err != nil ||
		j.ID() != "a-done" {
		t.Fatalf("LastFinished except e-failed = %v, %v; want a-done", j, err)
	}
	all := []shared.JobID{"a-done", "b-failed", "e-failed"}
	if j, found, err := repo.LastFinished(ctx, "calendar_sync", all); err != nil || found || j != nil {
		t.Fatalf("LastFinished except all = %v, %v, %v", j, found, err)
	}
}

func TestJobLastFinishedErrors(t *testing.T) {
	db := openTest(t, nil)
	repo := NewJobRepository(db)
	j := newTestJob(t, jobSpec("bad", "calendar_sync"))
	if err := j.Cancel(jobT0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Enqueue(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE jobs SET pinned_model_id = 'a/b' WHERE id = 'bad'")
	if _, _, err := repo.LastFinished(context.Background(), "calendar_sync", nil); err == nil {
		t.Fatal("LastFinished of an unreadable row succeeded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := repo.LastFinished(ctx, "calendar_sync", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
