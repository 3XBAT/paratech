package homeworkflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// Steps of a post-processing attempt, for the log.
const (
	stepPayload    = "payload"
	stepTranscript = "load transcript"
	stepDocument   = "read document"
	stepRecording  = "load recording"
	stepLesson     = "load lesson"
	stepContext    = "select context"
	stepLLM        = "llm"
	stepAnswer     = "check answer"
	stepProgress   = "progress"
	stepSave       = "save result"
)

// PostProcessor turns a stored transcript into a summary and the homework assignments with their deadlines (PRD
// sections 9 and 10). It is two things for the app:
//
//   - the consumer of the STT completion event (TranscriptCreated satisfies transcribe.EventPublisher structurally):
//     it enqueues one summarize and one extract_homework job per transcript;
//   - the handler of both job types (Handle satisfies jobrunner.Handler structurally).
//
// A job selects the minimal lesson context (contextselection), builds the request with the trusted instructions and
// the transcript as untrusted data (prompts.Build), calls the LLM Client and checks the answer with the strict parser
// of its operation. The answer is model output (hard rule 3): it is stored as data and never triggers a side effect.
// Nothing is written before the whole answer passed the checks, so an invalid answer creates no records and fails the
// job at once (permanent): the Hub returns the same stored answer for the same idempotency key, so a retry cannot help.
//
// Idempotency (hard rule 7): the job keys are derived from the transcript and the prompts version, so a repeated
// event creates no second job; the LLM idempotency key is a digest of the job and the built request (llmKey), so a
// retry of the job is not charged twice (AC-11); the summary id is the transcript id and an assignment is found
// again by its origin (transcript, item index), so a rerun updates instead of adding. An assignment the user edited is
// kept as it is (assignment.Reextract). Extracted assignments at positions a shorter answer no longer has are
// withdrawn unless the user edited them or a delivery uses them (assignment.Superseded). Saves and deletes are one
// aggregate each (ARCH-23) and repeatable.
//
// Errors: no network or no quota make the runner park the job (WAITING_FOR_NETWORK, WAITING_FOR_QUOTA, see
// backoff.WaitStatus); missing inputs and invalid answers fail it; storage errors are retried.
type PostProcessor struct {
	llm         LLMClient
	transcripts TranscriptReader
	recordings  RecordingReader
	lessons     LessonReader
	artifacts   ArtifactStore
	summaries   SummaryRepository
	assignments AssignmentRepository
	jobs        JobEnqueuer
	clock       Clock
	ids         IDGenerator
	log         *slog.Logger

	modelAlias  string
	maxAttempts int
}

// NewPostProcessor checks the ports and the model alias.
func NewPostProcessor(d PostProcessDeps) (*PostProcessor, error) {
	if d.LLM == nil || d.Transcripts == nil || d.Recordings == nil || d.Lessons == nil || d.Artifacts == nil ||
		d.Summaries == nil || d.Assignments == nil || d.Jobs == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every post-processing port is required", ErrInvalidConfig)
	}
	// The alias rules belong to the request value; a throwaway request checks them once, at wiring time.
	if _, err := values.NewLLMRequest(values.OperationSummary, d.ModelAlias, "check", "", "check"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	p := &PostProcessor{
		llm: d.LLM, transcripts: d.Transcripts, recordings: d.Recordings, lessons: d.Lessons, artifacts: d.Artifacts,
		summaries: d.Summaries, assignments: d.Assignments, jobs: d.Jobs, clock: d.Clock, ids: d.IDs, log: d.Log,
		modelAlias: d.ModelAlias, maxAttempts: d.MaxAttempts,
	}
	if p.maxAttempts == 0 {
		p.maxAttempts = DefaultPostProcessAttempts
	}
	if p.log == nil {
		p.log = slog.New(slog.DiscardHandler)
	}
	return p, nil
}

// TranscriptCreated enqueues the summarize and extract_homework jobs of a stored transcript (PRD sections 7.1 and 9).
// It is idempotent by the transcript: the job keys are "<type>:<transcript id>:v<prompts version>", so a repeated
// event, which the STT job publishes at least once, finds the existing jobs and creates nothing.
func (p *PostProcessor) TranscriptCreated(ctx context.Context, e transcript.Created) error {
	req, err := values.NewPostProcessRequest(e.Transcript)
	if err != nil {
		return fail(ErrEnqueue, err)
	}
	now := p.clock.Now()
	for _, typ := range []job.Type{SummarizeJobType, ExtractHomeworkJobType} {
		j, err := job.New(job.Spec{
			ID: p.ids.NewJobID(), Type: typ, Key: jobKey(typ, e.Transcript), Payload: req.Payload(), MaxAttempts: p.maxAttempts,
		}, now)
		if err != nil {
			return fail(ErrEnqueue, err)
		}
		if _, _, err := p.jobs.Enqueue(ctx, j); err != nil {
			return fail(ErrEnqueue, err)
		}
	}
	return nil
}

// jobKey is the idempotency key of the post-processing job of one type for one transcript and prompts version.
func jobKey(typ job.Type, t shared.TranscriptID) string {
	return fmt.Sprintf("%s:%s:v%d", typ, t, prompts.Version)
}

// pendingKey stands in for the idempotency key until the request content it is derived from is built.
const pendingKey = "pending"

// llmKey is the idempotency key of the LLM request of a job: a digest of the job id and the whole request, so a retry
// with the same content is replayed and charged once (PRD section 9.2, AC-11), while a retry whose content changed
// meanwhile (lesson renamed, clock zone changed) gets a fresh key instead of IDEMPOTENCY_CONFLICT (ADR-0005(d)).
// NUL separates the parts: the texts cannot contain it (values.NewLLMRequest).
func llmKey(id shared.JobID, r values.LLMRequest) string {
	h := sha256.New()
	for _, part := range []string{string(id), string(r.Operation()), r.ModelAlias(), r.Instructions(), r.UntrustedData()} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("pp:%s:v%d:%x", r.Operation(), prompts.Version, h.Sum(nil)[:16])
}

// buildRequest builds the request of a job from the selected context and keys it with llmKey. The draft generation
// (Drafts) uses it too.
func buildRequest(modelAlias string, id shared.JobID, sel contextselection.Context) (values.LLMRequest, error) {
	draft, err := prompts.Build(sel, modelAlias, pendingKey)
	if err != nil {
		return values.LLMRequest{}, err
	}
	return values.NewLLMRequest(draft.Operation(), draft.ModelAlias(), draft.Instructions(), draft.UntrustedData(), llmKey(id, draft))
}

// operationOf returns the LLM operation of a post-processing job type.
func operationOf(t job.Type) (values.Operation, bool) {
	switch t {
	case SummarizeJobType:
		return values.OperationSummary, true
	case ExtractHomeworkJobType:
		return values.OperationExtractHomework, true
	}
	return "", false
}

// lessonInput is what a job knows about its transcript once loaded.
type lessonInput struct {
	transcript shared.TranscriptID
	lesson     shared.LessonID // empty: an ad-hoc recording or a lesson no longer in the schedule
	material   contextselection.Material
}

// Handle runs one attempt of a summarize or extract_homework job (jobrunner.Handler).
func (p *PostProcessor) Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, pr job.Progress) error) error {
	if report == nil {
		report = func(context.Context, job.Progress) error { return nil }
	}
	op, ok := operationOf(snap.Type())
	if !ok {
		return p.permanent(snap, stepPayload, ErrUnknownJobType, nil)
	}
	req, err := values.ParsePostProcessPayload(snap.Payload())
	if err != nil {
		return p.permanent(snap, stepPayload, ErrInvalidPayload, err)
	}

	in, err := p.load(ctx, snap, req.Transcript())
	if err != nil {
		return err
	}

	sel, err := contextselection.Select(op, in.material)
	switch {
	case errors.Is(err, contextselection.ErrMissingContext):
		return p.permanent(snap, stepContext, ErrNothingToProcess, err)
	case err != nil:
		return p.permanent(snap, stepContext, ErrContextRefused, err)
	}
	llmReq, err := buildRequest(p.modelAlias, snap.ID(), sel)
	if err != nil {
		return p.permanent(snap, stepContext, ErrContextRefused, err)
	}

	res, err := p.llm.Complete(ctx, llmReq)
	if err != nil {
		return p.retry(snap, stepLLM, ErrLLM, err)
	}

	switch op {
	case values.OperationSummary:
		return p.summarize(ctx, snap, report, in, res.Output)
	default:
		return p.extract(ctx, snap, report, in, res.Output, sel.LessonStart())
	}
}

// load reads the transcript text and the lesson context of the job.
func (p *PostProcessor) load(ctx context.Context, snap job.Snapshot, id shared.TranscriptID) (lessonInput, error) {
	t, err := p.transcripts.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return lessonInput{}, p.permanent(snap, stepTranscript, ErrTranscriptNotFound, err)
	case err != nil:
		return lessonInput{}, p.retry(snap, stepTranscript, ErrStorage, err)
	}

	segments, err := p.readSegments(ctx, t.Text())
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, transcript.ErrInvalidDocument):
		return lessonInput{}, p.permanent(snap, stepDocument, ErrTranscriptUnreadable, err)
	case err != nil:
		return lessonInput{}, p.retry(snap, stepDocument, ErrStorage, err)
	}

	rec, err := p.recordings.Get(ctx, t.RecordingID())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return lessonInput{}, p.permanent(snap, stepRecording, ErrTranscriptNotFound, err)
	case err != nil:
		return lessonInput{}, p.retry(snap, stepRecording, ErrStorage, err)
	}

	// Times go to the model in the student's zone: relative deadlines ("by Monday") are resolved against it.
	loc := p.clock.Now().Location()
	in := lessonInput{
		transcript: id,
		material: contextselection.Material{
			Lesson:     contextselection.Lesson{Start: rec.StartedAt().In(loc)},
			Transcript: segments,
		},
	}
	if rec.LessonID() == "" {
		return in, nil
	}
	l, err := p.lessons.Get(ctx, rec.LessonID())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		// The lesson left the schedule: the recording start still anchors the dates.
		return in, nil
	case err != nil:
		return lessonInput{}, p.retry(snap, stepLesson, ErrStorage, err)
	}
	in.lesson = l.ID()
	in.material.Lesson = contextselection.Lesson{Title: l.Content().Title, Start: l.Content().Start.In(loc)}
	return in, nil
}

// readSegments reads and validates the transcript document.
func (p *PostProcessor) readSegments(ctx context.Context, ref shared.ArtifactRef) ([]contextselection.Segment, error) {
	rc, err := p.artifacts.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, transcript.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	doc, err := transcript.ParseDocument(data)
	if err != nil {
		return nil, err
	}
	segs := doc.Segments()
	out := make([]contextselection.Segment, len(segs))
	for i, s := range segs {
		out[i] = contextselection.Segment{StartMs: s.StartMs, EndMs: s.EndMs, Text: s.Text}
	}
	return out, nil
}

// summarize checks the summary answer and stores it.
func (p *PostProcessor) summarize(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	in lessonInput, out values.UntrustedOutput) error {
	s, err := prompts.ParseSummary(out)
	if err != nil {
		return p.permanent(snap, stepAnswer, ErrInvalidAnswer, err)
	}
	content, err := summary.NewContent(s.Text(), s.KeyPoints())
	if err != nil {
		return p.permanent(snap, stepAnswer, ErrInvalidAnswer, err)
	}
	if err := p.answered(ctx, snap, report); err != nil {
		return err
	}

	var last error
	for range saveAttempts {
		cur, err := p.summaries.Get(ctx, summary.IDOf(in.transcript))
		switch {
		case errors.Is(err, shared.ErrNotFound):
			cur, err = summary.New(summary.Spec{
				Transcript: in.transcript, Lesson: in.lesson, PromptVersion: prompts.Version, Content: content,
			}, p.clock.Now())
			if err != nil {
				return p.permanent(snap, stepSave, ErrStorage, err)
			}
		case err != nil:
			return p.retry(snap, stepSave, ErrStorage, err)
		default:
			changed, err := cur.Replace(content, prompts.Version, p.clock.Now())
			if err != nil {
				return p.permanent(snap, stepSave, ErrStorage, err)
			}
			if !changed {
				return nil
			}
		}

		// A conflict means another attempt stored it meanwhile: reload and decide again.
		if last = p.summaries.Save(ctx, cur); last == nil || !errors.Is(last, shared.ErrConflict) {
			break
		}
	}
	if last != nil {
		return p.retry(snap, stepSave, ErrStorage, last)
	}
	return nil
}

// extract checks the extraction answer and stores one assignment per item, keeping the ones the user edited.
func (p *PostProcessor) extract(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	in lessonInput, out values.UntrustedOutput, lessonStart time.Time) error {
	x, err := prompts.ParseExtraction(out, lessonStart)
	if err != nil {
		return p.permanent(snap, stepAnswer, ErrInvalidAnswer, err)
	}
	items := x.Homework()
	details := make([]assignment.Details, 0, len(items))
	for _, h := range items {
		d, err := assignment.NewDetails(h.Title(), h.Description(), h.Due())
		if err != nil {
			return p.permanent(snap, stepAnswer, ErrInvalidAnswer, err)
		}
		details = append(details, d)
	}
	if err := p.answered(ctx, snap, report); err != nil {
		return err
	}

	for i, d := range details {
		if err := p.storeAssignment(ctx, snap, in, i, d); err != nil {
			return err
		}
	}
	return p.withdrawSuperseded(ctx, snap, in, len(details))
}

// withdrawSuperseded deletes the extracted assignments of the transcript that an answer of items items left behind,
// one aggregate per delete (ARCH-23); the aggregate decides which (assignment.Superseded).
func (p *PostProcessor) withdrawSuperseded(ctx context.Context, snap job.Snapshot, in lessonInput, items int) error {
	list, err := p.assignments.ListByTranscript(ctx, in.transcript)
	if err != nil {
		return p.retry(snap, stepSave, ErrStorage, err)
	}
	for _, a := range list {
		if err := p.withdraw(ctx, snap, a, items); err != nil {
			return err
		}
	}
	return nil
}

// withdraw deletes a when it is superseded. An assignment in use (a delivery references it) is kept.
func (p *PostProcessor) withdraw(ctx context.Context, snap job.Snapshot, a *assignment.Assignment, items int) error {
	var last error
	for range saveAttempts {
		if !a.Superseded(items) {
			return nil
		}
		last = p.assignments.Delete(ctx, a)
		switch {
		case last == nil:
			return nil
		case errors.Is(last, assignment.ErrInUse):
			p.log.Info("superseded assignment kept: it is in use", slog.String("job", snap.ID().String()),
				slog.String("assignment", a.ID().String()))
			return nil
		case !errors.Is(last, shared.ErrVersionConflict):
			return p.retry(snap, stepSave, ErrStorage, last)
		}

		// Changed meanwhile, for example by a user edit: reload, the aggregate decides again.
		var err error
		a, err = p.assignments.Get(ctx, a.ID())
		switch {
		case errors.Is(err, shared.ErrNotFound):
			return nil
		case err != nil:
			return p.retry(snap, stepSave, ErrStorage, err)
		}
	}
	return p.retry(snap, stepSave, ErrStorage, last)
}

// storeAssignment creates the assignment of item index or applies the new details to the one made before.
func (p *PostProcessor) storeAssignment(ctx context.Context, snap job.Snapshot, in lessonInput, index int, d assignment.Details) error {
	var last error
	for range saveAttempts {
		a, err := p.assignments.GetBySource(ctx, in.transcript, index)
		switch {
		case errors.Is(err, shared.ErrNotFound):
			a, err = assignment.NewExtracted(assignment.Spec{
				ID: p.ids.NewAssignmentID(), Lesson: in.lesson,
				Origin:        assignment.Origin{Transcript: in.transcript, Index: index},
				PromptVersion: prompts.Version, Details: d,
			}, p.clock.Now())
			if err != nil {
				return p.permanent(snap, stepSave, ErrStorage, err)
			}
		case err != nil:
			return p.retry(snap, stepSave, ErrStorage, err)
		default:
			changed, err := a.Reextract(d, prompts.Version, p.clock.Now())
			if err != nil {
				return p.permanent(snap, stepSave, ErrStorage, err)
			}
			if !changed {
				return nil
			}
		}

		// A conflict is a user edit or another attempt meanwhile: reload, the aggregate decides again.
		if last = p.assignments.Save(ctx, a); last == nil || !errors.Is(last, shared.ErrConflict) {
			break
		}
	}
	if last != nil {
		return p.retry(snap, stepSave, ErrStorage, last)
	}
	return nil
}

// answered reports that the answer was checked; the runner refuses it when the job was cancelled or changed meanwhile,
// and then nothing is stored.
func (p *PostProcessor) answered(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	pr, err := job.NewProgress(answeredPercent, "")
	if err != nil {
		return p.permanent(snap, stepProgress, ErrStorage, err)
	}
	if err := report(ctx, pr); err != nil {
		p.log.Info("post-processing job stopped", slog.String("job", snap.ID().String()), slog.Any("reason", err))
		return fail(ErrStopped, err)
	}
	return nil
}

// permanent fails the job at once; the cause goes to the log only.
func (p *PostProcessor) permanent(snap job.Snapshot, step string, msg, cause error) error {
	p.warn(snap, step, msg, cause)
	return backoff.Permanent(fail(msg, cause))
}

// retry leaves the decision to the runner: a wait for the network or the quota, a permanent failure when the cause is
// classified so, otherwise a retry with backoff.
func (p *PostProcessor) retry(snap job.Snapshot, step string, msg, cause error) error {
	p.warn(snap, step, msg, cause)
	return fail(msg, cause)
}

func (p *PostProcessor) warn(snap job.Snapshot, step string, msg, cause error) {
	warnStep(p.log, "post-processing step failed", snap, step, msg, cause)
}

// warnStep logs a failed step of a job handler: the fixed error and, separately, its cause.
func warnStep(log *slog.Logger, what string, snap job.Snapshot, step string, msg, cause error) {
	attrs := []any{
		slog.String("job", snap.ID().String()), slog.String("type", string(snap.Type())), slog.String("step", step),
		slog.String("error", msg.Error()),
	}
	if cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	log.Warn(what, attrs...)
}
