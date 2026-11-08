package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// ListByKeyPrefix lists every job of the type whose key starts with the prefix, in any status, oldest first, then
// by id; nothing of another type or another recording (TASK-392).
func TestListByKeyPrefix(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	add := func(id, typ, key string, created time.Duration) *job.Job {
		t.Helper()
		j, err := job.New(job.Spec{ID: shared.JobID(id), Type: job.Type(typ), Key: key, MaxAttempts: 3}, jobT0.Add(created))
		if err != nil {
			t.Fatal(err)
		}
		if _, created, err := r.Enqueue(ctx, j); err != nil || !created {
			t.Fatalf("Enqueue %s: created=%v err=%v", id, created, err)
		}
		return j
	}
	first := add("j1", "stt", "stt:rec-1:m1", 0)
	add("j3", "stt", "stt:rec-1:m1:r2", time.Minute)
	add("j2", "stt", "stt:rec-1:m2:r3", time.Minute) // same time: the id orders
	add("j4", "stt", "stt:rec-10:m1", 0)
	add("j5", "summarize", "stt:rec-1:x", 0)
	if err := first.Cancel(jobT0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, first); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		typ    job.Type
		prefix string
		want   []shared.JobID
	}{
		{"the recording's jobs in order", "stt", "stt:rec-1:", []shared.JobID{"j1", "j2", "j3"}},
		{"another recording", "stt", "stt:rec-10:", []shared.JobID{"j4"}},
		{"none", "stt", "stt:rec-2:", []shared.JobID{}},
		{"another type", "summarize", "stt:rec-1:", []shared.JobID{"j5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.ListByKeyPrefix(ctx, tt.typ, tt.prefix)
			if err != nil {
				t.Fatal(err)
			}
			ids := []shared.JobID{}
			for _, j := range got {
				ids = append(ids, j.ID())
			}
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("ListByKeyPrefix(%s, %q) = %v, want %v", tt.typ, tt.prefix, ids, tt.want)
			}
		})
	}

	got, err := r.ListByKeyPrefix(ctx, "stt", "stt:rec-1:m1")
	if err != nil || len(got) != 2 || got[0].Status() != job.StatusCancelled || !got[0].CreatedAt().Equal(jobT0) {
		t.Fatalf("restored job: %v, %v", got, err)
	}
}
