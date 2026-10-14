package sqlite

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Rows are created at T0+i minutes; r2 and r3 share a creation time, so the id breaks the tie.
var recentOffsets = map[string]time.Duration{"r0": 0, "r1": time.Minute, "r2": 2 * time.Minute, "r3": 2 * time.Minute}

func TestRecordingListRecent(t *testing.T) {
	repo, ctx := NewRecordingRepository(openTest(t, nil)), context.Background()
	for id, off := range recentOffsets {
		r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), MaxDuration: time.Hour}, recT0.Add(off))
		if err != nil {
			t.Fatal(err)
		}
		saveRecording(t, repo, r)
	}

	tests := []struct {
		name  string
		limit int
		want  []shared.RecordingID
	}{
		{name: "all, newest first", limit: 10, want: []shared.RecordingID{"r3", "r2", "r1", "r0"}},
		{name: "bounded", limit: 2, want: []shared.RecordingID{"r3", "r2"}},
		{name: "zero lists nothing", limit: 0, want: nil},
		{name: "negative lists nothing", limit: -1, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListRecent(ctx, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("ListRecent returned nil, want an empty slice")
			}
			ids := make([]shared.RecordingID, 0, len(got))
			for _, r := range got {
				ids = append(ids, r.ID())
			}
			if !equalIDs(ids, tt.want) {
				t.Fatalf("ListRecent(%d) = %v, want %v", tt.limit, ids, tt.want)
			}
		})
	}
}

func TestRecordingListRecentFailsOnCorruptRow(t *testing.T) {
	db := openTest(t, nil)
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, version, created_at, updated_at)
		VALUES ('bad', 'FAILED', 1, 1000, 'secrets/bad', 1, 1, 1)`)
	if _, err := NewRecordingRepository(db).ListRecent(context.Background(), 10); err == nil {
		t.Fatal("a corrupt row was listed")
	}
}

func TestJobListRecent(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewJobRepository(db), context.Background()
	for id, off := range recentOffsets {
		j, err := job.New(jobSpec("j"+id[1:], "stt"), jobT0.Add(off))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		limit int
		want  []shared.JobID
	}{
		{name: "all, newest first", limit: 10, want: []shared.JobID{"j3", "j2", "j1", "j0"}},
		{name: "bounded", limit: 3, want: []shared.JobID{"j3", "j2", "j1"}},
		{name: "zero lists nothing", limit: 0, want: nil},
		{name: "negative lists nothing", limit: -5, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListRecent(ctx, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("ListRecent returned nil, want an empty slice")
			}
			ids := make([]shared.JobID, 0, len(got))
			for _, j := range got {
				ids = append(ids, j.ID())
			}
			if !equalIDs(ids, tt.want) {
				t.Fatalf("ListRecent(%d) = %v, want %v", tt.limit, ids, tt.want)
			}
		})
	}

}

// The listed job is the stored aggregate, not a partial view.
func TestJobListRecentRoundTrip(t *testing.T) {
	repo, ctx := NewJobRepository(openTest(t, nil)), context.Background()
	stored := jobInStatus(t, job.StatusWaitingForPower)
	if _, _, err := repo.Enqueue(ctx, stored); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListRecent(ctx, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListRecent(1) = %d jobs, %v", len(got), err)
	}
	requireSameJob(t, got[0], stored)
}

func TestJobListRecentFailsOnCorruptRow(t *testing.T) {
	db := openTest(t, nil)
	repo := NewJobRepository(db)
	enqueueJob(t, repo, jobSpec("ok", "stt"))
	mustExec(t, db, "UPDATE jobs SET pinned_model_id = 'a/b' WHERE id = 'ok'")
	if _, err := repo.ListRecent(context.Background(), 10); err == nil {
		t.Fatal("a corrupt row was listed")
	}
}

func equalIDs[T ~string](got, want []T) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
