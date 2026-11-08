package tests

// Scenario of «Отменить расшифровку» and «Расшифровать снова» (TASK-392) on the real stack: SQLite, the file store,
// the whispercli adapter with the fake whisper-cli (the test binary), the job runner, the transcription queue, the
// post-processing enqueue and the recordings screen read. Only the clock, the binary and the LLM are fake.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/transcribe"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ transcribe.JobEnqueuer      = (*sqlite.JobRepository)(nil)
	_ transcribe.TranscriptFinder = (*sqlite.TranscriptRepository)(nil)
)

// onlyModel pins one model, as the catalog does with a single installed model.
type onlyModel shared.SttModelID

func (m onlyModel) ResolveForJob(context.Context, shared.SttModelID) (shared.SttModelID, error) {
	return shared.SttModelID(m), nil
}

// sttJobIDs gives the STT job ids.
type sttJobIDs struct{ n atomic.Int64 }

func (s *sttJobIDs) NewJobID() shared.JobID { return shared.JobID(fmt.Sprintf("stt-%d", s.n.Add(1))) }

// enqueuer is the transcription queue of one run of the app; ids outlive the run, as real ids are unique.
func (e *sttEnv) enqueuer(model shared.SttModelID, ids *sttJobIDs) *transcribe.Enqueuer {
	e.t.Helper()
	enq, err := transcribe.NewEnqueuer(transcribe.EnqueueDeps{
		Recordings: e.recordings, Pending: e.recordings, Models: onlyModel(model), Jobs: e.jobs, Transcripts: e.transcripts,
		IDs: ids, Tx: e.db, Clock: e.clock, MaxAttempts: 2,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return enq
}

// chain reads the recording's row as the recordings screen shows it.
func (e *sttEnv) chain() activity.Chain {
	e.t.Helper()
	svc, err := activity.New(e.recordings, e.jobs, sqlite.NewLessonRepository(e.db), nil)
	if err != nil {
		e.t.Fatal(err)
	}
	rows, err := svc.ListRecordings(context.Background())
	if err != nil || len(rows) != 1 {
		e.t.Fatalf("ListRecordings = %v, %v", rows, err)
	}
	return rows[0].Chain
}

// postJobs returns the types of the post-processing jobs of transcript t.
func (e *sttEnv) postJobs(t shared.TranscriptID) []job.Type {
	e.t.Helper()
	var out []job.Type
	for _, typ := range []job.Type{homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType} {
		jobs, err := e.jobs.ListByKeyPrefix(context.Background(), typ, cvalues.PostProcessJobKeyPrefix(string(typ), t))
		if err != nil {
			e.t.Fatal(err)
		}
		for _, j := range jobs {
			out = append(out, j.Type())
		}
	}
	return out
}

func TestCancelTranscriptionAndStartAgain(t *testing.T) {
	e := newSTTEnvWith(t, sttOptions{events: func(e *sttEnv) transcribe.EventPublisher {
		post, err := homeworkflow.NewPostProcessor(homeworkflow.PostProcessDeps{
			LLM: testenv.NewFakeLLM(), Transcripts: e.transcripts, Recordings: e.recordings,
			Lessons: sqlite.NewLessonRepository(e.db), Artifacts: e.store, Summaries: sqlite.NewSummaryRepository(e.db),
			Assignments: sqlite.NewAssignmentRepository(e.db), LectureTexts: sqlite.NewLectureTextRepository(e.db),
			Jobs: e.jobs, Clock: e.clock, IDs: &postIDs{}, ModelAlias: "default", MaxAttempts: 3,
		})
		if err != nil {
			t.Fatal(err)
		}
		return post
	}})
	ctx := context.Background()
	m := e.installModel("m-1", "base", "1", testenv.WhisperHang)
	ids := &sttJobIDs{}
	enq := e.enqueuer(m.ID(), ids)
	if err := enq.RecordingCompleted(ctx, e.rec.ID()); err != nil {
		t.Fatal(err)
	}
	first, ok, err := enq.ActiveJob(ctx, e.rec.ID())
	if err != nil || !ok {
		t.Fatalf("ActiveJob = %v, %v", ok, err)
	}
	e.start()

	// The fake prints 10% and then hangs: the report proves the process runs.
	testenv.Eventually(t, "the sidecar reports progress", func() bool {
		j := e.job(first)
		return j.Status() == job.StatusRunning && j.Progress().Percent() > 0
	}, func() { e.clock.Advance(time.Second) })
	if c := e.chain(); c.Stage != activity.StageTranscribing || !c.CanCancel {
		t.Fatalf("running row %+v, want transcribing with the cancel offered", c)
	}

	if err := e.runner.Cancel(ctx, first); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	e.waitFor(first, job.StatusCancelled)

	// CANCELLED is saved after the engine returned, which waits for the process; its run directory is gone.
	if dirs := e.runDirs(); len(dirs) != 0 {
		t.Errorf("run directories left: %v", dirs)
	}
	if _, err := e.transcripts.Get(ctx, shared.TranscriptID(first)); err == nil {
		t.Error("a cancelled job stored a transcript")
	}
	if got := e.postJobs(shared.TranscriptID(first)); len(got) != 0 {
		t.Errorf("post-processing after a cancel: %v", got)
	}
	cancelled := activity.Chain{
		Stage: activity.StageFailed, Step: activity.StepTranscription, ErrorCode: activity.ErrorTranscriptionCancelled,
		Action: activity.ActionRetranscribe,
	}
	if c := e.chain(); c != cancelled {
		t.Fatalf("cancelled row %+v, want %+v", c, cancelled)
	}
	if err := e.runner.Cancel(ctx, first); !errors.Is(err, jobrunner.ErrNotCancellable) {
		t.Fatalf("a repeated Cancel = %v, want not cancellable", err)
	}

	// A restart: the startup sweep and a repeated completion create nothing, the row still says cancelled.
	again := e.enqueuer(m.ID(), ids)
	if n, err := again.EnqueuePending(ctx); err != nil || n != 0 {
		t.Fatalf("EnqueuePending = %d, %v; want nothing", n, err)
	}
	if err := again.RecordingCompleted(ctx, e.rec.ID()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := again.ActiveJob(ctx, e.rec.ID()); err != nil || ok {
		t.Fatalf("ActiveJob after the cancel = %v, %v", ok, err)
	}
	if c := e.chain(); c != cancelled {
		t.Fatalf("row after the restart %+v", c)
	}

	// «Расшифровать снова»: the model now recognises; the new job runs to COMPLETED and the post-processing follows.
	if err := e.store.Write(ctx, m.File(), strings.NewReader(testenv.WhisperOK)); err != nil {
		t.Fatal(err)
	}
	second, err := again.Retranscribe(ctx, e.rec.ID())
	if err != nil {
		t.Fatalf("Retranscribe: %v", err)
	}
	if j := e.job(second); j.Key() != "stt:rec-1:m-1:r2" || j.PinnedModelID() != "m-1" {
		t.Fatalf("new job key %q, model %q", j.Key(), j.PinnedModelID())
	}
	if _, err := again.Retranscribe(ctx, e.rec.ID()); !errors.Is(err, transcribe.ErrNotAllowed) {
		t.Fatalf("a repeated Retranscribe = %v, want ErrNotAllowed", err)
	}
	e.waitFor(second, job.StatusCompleted)

	tr, err := e.transcripts.Get(ctx, shared.TranscriptID(second))
	if err != nil || tr.RecordingID() != e.rec.ID() {
		t.Fatalf("transcript of the new job: %v, %v", tr, err)
	}
	if got := e.postJobs(shared.TranscriptID(second)); len(got) != 2 {
		t.Fatalf("post-processing jobs of the new transcript: %v", got)
	}
	if c := e.chain(); c.Stage != activity.StageQueued || c.Step != activity.StepSummary || c.CanCancel {
		t.Fatalf("row after the new transcription %+v, want the summary queued", c)
	}
	if _, err := again.Retranscribe(ctx, e.rec.ID()); !errors.Is(err, transcribe.ErrNotAllowed) {
		t.Fatalf("Retranscribe with a transcript = %v, want ErrNotAllowed", err)
	}
	if dirs := e.runDirs(); len(dirs) != 0 {
		t.Errorf("run directories left: %v", dirs)
	}
}
