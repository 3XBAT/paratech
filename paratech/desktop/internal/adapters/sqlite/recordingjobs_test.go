package sqlite

// The jobs of the processing chain of recordings (TASK-076: one row per recording with the status of its chain).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	coursework "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

func TestJobListForRecordings(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewJobRepository(db), context.Background()
	recs := NewRecordingRepository(db)
	for _, id := range []string{"r1", "r2", "r3"} {
		saveRecording(t, recs, newTestRecording(t, id, ""))
	}
	for _, tr := range []struct{ id, rec string }{{"t1", "r1"}, {"t1b", "r1"}, {"t2", "r2"}, {"t3", "r3"}} {
		mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_name, model_version, text_ref, created_at)
			VALUES (?, ?, 'base', '1', 'transcripts/x', 0)`, tr.id, tr.rec)
	}

	// The payloads are the real ones of the domain values, so a change of their layout breaks this test.
	sttOf := func(rec string) string {
		req, err := transcription.NewSTTRequest(shared.RecordingID(rec))
		if err != nil {
			t.Fatal(err)
		}
		return req.Payload()
	}
	postOf := func(tr string) string {
		req, err := coursework.NewPostProcessRequest(shared.TranscriptID(tr))
		if err != nil {
			t.Fatal(err)
		}
		return req.Payload()
	}
	add := func(id string, typ job.Type, payload string) {
		t.Helper()
		s := jobSpec(id, string(typ))
		s.Payload = payload
		enqueueJob(t, repo, s)
	}
	add("stt-r1", transcription.STTJobType, sttOf("r1"))
	add("stt-r2", transcription.STTJobType, sttOf("r2"))
	add("stt-r3", transcription.STTJobType, sttOf("r3")) // not asked for
	add("sum-t1", coursework.SummarizeJobType, postOf("t1"))
	add("hw-t1b", coursework.ExtractHomeworkJobType, postOf("t1b"))
	add("sum-t3", coursework.SummarizeJobType, postOf("t3")) // not asked for
	add("other-r1", "homework_draft", sttOf("r1"))           // a type that is not asked for
	add("sum-gone", coursework.SummarizeJobType, postOf("t-missing"))

	got, unreadable, err := repo.ListForRecordings(ctx, []shared.RecordingID{"r1", "r2", "r-none"},
		[]job.Type{transcription.STTJobType}, []job.Type{coursework.SummarizeJobType, coursework.ExtractHomeworkJobType})
	if err != nil || len(unreadable) != 0 {
		t.Fatalf("ListForRecordings: unreadable %v, err %v", unreadable, err)
	}
	ids := func(rec shared.RecordingID) []string {
		var out []string
		for _, j := range got[rec] {
			out = append(out, string(j.ID()))
		}
		slices.Sort(out)
		return out
	}
	if fmt.Sprint(ids("r1")) != "[hw-t1b stt-r1 sum-t1]" || fmt.Sprint(ids("r2")) != "[stt-r2]" || len(got) != 2 {
		t.Fatalf("jobs r1 %v, r2 %v, all %v", ids("r1"), ids("r2"), got)
	}
	requireSameJob(t, got["r2"][0], mustGetJob(t, repo, "stt-r2"))
}

func TestJobListForRecordingsSkipsUnreadableRows(t *testing.T) {
	db := openTest(t, nil)
	repo := NewJobRepository(db)
	saveRecording(t, NewRecordingRepository(db), newTestRecording(t, "r1", ""))
	for _, id := range []string{"a-good", "b-scan"} {
		s := jobSpec(id, transcription.STTJobType)
		s.Payload = `{"recording_id":"r1"}`
		enqueueJob(t, repo, s)
	}
	mustExec(t, db, "UPDATE jobs SET next_run_at = 'not a number' WHERE id = 'b-scan'")

	got, unreadable, err := repo.ListForRecordings(context.Background(), []shared.RecordingID{"r1"},
		[]job.Type{transcription.STTJobType}, nil)
	if err != nil || len(got["r1"]) != 1 || got["r1"][0].ID() != "a-good" || fmt.Sprint(unreadable) != "[b-scan]" {
		t.Fatalf("ListForRecordings = %v, unreadable %v, %v", got, unreadable, err)
	}
}

func TestJobListForRecordingsFailsOnQueryError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewJobRepository(openTest(t, nil)).ListForRecordings(ctx, []shared.RecordingID{"r1"}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func mustGetJob(t *testing.T, r *JobRepository, id shared.JobID) *job.Job {
	t.Helper()
	j, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
