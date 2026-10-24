package tests

// Scenario tests of the recording deletion (TASK-095, ADR-0018) on the real stack: SQLite, the file store, the job
// runner and the post-processing of ppEnv. The lecture text job is a stand-in handler that is RUNNING when the user
// deletes the recording.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

var (
	_ recordingdelete.RecordingRepository = (*sqlite.RecordingRepository)(nil)
	_ recordingdelete.DeletionStore       = (*sqlite.RecordingDeletions)(nil)
	_ recordingdelete.JobFinder           = (*sqlite.JobRepository)(nil)
	_ recordingdelete.JobCanceller        = (*jobrunner.Service)(nil)
	_ recordingdelete.Tx                  = (*sqlite.DB)(nil)
)

type notRecovering struct{}

func (notRecovering) Recovering(shared.RecordingID) bool { return false }

// deleteClock runs in real time from a fixed start, so the wait of a deletion is real but short.
type deleteClock struct {
	base, start time.Time
	clock.System
}

func (c deleteClock) Now() time.Time { return c.base.Add(time.Since(c.start)) }

type deleteEnv struct {
	*ppEnv
	recordings *sqlite.RecordingRepository
	files      []shared.ArtifactRef // the files of r1
	chunks     shared.ArtifactRef   // the chunks directory of r1
	started    chan struct{}        // the lecture text handler runs
}

// newDeleteEnv runs the post-processing of r1 until its lecture text job, handled by lecture, is RUNNING, and adds a
// live part, a lecture text row and a chunk file. extra are more job handlers of the runner.
func newDeleteEnv(t *testing.T, lecture func(ctx context.Context) error, extra ...jobrunner.Registration) *deleteEnv {
	t.Helper()
	llm := testenv.NewFakeLLM()
	llm.Answer(values.OperationSummary, ppSummary)
	llm.Answer(values.OperationExtractHomework, ppExtract)
	e := &deleteEnv{ppEnv: newPPEnv(t, llm), started: make(chan struct{}, 1)}
	e.recordings = sqlite.NewRecordingRepository(e.db)
	h := handlerFn(func(ctx context.Context, _ job.Snapshot, _ progressFn) error {
		select {
		case e.started <- struct{}{}:
		default:
		}
		return lecture(ctx)
	})
	if err := e.runner.Register(jobrunner.Registration{
		Type: homeworkflow.LectureTextJobType, Handler: h, Recovery: jobrunner.RecoveryRequeue,
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range extra {
		if err := e.runner.Register(r); err != nil {
			t.Fatal(err)
		}
	}
	e.start()
	e.waitPost(homeworkflow.SummarizeJobType, job.StatusCompleted)
	e.waitPost(homeworkflow.ExtractHomeworkJobType, job.StatusCompleted)
	e.waitPost(homeworkflow.LectureTextJobType, job.StatusRunning)
	<-e.started

	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text) VALUES ('r1', 0, 0, 1000, 'part')",
		"INSERT INTO lecture_texts (id, transcript_id, lesson_id, created_at, updated_at) VALUES ('stt-1', 'stt-1', 'l1', 0, 0)",
	} {
		if _, err := e.db.DBTX(ctx).ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.recordings.Get(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := r.ChunksDir().Child("000001.wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Write(ctx, chunk, strings.NewReader("RIFF chunk")); err != nil {
		t.Fatal(err)
	}
	doc, err := transcript.DocumentRef(ppTranscript)
	if err != nil {
		t.Fatal(err)
	}
	e.files, e.chunks = []shared.ArtifactRef{chunk, r.Audio(), doc}, r.ChunksDir()
	for _, f := range e.files {
		if _, err := e.store.Path(ctx, f); err != nil {
			t.Fatalf("%s before the deletion: %v", f, err)
		}
	}
	return e
}

func (e *deleteEnv) service(wait time.Duration) *recordingdelete.Service {
	e.t.Helper()
	s, err := recordingdelete.New(recordingdelete.Deps{
		Recordings: e.recordings, Store: sqlite.NewRecordingDeletions(e.db), Jobs: e.jobs, Canceller: e.runner,
		Artifacts: e.store, Recovery: notRecovering{}, Clock: deleteClock{base: ppNow, start: time.Now()},
		Sleeper: clock.System{}, Tx: e.db,
	}, recordingdelete.Config{TrackExtension: "wav", Wait: wait, Poll: 10 * time.Millisecond})
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// request chooses every assignment found in r1.
func (e *deleteEnv) request(s *recordingdelete.Service) recordingdelete.Request {
	e.t.Helper()
	p, err := s.Preview(context.Background(), "r1")
	if err != nil {
		e.t.Fatal(err)
	}
	if len(p.Contents.Assignments) != 2 || p.Contents.Jobs != 1 || p.Contents.Parts != 1 || p.Contents.LectureTexts != 1 {
		e.t.Fatalf("preview contents = %+v", p.Contents)
	}
	req := recordingdelete.Request{Recording: "r1"}
	for _, a := range p.Contents.Assignments {
		req.Assignments = append(req.Assignments, a.ID)
	}
	return req
}

func (e *deleteEnv) count(table string) int {
	e.t.Helper()
	ctx := context.Background()
	var n int
	if err := e.db.DBTX(ctx).QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// checkGone verifies that no row and no file of r1 is left, and that both deadline events are to be removed.
func (e *deleteEnv) checkGone() {
	e.t.Helper()
	for _, table := range []string{
		"recordings", "transcripts", "transcript_parts", "summaries", "lecture_texts", "jobs", "assignments",
		"homework_drafts",
	} {
		if n := e.count(table); n != 0 {
			e.t.Errorf("%s: %d rows left", table, n)
		}
	}
	if n := e.count("deadline_event_removals"); n != 2 {
		e.t.Errorf("deadline event removals = %d, want 2", n)
	}
	ctx := context.Background()
	for _, f := range e.files {
		if _, err := e.store.Path(ctx, f); !errors.Is(err, shared.ErrNotFound) {
			e.t.Errorf("%s after the deletion: %v", f, err)
		}
	}
	if _, err := e.store.List(ctx, e.chunks); err == nil {
		e.t.Error("the chunks directory is left")
	}
}

// A running job that honours its context is cancelled and everything of the recording goes.
func TestDeleteRecordingWithARunningJob(t *testing.T) {
	e := newDeleteEnv(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	s := e.service(10 * time.Second)

	res, err := s.Delete(context.Background(), e.request(s))
	if err != nil || res.Outcome != recordingdelete.OutcomeDeleted || len(res.Deleted) != 2 {
		t.Fatalf("Delete = %+v, %v; want deleted with 2 assignments", res, err)
	}
	e.checkGone()
}

// A turnkey work of an assignment of the recording with its writing job RUNNING (TASK-131): the deletion cancels the
// job and the work goes with its assignment.
func TestDeleteRecordingWithARunningWorkWriting(t *testing.T) {
	writing := make(chan struct{}, 1)
	e := newDeleteEnv(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, jobrunner.Registration{
		Type: turnkey.WritingJobType, Recovery: jobrunner.RecoveryRequeue,
		Handler: handlerFn(func(ctx context.Context, _ job.Snapshot, _ progressFn) error {
			select {
			case writing <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}),
	})
	ctx := context.Background()
	a := e.storedAssignments()[0].ID()
	if _, err := e.db.DBTX(ctx).ExecContext(ctx,
		"INSERT INTO works (id, assignment_id, status, round, attempt, created_at, updated_at) VALUES ('w-del', ?, 'triage', 1, 1, 0, 0)",
		string(a)); err != nil {
		t.Fatal(err)
	}
	payload, err := values.NewWorkJob("w-del", a)
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{
		ID: "j-write", Type: turnkey.WritingJobType, Key: "work_writing:w-del:g1:a", Payload: payload.Payload(), MaxAttempts: 3,
	}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.jobs.Enqueue(ctx, j); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the writing job runs", func() bool {
		got, err := e.jobs.Get(ctx, "j-write")
		return err == nil && got.Status() == job.StatusRunning
	}, func() { e.clock.Advance(time.Second) })
	<-writing

	s := e.service(10 * time.Second)
	p, err := s.Preview(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	req := recordingdelete.Request{Recording: "r1"}
	for _, as := range p.Contents.Assignments {
		req.Assignments = append(req.Assignments, as.ID)
	}
	res, err := s.Delete(ctx, req)
	if err != nil || res.Outcome != recordingdelete.OutcomeDeleted || len(res.Deleted) != 2 {
		t.Fatalf("Delete = %+v, %v; want deleted with 2 assignments", res, err)
	}
	e.checkGone()
	if n := e.count("works"); n != 0 {
		t.Fatalf("works: %d rows left", n)
	}
}

// A job that ignores its cancellation longer than the wait leaves the deletion pending; once it ends, the next start
// finishes it.
func TestDeleteRecordingPendingThenResumed(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	e := newDeleteEnv(t, func(context.Context) error {
		<-release
		return nil
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) }) // before the runner stops
	s := e.service(200 * time.Millisecond)
	ctx := context.Background()

	res, err := s.Delete(ctx, e.request(s))
	if err != nil || res.Outcome != recordingdelete.OutcomePending {
		t.Fatalf("Delete = %+v, %v; want pending", res, err)
	}
	r, err := e.recordings.Get(ctx, "r1")
	if err != nil || !r.Deleting() || r.Status() != recording.StatusCompleted {
		t.Fatalf("recording after a pending deletion: %v", err)
	}
	for _, f := range e.files {
		if _, err := e.store.Path(ctx, f); err != nil {
			t.Fatalf("%s deleted while its job runs: %v", f, err)
		}
	}

	once.Do(func() { close(release) })
	lecture, _ := e.postJob(homeworkflow.LectureTextJobType)
	testenv.Eventually(t, "the lecture text job ends", func() bool {
		j, err := e.jobs.Get(ctx, lecture.ID())
		return err == nil && j.Status().IsTerminal()
	}, nil)

	n, err := e.service(200 * time.Millisecond).Resume(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Resume = %d, %v; want 1", n, err)
	}
	e.checkGone()
}
