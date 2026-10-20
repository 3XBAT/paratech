package homeworkflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// rerunEnv is a ppEnv whose transcript t1 (recording r1) has its post-processing jobs enqueued, with a Rerunner.
type rerunEnv struct {
	*ppEnv
	rr *Rerunner
}

func newRerunEnv(t *testing.T) *rerunEnv {
	t.Helper()
	e := newPPEnv(t)
	rr, err := NewRerunner(RerunDeps{
		Transcripts: ppTranscripts{e.ppStore}, Jobs: ppJobs{e.ppStore}, Summaries: ppSummaries{e.ppStore},
		Clock: e.clock, IDs: ppIDs{e.ppStore},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.pp.TranscriptCreated(context.Background(), e.transcripts["t1"].Created()); err != nil {
		t.Fatal(err)
	}
	return &rerunEnv{ppEnv: e, rr: rr}
}

// latest returns the newest stored job of typ.
func (e *rerunEnv) latest(typ job.Type) *job.Job {
	e.t.Helper()
	j, err := ppJobs{e.ppStore}.LatestByKeyPrefix(context.Background(), typ, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

// run claims the job, lets the PostProcessor handle it and records the outcome as the runner would.
func (e *rerunEnv) run(j *job.Job) error {
	e.t.Helper()
	now := e.clock.Now()
	if err := j.Claim(now); err != nil {
		e.t.Fatal(err)
	}
	err := e.pp.Handle(context.Background(), j.Snapshot(), nil)
	if err == nil {
		if cerr := j.Complete(now); cerr != nil {
			e.t.Fatal(cerr)
		}
		return nil
	}
	if ferr := j.Fail(now, err.Error()); ferr != nil {
		e.t.Fatal(ferr)
	}
	return err
}

func TestNewRerunnerValidates(t *testing.T) {
	e := newPPEnv(t)
	full := RerunDeps{
		Transcripts: ppTranscripts{e.ppStore}, Jobs: ppJobs{e.ppStore}, Summaries: ppSummaries{e.ppStore},
		Clock: e.clock, IDs: ppIDs{e.ppStore},
	}
	for name, mutate := range map[string]func(*RerunDeps){
		"no transcripts": func(d *RerunDeps) { d.Transcripts = nil },
		"no jobs":        func(d *RerunDeps) { d.Jobs = nil },
		"no summaries":   func(d *RerunDeps) { d.Summaries = nil },
		"no clock":       func(d *RerunDeps) { d.Clock = nil },
		"no ids":         func(d *RerunDeps) { d.IDs = nil },
		"bad attempts":   func(d *RerunDeps) { d.MaxAttempts = -1 },
	} {
		d := full
		mutate(&d)
		if _, err := NewRerunner(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: NewRerunner = %v, want ErrInvalidConfig", name, err)
		}
	}
}

// AC-7 of TASK-086: «Повторить» after a permanent failure starts a new job with new LLM keys; the transcript stays.
func TestRetryAfterPermanentFailure(t *testing.T) {
	for _, tc := range []struct {
		work   Work
		typ    job.Type
		op     values.Operation
		answer string
	}{
		{WorkSummary, SummarizeJobType, values.OperationSummary, summaryAnswer},
		{WorkHomework, ExtractHomeworkJobType, values.OperationExtractHomework, extractAnswer},
		{WorkLectureText, LectureTextJobType, values.OperationLectureText, lectureAnswer("Rivers", 0, "Rivers.")},
	} {
		t.Run(string(tc.work), func(t *testing.T) {
			e := newRerunEnv(t)
			ctx := context.Background()
			e.llm.Answer(tc.op, "not JSON")
			failed := e.latest(tc.typ)
			if err := e.run(failed); !errors.Is(err, ErrInvalidAnswer) {
				t.Fatalf("first run = %v", err)
			}
			oldKeys := map[string]bool{}
			for _, r := range e.llm.RequestsFor(tc.op) {
				oldKeys[r.IdempotencyKey()] = true
			}

			e.clock.Advance(time.Minute)
			id, err := e.rr.Retry(ctx, "r1", tc.work)
			if err != nil {
				t.Fatalf("Retry: %v", err)
			}
			// The retry is pending now: a second click is refused.
			if _, err := e.rr.Retry(ctx, "r1", tc.work); !errors.Is(err, job.ErrStillActive) {
				t.Fatalf("second click = %v, want ErrStillActive", err)
			}
			next := e.latest(tc.typ)
			if next.ID() != id || next.Status() != job.StatusQueued || next.Payload() != failed.Payload() ||
				next.Key() != rerunKey(tc.typ, "t1", failed.ID()) || !strings.HasPrefix(next.Key(), jobKey(tc.typ, "t1")+":after:") ||
				failed.Status() != job.StatusFailed {
				t.Fatalf("new job %s key %q status %s payload %s", next.ID(), next.Key(), next.Status(), next.Payload())
			}
			if n := len(e.jobs); n != 4 {
				t.Fatalf("jobs = %d, want the three of the transcript and one retry", n)
			}

			e.llm.Answer(tc.op, tc.answer)
			if err := e.run(next); err != nil {
				t.Fatalf("retried job: %v", err)
			}
			reqs := e.llm.RequestsFor(tc.op)
			if last := reqs[len(reqs)-1]; oldKeys[last.IdempotencyKey()] || last.Instructions() != prompts.Instructions(tc.op) {
				t.Fatal("the retried job reused an old LLM key or a reminder")
			}
			if e.records() == 0 && e.lecture("t1") == nil {
				t.Fatal("nothing stored after the retry")
			}
			if tr, err := (ppTranscripts{e.ppStore}).Get(ctx, "t1"); err != nil || tr.Version() != e.transcripts["t1"].Version() {
				t.Fatalf("transcript changed: %v", err)
			}
		})
	}
}

func TestRetryRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *rerunEnv)
		rec   shared.RecordingID
		work  Work
		want  error
	}{
		{name: "queued", want: job.ErrStillActive},
		{name: "running", setup: func(e *rerunEnv) { _ = e.latest(SummarizeJobType).Claim(ppT0) }, want: job.ErrStillActive},
		{name: "waiting for the network", setup: func(e *rerunEnv) {
			j := e.latest(SummarizeJobType)
			_ = j.Claim(ppT0)
			_ = j.WaitFor(job.StatusWaitingForNetwork, ppT0)
		}, want: job.ErrStillActive},
		{name: "completed", setup: func(e *rerunEnv) {
			e.llm.Answer(values.OperationSummary, summaryAnswer)
			_ = e.run(e.latest(SummarizeJobType))
		}, want: job.ErrNotFailed},
		{name: "unknown recording", rec: "r9", want: shared.ErrNotFound},
		{name: "unknown work", work: "draft", want: ErrUnknownWork},
	}
	// Two clicks that race past the status check meet at the job key of the failed job: one job.
	t.Run("concurrent clicks", func(t *testing.T) {
		e := newRerunEnv(t)
		failed := e.latest(SummarizeJobType)
		_ = failed.Cancel(ppT0)
		first, _ := failed.RetryFailed("x1", rerunKey(SummarizeJobType, "t1", failed.ID()), ppT0)
		if _, created, _ := (ppJobs{e.ppStore}).Enqueue(context.Background(), first); !created {
			t.Fatal("setup")
		}
		second, _ := failed.RetryFailed("x2", rerunKey(SummarizeJobType, "t1", failed.ID()), ppT0)
		if id, created, _ := (ppJobs{e.ppStore}).Enqueue(context.Background(), second); created || id != "x1" {
			t.Fatalf("second enqueue = %s %v, want the first job", id, created)
		}
	})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newRerunEnv(t)
			if tc.setup != nil {
				tc.setup(e)
			}
			rec, work := tc.rec, tc.work
			if rec == "" {
				rec = "r1"
			}
			if work == "" {
				work = WorkSummary
			}
			before := len(e.jobs)
			if _, err := e.rr.Retry(context.Background(), rec, work); !errors.Is(err, tc.want) {
				t.Fatalf("Retry = %v, want %v", err, tc.want)
			}
			if len(e.jobs) != before {
				t.Fatal("a refused retry created a job")
			}
		})
	}

	// A transcript without post-processing jobs has nothing to retry.
	e := newPPEnv(t)
	rr, err := NewRerunner(RerunDeps{Transcripts: ppTranscripts{e.ppStore}, Jobs: ppJobs{e.ppStore},
		Summaries: ppSummaries{e.ppStore}, Clock: e.clock, IDs: ppIDs{e.ppStore}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rr.Retry(context.Background(), "r1", WorkHomework); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Retry without jobs = %v, want ErrNotFound", err)
	}
}

// TASK-090: a transcript made before the lecture text existed has no lecture_text job; «Повторить» of the lecture
// text starts its first one, once.
func TestRetryStartsAMissingLectureText(t *testing.T) {
	e := newRerunEnv(t)
	ctx := context.Background()
	delete(e.jobs, jobKey(LectureTextJobType, "t1"))
	before := len(e.jobs)

	id, err := e.rr.Retry(ctx, "r1", WorkLectureText)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	j := e.latest(LectureTextJobType)
	if j.ID() != id || j.Key() != jobKey(LectureTextJobType, "t1") || j.Payload() != `{"transcript_id":"t1"}` ||
		j.Status() != job.StatusQueued || j.MaxAttempts() != DefaultPostProcessAttempts || len(e.jobs) != before+1 {
		t.Fatalf("job %s key %q payload %s status %s", j.ID(), j.Key(), j.Payload(), j.Status())
	}
	if _, err := e.rr.Retry(ctx, "r1", WorkLectureText); !errors.Is(err, job.ErrStillActive) || len(e.jobs) != before+1 {
		t.Fatalf("second click = %v", err)
	}

	e.llm.Answer(values.OperationLectureText, lectureAnswer("Rivers", 0, "Rivers."))
	if err := e.run(j); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rr.Retry(ctx, "r1", WorkLectureText); !errors.Is(err, job.ErrNotFailed) {
		t.Fatalf("Retry of a made lecture text = %v, want ErrNotFailed", err)
	}
	// Only the lecture text starts when missing: the other works have nothing to retry.
	delete(e.jobs, jobKey(SummarizeJobType, "t1"))
	if _, err := e.rr.Retry(ctx, "r1", WorkSummary); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Retry of a missing summary job = %v, want ErrNotFound", err)
	}
}

// AC-7 of TASK-086: «Сделать конспект заново» keeps the old summary until a new answer passes the checks.
func TestRedoSummaryKeepsTheOldOneUntilANewOnePasses(t *testing.T) {
	e := newRerunEnv(t)
	ctx := context.Background()

	if _, err := e.rr.RedoSummary(ctx, "r1"); !errors.Is(err, job.ErrStillActive) {
		t.Fatalf("RedoSummary of a queued job = %v, want ErrStillActive", err)
	}
	e.llm.Answer(values.OperationSummary, summaryAnswer)
	if err := e.run(e.latest(SummarizeJobType)); err != nil {
		t.Fatal(err)
	}
	old, _ := e.summaryOf("t1")

	// The new answer is invalid: the old summary stays.
	e.clock.Advance(time.Minute)
	if _, err := e.rr.RedoSummary(ctx, "r1"); err != nil {
		t.Fatalf("RedoSummary: %v", err)
	}
	e.llm.Answer(values.OperationSummary, "no JSON")
	if err := e.run(e.latest(SummarizeJobType)); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("redo run = %v", err)
	}
	if st, _ := e.summaryOf("t1"); st.Version != old.Version || st.Text != old.Text {
		t.Fatalf("summary changed by an invalid answer: %+v", st)
	}

	// A redo after the failed redo is allowed, and its valid answer replaces the summary.
	e.clock.Advance(time.Minute)
	if _, err := e.rr.RedoSummary(ctx, "r1"); err != nil {
		t.Fatalf("RedoSummary after a failed redo: %v", err)
	}
	e.llm.Answer(values.OperationSummary, `{"sections":[{"heading":"Lakes","text":"Rivers and lakes.","points":[]}],"key_points":["Lakes"]}`)
	if err := e.run(e.latest(SummarizeJobType)); err != nil {
		t.Fatalf("second redo: %v", err)
	}
	if st, _ := e.summaryOf("t1"); st.Text != "Lakes:\nRivers and lakes." || st.Version != old.Version+1 {
		t.Fatalf("summary = %+v, want the new one", st)
	}
	if n := len(e.llm.RequestsFor(values.OperationSummary)); n != 1+AnswerCalls+1 {
		t.Fatalf("summary calls = %d", n)
	}
}

func TestRedoSummaryNeedsASummary(t *testing.T) {
	e := newRerunEnv(t)
	j := e.latest(SummarizeJobType)
	_ = j.Cancel(ppT0)
	if _, err := e.rr.RedoSummary(context.Background(), "r1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("RedoSummary without a summary = %v, want ErrNotFound", err)
	}
	if _, err := e.rr.RedoSummary(context.Background(), "r9"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("RedoSummary of an unknown recording = %v", err)
	}
	if n := len(e.jobs); n != 3 {
		t.Fatalf("jobs = %d, want no new job", n)
	}
}
