package recordingdelete

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	coursework "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

var ctx = context.Background()

// found are the assignments of the full recording "r1": one of each class plus one the user does not choose.
var found = []deletion.Assignment{
	{ID: "a-plain", Title: "Задача 1"}, {ID: "a-draft", Title: "Эссе", HasDraft: true},
	{ID: "a-sent", Title: "Отчёт", Deliveries: 1}, {ID: "a-left", Title: "Не выбрано"},
}

// fullRecording is a COMPLETED "r1" with a transcript document, a summary, a lecture text, the found assignments and
// jobs: a queued stt job, a running summary job that stops at the second cancel, a finished lecture text job and a
// running draft job of "a-draft".
func fullRecording(t *testing.T, w *world) {
	t.Helper()
	w.addRecording(t, "r1", recording.StatusCompleted)
	w.setContents("r1", deletion.Contents{Transcripts: 1, Parts: 0, Summaries: 1, LectureTexts: 1, Assignments: found},
		mustRef(t, "transcripts/j-stt.json"))
	w.addJob(t, "j-stt", transcription.STTJobType, "r1", "", job.StatusQueued, 0)
	w.addJob(t, "j-sum", coursework.SummarizeJobType, "r1", "", job.StatusRunning, 1)
	w.addJob(t, "j-lt", coursework.LectureTextJobType, "r1", "", job.StatusCompleted, 0)
	w.addJob(t, "j-draft", coursework.HomeworkDraftJobType, "", "a-draft", job.StatusRunning, 0)
}

func TestNewChecksTheConfiguration(t *testing.T) {
	w := newWorld()
	full := Deps{
		Recordings: recordingsFake{w: w, t: t}, Store: storeFake{w}, Jobs: jobsFake{w}, Canceller: cancellerFake{w},
		Artifacts: artifactsFake{w}, Recovery: guardFake{w}, Clock: clockFake{w}, Sleeper: clockFake{w}, Tx: txFake{},
	}
	cases := []struct {
		name   string
		mutate func(d *Deps, c *Config)
	}{
		{"no recordings", func(d *Deps, _ *Config) { d.Recordings = nil }},
		{"no canceller", func(d *Deps, _ *Config) { d.Canceller = nil }},
		{"no recovery guard", func(d *Deps, _ *Config) { d.Recovery = nil }},
		{"bad track extension", func(_ *Deps, c *Config) { c.TrackExtension = "w/v" }},
		{"negative wait", func(_ *Deps, c *Config) { c.Wait = -time.Second }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, c := full, Config{TrackExtension: "wav"}
			tc.mutate(&d, &c)
			if _, err := New(d, c); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := New(full, Config{TrackExtension: "wav"}); err != nil {
		t.Fatalf("New without the optional ports: %v", err)
	}
}

func TestPreview(t *testing.T) {
	w := newWorld()
	fullRecording(t, w)
	s := newService(t, w)

	got, err := s.Preview(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	want := Preview{
		Recording: "r1", StartedAt: t0.Add(-time.Hour), Refusal: RefusalNone,
		Contents: deletion.Contents{Sound: true, Transcripts: 1, Summaries: 1, LectureTexts: 1, Jobs: 2, Assignments: found},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Preview =\n %+v\nwant\n %+v", got, want)
	}
	if w.cancelCalls != 0 || len(w.deleted) != 0 {
		t.Fatal("a preview changed something")
	}
}

func TestPreviewRefusalsAndErrors(t *testing.T) {
	w := newWorld()
	w.addRecording(t, "r-live", recording.StatusRecording)
	w.addRecording(t, "r-lost", recording.StatusFailed)
	w.addRecording(t, "r-marked", recording.StatusFailed)
	w.recovering["r-lost"] = true
	s := newService(t, w)
	if _, err := s.Delete(ctx, Request{Recording: "r-marked"}); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[shared.RecordingID]Refusal{"r-live": RefusalInProgress, "r-lost": RefusalRecovering} {
		p, err := s.Preview(ctx, id)
		if err != nil || p.Refusal != want {
			t.Errorf("Preview(%s) = %q, %v; want %q", id, p.Refusal, err, want)
		}
	}
	errs := map[shared.RecordingID]error{"r-marked": ErrNotFound, "nope": ErrNotFound, "a/b": ErrInvalidRequest}
	for id, want := range errs {
		if _, err := s.Preview(ctx, id); !errors.Is(err, want) {
			t.Errorf("Preview(%s) = %v, want %v", id, err, want)
		}
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	w := newWorld()
	fullRecording(t, w)
	s := newService(t, w)

	res, err := s.Delete(ctx, Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-plain", "a-draft", "a-sent"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Recording: "r1", Outcome: OutcomeDeleted, Deleted: []shared.AssignmentID{"a-plain", "a-draft"},
		Kept: []shared.AssignmentID{"a-sent", "a-left"},
	}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("Delete =\n %+v\nwant\n %+v", res, want)
	}

	if _, ok := w.stored("r1"); ok {
		t.Fatal("the recording row is still there")
	}
	if len(w.files) != 0 {
		t.Fatalf("files left: %v", w.files)
	}
	doc, _ := transcript.DocumentRef("j-stt")
	for _, f := range []shared.ArtifactRef{mustRef(t, "recordings/r1"), mustRef(t, "recordings/r1.wav"), doc} {
		if !slices.Contains(w.deleted, f) {
			t.Errorf("%s not deleted (deleted %v)", f, w.deleted)
		}
	}
	if !reflect.DeepEqual(w.removals, []shared.AssignmentID{"a-plain", "a-draft"}) {
		t.Fatalf("deadline event removals = %v", w.removals)
	}
	purged := w.purged["r1"]
	slices.Sort(purged)
	if !reflect.DeepEqual(purged, []shared.JobID{"j-draft", "j-lt", "j-stt", "j-sum"}) {
		t.Fatalf("purged jobs = %v", purged)
	}
	if w.syncs != 1 {
		t.Fatalf("calendar syncs requested = %d, want 1", w.syncs)
	}
}

func TestDeleteWithoutAssignmentsRequestsNoSync(t *testing.T) {
	w := newWorld()
	fullRecording(t, w)
	res, err := newService(t, w).Delete(ctx, Request{Recording: "r1"})
	if err != nil || res.Outcome != OutcomeDeleted {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	if len(w.removals) != 0 || w.syncs != 0 {
		t.Fatalf("removals %v, syncs %d; want none", w.removals, w.syncs)
	}
	if w.status("j-draft") != job.StatusRunning {
		t.Fatal("the draft job of an assignment that is kept was cancelled")
	}
	if !reflect.DeepEqual(res.Kept, []shared.AssignmentID{"a-plain", "a-draft", "a-sent", "a-left"}) {
		t.Fatalf("Kept = %v", res.Kept)
	}
}

func TestDeleteRefusesWithoutChanges(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, w *world)
		req   Request
		want  error
	}{
		{"in progress", func(t *testing.T, w *world) { w.addRecording(t, "r1", recording.StatusRecording) },
			Request{Recording: "r1"}, ErrInProgress},
		{"being recovered", func(t *testing.T, w *world) {
			w.addRecording(t, "r1", recording.StatusFailed)
			w.recovering["r1"] = true
		}, Request{Recording: "r1"}, ErrRecovering},
		{"assignment of another recording", fullRecording,
			Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-plain", "elsewhere"}}, ErrInvalidRequest},
		{"assignment twice", fullRecording,
			Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-plain", "a-plain"}}, ErrInvalidRequest},
		{"missing", func(*testing.T, *world) {}, Request{Recording: "r1"}, ErrNotFound},
		{"bad id", func(*testing.T, *world) {}, Request{Recording: "a\\b"}, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld()
			tc.setup(t, w)
			if _, err := newService(t, w).Delete(ctx, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("Delete = %v, want %v", err, tc.want)
			}
			if r, ok := w.stored("r1"); ok && r.Deleting() {
				t.Fatal("a refused deletion marked the recording")
			}
			if len(w.deleted) != 0 || w.cancelCalls != 0 {
				t.Fatalf("a refused deletion deleted %v and cancelled %d jobs", w.deleted, w.cancelCalls)
			}
		})
	}
}

// A job that does not stop in time leaves the recording marked and its files in place; the next start finishes.
func TestDeleteWaitsForJobsThenResumeFinishes(t *testing.T) {
	w := newWorld()
	w.addRecording(t, "r1", recording.StatusCompleted)
	w.addJob(t, "j-stt", transcription.STTJobType, "r1", "", job.StatusRunning, -1)
	s := newService(t, w)

	res, err := s.Delete(ctx, Request{Recording: "r1"})
	if err != nil || res.Outcome != OutcomePending {
		t.Fatalf("Delete = %+v, %v; want pending", res, err)
	}
	if r, ok := w.stored("r1"); !ok || !r.Deleting() {
		t.Fatal("the recording is not left marked")
	}
	if len(w.deleted) != 0 {
		t.Fatalf("files deleted while a job runs: %v", w.deleted)
	}
	if w.now.Before(t0.Add(5 * time.Second)) {
		t.Fatalf("waited until %v, want the whole 5 s wait", w.now)
	}

	// The process stops; the job runner's recovery requeues the job, which Resume then cancels.
	w.mu.Lock()
	j := w.jobs["j-stt"]
	if err := j.Requeue(w.now); err != nil {
		t.Fatal(err)
	}
	delete(w.running, "j-stt")
	w.mu.Unlock()
	n, err := newService(t, w).Resume(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Resume = %d, %v; want 1", n, err)
	}
	if _, ok := w.stored("r1"); ok || len(w.files) != 0 {
		t.Fatalf("after Resume: row kept %v, files %v", ok, w.files)
	}
}

// A job enqueued or claimed while the deletion waits (a follow-up of a finishing job) is found and cancelled too.
func TestDeleteCancelsJobsThatAppearMeanwhile(t *testing.T) {
	w := newWorld()
	w.addRecording(t, "r1", recording.StatusCompleted)
	w.addJob(t, "j-stt", transcription.STTJobType, "r1", "", job.StatusRunning, 1)
	added := false
	w.onSleep = func() {
		if !added {
			added = true
			w.addJob(t, "j-follow", coursework.SummarizeJobType, "r1", "", job.StatusQueued, 0)
		}
	}

	res, err := newService(t, w).Delete(ctx, Request{Recording: "r1"})
	if err != nil || res.Outcome != OutcomeDeleted {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	if !added || !slices.Contains(w.purged["r1"], "j-follow") {
		t.Fatalf("the job enqueued meanwhile was not purged: %v", w.purged["r1"])
	}
}

// AC: a crash in the middle of a deletion leaves the recording marked, and the next start finishes it.
func TestCrashMidDeleteIsResumed(t *testing.T) {
	cases := []struct {
		name  string
		crash func(w *world)
		heal  func(w *world)
	}{
		{"files cannot be deleted", func(w *world) { w.failFiles = shared.ErrStorageBusy }, func(w *world) { w.failFiles = nil }},
		{"rows cannot be purged", func(w *world) { w.failPurge = shared.ErrStorageBusy }, func(w *world) { w.failPurge = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld()
			fullRecording(t, w)
			tc.crash(w)
			_, _ = newService(t, w).Delete(ctx, Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-plain"}})
			r, ok := w.stored("r1")
			if !ok || !r.Deleting() {
				t.Fatal("the recording is not left marked")
			}
			if !reflect.DeepEqual(r.DeletingAssignments(), []shared.AssignmentID{"a-plain"}) {
				t.Fatalf("stored choice = %v", r.DeletingAssignments())
			}

			tc.heal(w)
			n, err := newService(t, w).Resume(ctx) // a new process
			if err != nil || n != 1 {
				t.Fatalf("Resume = %d, %v; want 1", n, err)
			}
			if _, ok := w.stored("r1"); ok || len(w.files) != 0 {
				t.Fatalf("after Resume: row kept %v, files %v", ok, w.files)
			}
			if !reflect.DeepEqual(w.removals, []shared.AssignmentID{"a-plain"}) {
				t.Fatalf("removals = %v, want the first choice", w.removals)
			}
		})
	}
}

// A second request for a recording being deleted goes on with the first choice of assignments.
func TestDeleteAgainKeepsTheFirstChoice(t *testing.T) {
	w := newWorld()
	fullRecording(t, w)
	w.failFiles = shared.ErrStorageBusy
	s := newService(t, w)
	if res, err := s.Delete(ctx, Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-plain"}}); err != nil ||
		res.Outcome != OutcomePending {
		t.Fatalf("first Delete = %+v, %v", res, err)
	}

	w.failFiles = nil
	res, err := s.Delete(ctx, Request{Recording: "r1", Assignments: []shared.AssignmentID{"a-draft"}})
	if err != nil || res.Outcome != OutcomeDeleted || !reflect.DeepEqual(res.Deleted, []shared.AssignmentID{"a-plain"}) {
		t.Fatalf("second Delete = %+v, %v; want a-plain deleted", res, err)
	}
	if n, err := s.Resume(ctx); err != nil || n != 0 {
		t.Fatalf("Resume with nothing left = %d, %v", n, err)
	}
}

func TestDeleteMany(t *testing.T) {
	w := newWorld()
	w.addRecording(t, "r1", recording.StatusCompleted)
	w.addRecording(t, "r2", recording.StatusFailed)
	w.addRecording(t, "r-live", recording.StatusRecording)
	s := newService(t, w)

	b, err := s.DeleteMany(ctx, []Request{{Recording: "r1"}, {Recording: "r-live"}, {Recording: "r2"}, {Recording: "r1"},
		{Recording: "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Results) != 2 || b.Results[0].Recording != "r1" || b.Results[1].Recording != "r2" {
		t.Fatalf("Results = %+v", b.Results)
	}
	if len(b.Failures) != 2 || !errors.Is(b.Failures[0].Err, ErrInProgress) || !errors.Is(b.Failures[1].Err, ErrNotFound) {
		t.Fatalf("Failures = %+v", b.Failures)
	}
	if _, ok := w.stored("r-live"); !ok {
		t.Fatal("the recording in progress was deleted")
	}

	many := make([]Request, MaxBatch+1)
	for i := range many {
		many[i] = Request{Recording: shared.RecordingID("r" + strconv.Itoa(i))}
	}
	if _, err := s.DeleteMany(ctx, many); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteMany of %d = %v, want ErrInvalidRequest", len(many), err)
	}
}
