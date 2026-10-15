package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// completedTestRecording is a COMPLETED recording id with its final track.
func completedTestRecording(t *testing.T, id string) *recording.Recording {
	t.Helper()
	r := newTestRecording(t, id, "")
	at := recT0.Add(time.Hour)
	for _, step := range []func() error{
		func() error { return r.Begin(recT0) }, func() error { return r.Stop(at) }, func() error { return r.Finalize(at) },
		func() error { return r.Complete(at, recRef(t, "recordings/"+id+".wav"), time.Hour) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// The sweep lists COMPLETED recordings with a track and no transcript, in id order, page by page.
func TestListUntranscribed(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	for _, id := range []string{"r3", "r1", "r4", "r2"} {
		saveRecording(t, repo, completedTestRecording(t, id))
	}
	for _, s := range []recording.Status{recording.StatusRecording, recording.StatusFailed} {
		saveRecording(t, repo, recordingInStatus(t, s))
	}
	mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_name, model_version, text_ref, created_at)
		VALUES ('t1', 'r2', 'small', '1', 'transcripts/t1.json', 0)`)

	tests := []struct {
		name  string
		after shared.RecordingID
		limit int
		want  []shared.RecordingID
	}{
		{"first page", "", 2, []shared.RecordingID{"r1", "r3"}},
		{"next page", "r3", 2, []shared.RecordingID{"r4"}},
		{"everything", "", 10, []shared.RecordingID{"r1", "r3", "r4"}},
		{"past the end", "r4", 10, []shared.RecordingID{}},
		{"no limit is nothing", "", 0, []shared.RecordingID{}},
		{"negative limit is nothing", "", -1, []shared.RecordingID{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ListUntranscribed(ctx, tt.after, tt.limit)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("ListUntranscribed(%q, %d) = %v, %v; want %v", tt.after, tt.limit, got, err, tt.want)
			}
		})
	}
}

// HasKeyPrefix finds a job of the type by the start of its key, in any status, and nothing of another type.
func TestHasKeyPrefix(t *testing.T) {
	db := openTest(t, nil)
	r, ctx := NewJobRepository(db), context.Background()
	stt := enqueueJob(t, r, job.Spec{ID: "j1", Type: "stt", Key: "stt:rec-1:m1", MaxAttempts: 3})
	enqueueJob(t, r, job.Spec{ID: "j2", Type: "summarize", Key: "stt:rec-2:m1", MaxAttempts: 3})
	if err := stt.Cancel(jobT0); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, stt); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		typ    job.Type
		prefix string
		want   bool
	}{
		{"the recording's job, cancelled", "stt", "stt:rec-1:", true},
		{"the whole key", "stt", "stt:rec-1:m1", true},
		{"another recording", "stt", "stt:rec-10:", false},
		{"any plain prefix", "stt", "stt:rec-", true},
		{"another type", "stt", "stt:rec-2:", false},
		{"longer than any key", "stt", "stt:rec-1:m1:x", false},
		{"case matters", "stt", "STT:rec-1:", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.HasKeyPrefix(ctx, tt.typ, tt.prefix)
			if err != nil || got != tt.want {
				t.Fatalf("HasKeyPrefix(%s, %q) = %v, %v; want %v", tt.typ, tt.prefix, got, err, tt.want)
			}
		})
	}
}
