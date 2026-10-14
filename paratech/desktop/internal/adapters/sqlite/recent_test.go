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
			got, unreadable, err := repo.ListActivity(ctx, tt.maxTerminal)
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
	got, _, err := repo.ListActivity(ctx, 0)
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
	got, _, err := repo.ListActivity(ctx, 1)
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

	got, unreadable, err := repo.ListActivity(context.Background(), 10)
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
	if _, _, err := NewJobRepository(openTest(t, nil)).ListActivity(ctx, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
