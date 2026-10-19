package homeworkflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the Rerunner errors.
const (
	opRetryWork   = "homeworkflow: retry post-processing"
	opRedoSummary = "homeworkflow: redo summary"
)

// Work is a post-processing work of a recording the user may start again (TASK-086).
type Work string

// Works.
const (
	WorkSummary     Work = "summary"     // the summarize job
	WorkHomework    Work = "homework"    // the extract_homework job
	WorkLectureText Work = "lectureText" // the lecture_text job (TASK-090)
)

// jobType returns the job type of the work.
func (w Work) jobType() (job.Type, bool) {
	switch w {
	case WorkSummary:
		return SummarizeJobType, true
	case WorkHomework:
		return ExtractHomeworkJobType, true
	case WorkLectureText:
		return LectureTextJobType, true
	}
	return "", false
}

// RerunDeps are the ports of the Rerunner; all but Log are required.
type RerunDeps struct {
	Transcripts LatestTranscripts
	Jobs        PostJobs
	Summaries   SummaryReader
	Clock       Clock
	IDs         IDGenerator
	Log         *slog.Logger

	// MaxAttempts of a lecture_text job started for a transcript that has none; 0 means DefaultPostProcessAttempts.
	MaxAttempts int
}

// Rerunner starts the post-processing of a recording's newest transcript again at the user's request (TASK-086):
// «Повторить» after a failed summary or homework search and «Сделать конспект заново» for a ready summary.
//
// The failed job is never re-armed: a rerun of the same job would send the same requests, which the Hub answers from
// its store by their idempotency keys, bad answer included. A new job is created instead (job.Rerun), so its LLM keys,
// derived from the job id (llmKey), are new. Its job key is derived from the job it follows, so two clicks on the same
// failed job create one job. The transcript is only read. A summary that exists stays until the new answer passed
// the checks and replaces it (PostProcessor.summarize).
//
// A transcript made before the lecture text existed (TASK-090) has no lecture_text job: Retry of WorkLectureText
// starts its first one, with the job key the transcript event would have used, so it is made once.
type Rerunner struct {
	transcripts LatestTranscripts
	jobs        PostJobs
	summaries   SummaryReader
	clock       Clock
	ids         IDGenerator
	log         *slog.Logger
	maxAttempts int
}

// NewRerunner checks the ports.
func NewRerunner(d RerunDeps) (*Rerunner, error) {
	if d.Transcripts == nil || d.Jobs == nil || d.Summaries == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every rerun port is required", ErrInvalidConfig)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	r := &Rerunner{
		transcripts: d.Transcripts, jobs: d.Jobs, summaries: d.Summaries, clock: d.Clock, ids: d.IDs, log: d.Log,
		maxAttempts: d.MaxAttempts,
	}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	if r.maxAttempts == 0 {
		r.maxAttempts = DefaultPostProcessAttempts
	}
	return r, nil
}

// Retry starts work of the recording's newest transcript again after its job failed or was cancelled
// («Повторить»). WorkLectureText without a job starts the first one. Errors: no transcript or no job of the work wraps
// shared.ErrNotFound, a job still pending or running job.ErrStillActive, a completed one job.ErrNotFailed, an unknown
// work ErrUnknownWork.
func (r *Rerunner) Retry(ctx context.Context, rec shared.RecordingID, work Work) (shared.JobID, error) {
	typ, ok := work.jobType()
	if !ok {
		return "", fmt.Errorf("%s %q: %w", opRetryWork, work, ErrUnknownWork)
	}
	id, err := r.rerun(ctx, rec, typ, false)
	if err != nil {
		return "", fmt.Errorf("%s %s %s: %w", opRetryWork, rec, work, err)
	}
	return id, nil
}

// RedoSummary makes the summary of the recording's newest transcript again («Сделать конспект заново»); the summary
// stays as it is until the new one passed the checks. Errors: no transcript, no summary job or no summary wraps
// shared.ErrNotFound, a summary job still pending or running job.ErrStillActive.
func (r *Rerunner) RedoSummary(ctx context.Context, rec shared.RecordingID) (shared.JobID, error) {
	id, err := r.rerun(ctx, rec, SummarizeJobType, true)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", opRedoSummary, rec, err)
	}
	return id, nil
}

func (r *Rerunner) rerun(ctx context.Context, rec shared.RecordingID, typ job.Type, redo bool) (shared.JobID, error) {
	t, err := r.transcripts.LatestByRecording(ctx, rec)
	if err != nil {
		return "", err
	}
	prev, err := r.jobs.LatestByKeyPrefix(ctx, typ, values.PostProcessJobKeyPrefix(string(typ), t.ID()))
	if errors.Is(err, shared.ErrNotFound) && typ == LectureTextJobType && !redo {
		return r.startMissing(ctx, typ, t.ID())
	}
	if err != nil {
		return "", err
	}

	id, key, now := r.ids.NewJobID(), rerunKey(typ, t.ID(), prev.ID()), r.clock.Now()
	var next *job.Job
	if redo {
		next, err = prev.Rerun(id, key, now)
	} else {
		next, err = prev.RetryFailed(id, key, now)
	}
	if err != nil {
		return "", err
	}
	if redo {
		if _, err := r.summaries.Get(ctx, summary.IDOf(t.ID())); err != nil {
			return "", err
		}
	}
	got, created, err := r.jobs.Enqueue(ctx, next)
	if err != nil {
		return "", err
	}
	r.log.Info("post-processing started again", slog.String("job", got.String()), slog.String("after", prev.ID().String()),
		slog.String("type", string(typ)), slog.Bool("created", created))
	return got, nil
}

// startMissing creates the first job of typ for transcript t, keyed like the one TranscriptCreated makes, so a repeated
// click finds it instead of creating a second one.
func (r *Rerunner) startMissing(ctx context.Context, typ job.Type, t shared.TranscriptID) (shared.JobID, error) {
	req, err := values.NewPostProcessRequest(t)
	if err != nil {
		return "", err
	}
	j, err := job.New(job.Spec{
		ID: r.ids.NewJobID(), Type: typ, Key: jobKey(typ, t), Payload: req.Payload(), MaxAttempts: r.maxAttempts,
	}, r.clock.Now())
	if err != nil {
		return "", err
	}
	got, created, err := r.jobs.Enqueue(ctx, j)
	if err != nil {
		return "", err
	}
	r.log.Info("post-processing started for an older transcript", slog.String("job", got.String()),
		slog.String("type", string(typ)), slog.Bool("created", created))
	return got, nil
}

// rerunKey is the job key of the job that follows job prev: the key of the transcript's work plus a digest of prev, so
// that it stays short and a repeated click on the same failed job finds the job of the first click.
func rerunKey(typ job.Type, t shared.TranscriptID, prev shared.JobID) string {
	sum := sha256.Sum256([]byte(prev))
	return fmt.Sprintf("%s:after:%x", jobKey(typ, t), sum[:8])
}
