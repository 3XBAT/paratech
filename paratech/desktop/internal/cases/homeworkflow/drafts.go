package homeworkflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, used in error messages.
const (
	opGenerate      = "homeworkflow: generate draft"
	opGeneration    = "homeworkflow: get draft generation"
	opEditDraft     = "homeworkflow: edit draft"
	opDraftVersions = "homeworkflow: list draft versions"
	opDraftVersion  = "homeworkflow: get draft version"
	opExportDraft   = "homeworkflow: export draft"
)

// Steps of a draft generation attempt, for the log; the others are shared with the post-processing.
const (
	stepAssignment = "load assignment"
	stepDraft      = "load draft"
	stepSummary    = "load summary"
)

// Drafts runs the homework draft scenario of PRD section 10 (AC-13): generate a draft of an assignment with the LLM,
// edit it locally, give additional instructions, regenerate, export the chosen version. It is also the handler of the
// homework_draft job (Handle satisfies jobrunner.Handler structurally).
//
// Every version is kept (domain/coursework/homeworkdraft). A generation runs as a job that starts from the latest
// version, its base. Its idempotency key is derived from the assignment, the base, the instructions and the prompts
// version, so a double click on Generate finds the job of the first click (PRD section 14 case 5); once the job added
// its version the base has moved on and the next Generate is a new job, and after a failed or cancelled job the same
// request runs again under the next round of the key. A job adds its answer only while its base is
// still the latest version: if the student edited the draft meanwhile, the answer is dropped and the job fails with
// ErrDraftChanged, so a generation never becomes the latest version over a user edit.
//
// The model answer is data (hard rule 3): it is checked by the strict draft parser and stored as a version; it never
// triggers a side effect. ExportDraft writes a version the student chose to the export file the core derives
// (proposal.ExportRef); it is a user action only, and sending goes through the action proposal pipeline.
type Drafts struct {
	llm         LLMClient
	assignments AssignmentRepository
	drafts      DraftRepository
	summaries   SummaryRepository
	lessons     LessonReader
	artifacts   ArtifactStore
	jobs        JobEnqueuer
	jobReader   JobReader
	clock       Clock
	ids         IDGenerator
	tx          Tx
	log         *slog.Logger

	modelAlias      string
	maxAttempts     int
	generateRetries int
}

// NewDrafts checks the ports, the model alias and the limits.
func NewDrafts(d DraftDeps) (*Drafts, error) {
	if d.LLM == nil || d.Assignments == nil || d.Drafts == nil || d.Summaries == nil || d.Lessons == nil ||
		d.Artifacts == nil || d.Jobs == nil || d.JobReader == nil || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every draft port is required", ErrInvalidConfig)
	}
	// The alias rules belong to the request value; a throwaway request checks them once, at wiring time.
	if _, err := values.NewLLMRequest(values.OperationHomeworkDraft, d.ModelAlias, "check", "", "check"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	if d.GenerateRetries < 0 || d.GenerateRetries > MaxGenerateRetries {
		return nil, fmt.Errorf("%w: generate retries %d, want 0-%d", ErrInvalidConfig, d.GenerateRetries, MaxGenerateRetries)
	}
	s := &Drafts{
		llm: d.LLM, assignments: d.Assignments, drafts: d.Drafts, summaries: d.Summaries, lessons: d.Lessons,
		artifacts: d.Artifacts, jobs: d.Jobs, jobReader: d.JobReader, clock: d.Clock, ids: d.IDs, tx: d.Tx, log: d.Log,
		modelAlias: d.ModelAlias, maxAttempts: d.MaxAttempts, generateRetries: d.GenerateRetries,
	}
	if s.maxAttempts == 0 {
		s.maxAttempts = DefaultPostProcessAttempts
	}
	if s.generateRetries == 0 {
		s.generateRetries = DefaultGenerateRetries
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Generate starts the generation of a new draft version of assignment a from its latest version, with the student's
// additional instructions (may be empty; PRD section 10 steps 2, 4 and 5). It returns the job; the version appears
// when the job completes. An equivalent generation whose job is still active or completed returns that job with
// Created false (PRD section 14 case 5 forbids concurrent equivalent jobs). If that job failed or was cancelled, the
// student retries: Generate enqueues the same request under the next round of the key (":r1", ":r2", ...), so
// concurrent clicks after a failure still race for one key and make one job; after DraftDeps.GenerateRetries rounds
// it returns ErrGenerateRetries. Two generations from the same base with different instructions are two jobs: the one that
// finishes second fails with ErrDraftChanged, although the student edited nothing (the base moved on).
// Errors: a missing assignment wraps shared.ErrNotFound, bad instructions values.ErrInvalidDraftRequest, a full
// draft homeworkdraft.ErrFull; nothing is created then.
func (s *Drafts) Generate(ctx context.Context, a shared.AssignmentID, instruction string) (GenerateResult, error) {
	return s.generate(ctx, a, latestBase, instruction)
}

// GenerateFrom is Generate for the version the student sees: if base is not the latest version any more (a generation
// finished or an edit was saved meanwhile), nothing is created and the error wraps homeworkdraft.ErrStaleBase, so the
// screen reloads instead of generating from a version the student has not seen.
func (s *Drafts) GenerateFrom(ctx context.Context, a shared.AssignmentID, base int, instruction string) (GenerateResult, error) {
	if base < 0 {
		return GenerateResult{}, fmt.Errorf("%s: %w: base %d", opGenerate, homeworkdraft.ErrStaleBase, base)
	}
	return s.generate(ctx, a, base, instruction)
}

// latestBase asks generate to start from the latest version, whatever it is.
const latestBase = -1

func (s *Drafts) generate(ctx context.Context, a shared.AssignmentID, base int, instruction string) (GenerateResult, error) {
	if _, err := s.assignments.Get(ctx, a); err != nil {
		return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
	}
	d, err := s.load(ctx, a)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
	}
	if base == latestBase {
		base = d.LatestNumber()
	}
	if err := d.CanAppend(base); err != nil {
		return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
	}
	req, err := values.NewDraftRequest(a, base, instruction)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
	}

	for round := 0; round <= s.generateRetries; round++ {
		j, err := job.New(job.Spec{
			ID: s.ids.NewJobID(), Type: HomeworkDraftJobType, Key: draftJobKey(req, round), Payload: req.Payload(),
			MaxAttempts: s.maxAttempts,
		}, s.clock.Now())
		if err != nil {
			return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
		}
		id, created, err := s.jobs.Enqueue(ctx, j)
		if err != nil {
			return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
		}
		if created {
			return GenerateResult{JobID: id, Created: true, Base: base}, nil
		}
		existing, err := s.jobReader.Get(ctx, id)
		if err != nil {
			return GenerateResult{}, fmt.Errorf("%s: %w", opGenerate, err)
		}
		if !endedWithoutResult(existing.Status()) {
			return GenerateResult{JobID: id, Created: false, Base: base}, nil
		}
	}
	return GenerateResult{}, fmt.Errorf("%s %s: %w", opGenerate, a, ErrGenerateRetries)
}

// endedWithoutResult reports whether a generation job is over without adding a version (FAILED or CANCELLED), so
// the same request may run again; an active job is still running and a COMPLETED one moved the base.
func endedWithoutResult(st job.Status) bool {
	return st.IsTerminal() && st != job.StatusCompleted
}

// draftFailures are the fixed errors a generation job can end with (Handle). The job keeps only the text of its last
// error, which ends with one of them: the runner may prefix it ("attempts exhausted: ...").
var draftFailures = []error{
	ErrDraftChanged, ErrDraftFull, ErrDraftAssignmentGone, ErrDraftContextRefused, ErrDraftInvalidAnswer,
	ErrDraftPayload, ErrDraftLLM, ErrDraftStorage, ErrDraftStopped,
}

// Generation returns the state of the generation job id that Generate returned, for the screen to follow (PRD section
// 10 step 2). A missing job or a job of another type wraps shared.ErrNotFound.
func (s *Drafts) Generation(ctx context.Context, id shared.JobID) (GenerationView, error) {
	j, err := s.jobReader.Get(ctx, id)
	if err != nil {
		return GenerationView{}, fmt.Errorf("%s %s: %w", opGeneration, id, err)
	}
	if j.Type() != HomeworkDraftJobType {
		return GenerationView{}, fmt.Errorf("%s %s: not a draft job: %w", opGeneration, id, shared.ErrNotFound)
	}
	v := GenerationView{JobID: j.ID(), Status: j.Status()}
	if j.Status() == job.StatusFailed {
		for _, e := range draftFailures {
			if strings.HasSuffix(j.LastError(), e.Error()) {
				v.Failure = e
				break
			}
		}
	}
	return v, nil
}

// draftJobKey is the idempotency key of a generation: equivalent requests (same assignment, base version, cleaned
// instructions and prompts version) share it. The instructions enter as a digest, so the key stays short. Round 0
// is the first try; round n > 0 is the n-th retry after a failed or cancelled job (Generate).
func draftJobKey(r values.DraftRequest, round int) string {
	sum := sha256.Sum256([]byte(r.Instruction()))
	key := fmt.Sprintf("%s:%s:b%d:v%d:%x", HomeworkDraftJobType, r.Assignment(), r.Base(), prompts.Version, sum[:8])
	if round > 0 {
		key += fmt.Sprintf(":r%d", round)
	}
	return key
}

// EditDraft stores what the student wrote as a new version (PRD section 10 step 3). e.Base is the version the student
// edited; if the draft has a newer one meanwhile (a generation finished), the edit is refused with
// homeworkdraft.ErrStaleBase and the UI reloads. Text equal to the latest version adds nothing. Invalid text wraps
// homeworkdraft.ErrInvalidContent; a missing assignment wraps shared.ErrNotFound.
func (s *Drafts) EditDraft(ctx context.Context, e DraftEdit) (DraftVersionView, error) {
	var view DraftVersionView
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.assignments.Get(ctx, e.Assignment); err != nil {
			return err
		}
		d, err := s.load(ctx, e.Assignment)
		if err != nil {
			return err
		}
		changed, err := d.Edit(e.Base, e.Title, e.Body, s.clock.Now())
		if err != nil {
			return err
		}
		if changed {
			if err := s.drafts.Save(ctx, d); err != nil {
				return err
			}
		}
		latest, _ := d.Latest() // Edit succeeded, so there is one
		view = draftVersionViewOf(d, latest)
		return nil
	})
	if err != nil {
		return DraftVersionView{}, fmt.Errorf("%s %s: %w", opEditDraft, e.Assignment, err)
	}
	return view, nil
}

// DraftVersions returns the versions of the draft of assignment a in order; none is an empty list. A missing
// assignment wraps shared.ErrNotFound.
func (s *Drafts) DraftVersions(ctx context.Context, a shared.AssignmentID) ([]DraftVersionView, error) {
	if _, err := s.assignments.Get(ctx, a); err != nil {
		return nil, fmt.Errorf("%s %s: %w", opDraftVersions, a, err)
	}
	d, err := s.load(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opDraftVersions, a, err)
	}
	revs := d.Revisions()
	out := make([]DraftVersionView, len(revs))
	for i, r := range revs {
		out[i] = draftVersionViewOf(d, r)
	}
	return out, nil
}

// DraftVersion returns version n of the draft of assignment a; a missing assignment, draft or version wraps
// shared.ErrNotFound.
func (s *Drafts) DraftVersion(ctx context.Context, a shared.AssignmentID, n int) (DraftVersionView, error) {
	d, r, err := s.revision(ctx, a, n)
	if err != nil {
		return DraftVersionView{}, fmt.Errorf("%s %s: %w", opDraftVersion, a, err)
	}
	return draftVersionViewOf(d, r), nil
}

// ExportDraft writes version n of the draft of assignment a to the export file of the assignment (PRD section 10
// step 6), atomically through the Storage Manager, replacing an earlier export. Only the student's explicit action
// calls it: the model chooses neither the version nor the path. A send that was confirmed for the earlier file content
// becomes STALE (proposal.Recheck). A missing assignment, draft or version wraps shared.ErrNotFound and nothing is
// written.
func (s *Drafts) ExportDraft(ctx context.Context, a shared.AssignmentID, n int) (DraftExport, error) {
	_, r, err := s.revision(ctx, a, n)
	if err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDraft, a, err)
	}
	ref, err := proposal.ExportRef(a)
	if err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDraft, a, err)
	}
	if err := s.artifacts.Write(ctx, ref, strings.NewReader(r.Markdown())); err != nil {
		return DraftExport{}, fmt.Errorf("%s %s: %w", opExportDraft, a, err)
	}
	return DraftExport{Version: r.Number(), File: ref}, nil
}

// revision loads version n of the stored draft of a; the draft of a deleted assignment is not served.
func (s *Drafts) revision(ctx context.Context, a shared.AssignmentID, n int) (*homeworkdraft.Draft, homeworkdraft.Revision, error) {
	if _, err := s.assignments.Get(ctx, a); err != nil {
		return nil, homeworkdraft.Revision{}, err
	}
	d, err := s.drafts.Get(ctx, a)
	if err != nil {
		return nil, homeworkdraft.Revision{}, err
	}
	r, ok := d.Revision(n)
	if !ok {
		return nil, homeworkdraft.Revision{}, fmt.Errorf("draft version %d: %w", n, shared.ErrNotFound)
	}
	return d, r, nil
}

// load returns the stored draft of a, or its empty history when there is none yet.
func (s *Drafts) load(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error) {
	d, err := s.drafts.Get(ctx, a)
	if errors.Is(err, shared.ErrNotFound) {
		return homeworkdraft.New(a)
	}
	return d, err
}

// Handle runs one attempt of a homework_draft job (jobrunner.Handler): it selects the minimal context (the lesson
// title, the assignment, the lesson summary, the base version and the instructions), calls the LLM and adds the
// checked answer as a generated version. Nothing is stored unless the whole answer passed the checks.
func (s *Drafts) Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, pr job.Progress) error) error {
	if report == nil {
		report = func(context.Context, job.Progress) error { return nil }
	}
	if snap.Type() != HomeworkDraftJobType {
		return s.permanent(snap, stepPayload, ErrDraftPayload, nil)
	}
	req, err := values.ParseDraftPayload(snap.Payload())
	if err != nil {
		return s.permanent(snap, stepPayload, ErrDraftPayload, err)
	}

	a, err := s.assignments.Get(ctx, req.Assignment())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return s.permanent(snap, stepAssignment, ErrDraftAssignmentGone, err)
	case err != nil:
		return s.retry(snap, stepAssignment, ErrDraftStorage, err)
	}
	d, err := s.load(ctx, a.ID())
	if err != nil {
		return s.retry(snap, stepDraft, ErrDraftStorage, err)
	}
	if d.ProducedBy(snap.ID()) {
		return nil // an earlier attempt added the version
	}
	// Checked before the LLM call too, so an answer that could not be added is not paid for.
	if err := d.CanAppend(req.Base()); err != nil {
		return s.refused(snap, err)
	}

	m, err := s.material(ctx, snap, a, d, req)
	if err != nil {
		return err
	}
	sel, err := contextselection.Select(values.OperationHomeworkDraft, m)
	if err != nil {
		return s.permanent(snap, stepContext, ErrDraftContextRefused, err)
	}
	llmReq, err := buildRequest(s.modelAlias, snap.ID(), sel)
	if err != nil {
		return s.permanent(snap, stepContext, ErrDraftContextRefused, err)
	}

	res, err := s.llm.Complete(ctx, llmReq)
	if err != nil {
		return s.retry(snap, stepLLM, ErrDraftLLM, err)
	}
	answer, err := prompts.ParseDraft(res.Output)
	if err != nil {
		return s.permanent(snap, stepAnswer, ErrDraftInvalidAnswer, err)
	}
	if err := s.answered(ctx, snap, report); err != nil {
		return err
	}

	return s.store(ctx, snap, a.ID(), homeworkdraft.Generation{
		Base: req.Base(), Job: snap.ID(), Title: answer.Title(), Body: answer.Body(), Instruction: req.Instruction(),
		PromptVersion: prompts.Version,
	})
}

// material gathers what the generation may send: the lesson title and summary are optional context.
func (s *Drafts) material(ctx context.Context, snap job.Snapshot, a *assignment.Assignment, d *homeworkdraft.Draft,
	req values.DraftRequest) (contextselection.Material, error) {
	details := a.Details()
	m := contextselection.Material{
		Assignment:     contextselection.Assignment{Title: details.Title(), Description: details.Description()},
		StudentRequest: req.Instruction(),
	}
	if base, ok := d.Latest(); ok {
		m.PreviousDraft = base.Body()
	}

	if a.LessonID() != "" {
		l, err := s.lessons.Get(ctx, a.LessonID())
		switch {
		case err == nil:
			m.Lesson.Title = l.Content().Title
		case !errors.Is(err, shared.ErrNotFound): // a lesson that left the schedule is no context
			return contextselection.Material{}, s.retry(snap, stepLesson, ErrDraftStorage, err)
		}
	}
	if a.TranscriptID() != "" {
		sum, err := s.summaries.Get(ctx, summary.IDOf(a.TranscriptID()))
		switch {
		case err == nil:
			m.Summary = sum.Content().Text()
		case !errors.Is(err, shared.ErrNotFound):
			return contextselection.Material{}, s.retry(snap, stepSummary, ErrDraftStorage, err)
		}
	}
	return m, nil
}

// store adds the generated version; a version conflict means another writer saved meanwhile: reload and decide again.
func (s *Drafts) store(ctx context.Context, snap job.Snapshot, a shared.AssignmentID, g homeworkdraft.Generation) error {
	var last error
	for range saveAttempts {
		d, err := s.load(ctx, a)
		if err != nil {
			return s.retry(snap, stepSave, ErrDraftStorage, err)
		}
		added, err := d.AddGenerated(g, s.clock.Now())
		switch {
		case errors.Is(err, homeworkdraft.ErrStaleBase), errors.Is(err, homeworkdraft.ErrFull):
			return s.refused(snap, err)
		case err != nil:
			return s.permanent(snap, stepSave, ErrDraftInvalidAnswer, err)
		case !added:
			return nil
		}
		// Only a stale version is worth a reload; another conflict (a missing assignment row) would repeat.
		if last = s.drafts.Save(ctx, d); !errors.Is(last, shared.ErrVersionConflict) {
			break
		}
	}
	if last != nil {
		return s.retry(snap, stepSave, ErrDraftStorage, last)
	}
	return nil
}

// refused fails the job whose answer the draft cannot take (any more).
func (s *Drafts) refused(snap job.Snapshot, err error) error {
	if errors.Is(err, homeworkdraft.ErrFull) {
		return s.permanent(snap, stepSave, ErrDraftFull, err)
	}
	s.log.Info("draft changed during the generation: the answer is not added", slog.String("job", snap.ID().String()))
	return backoff.Permanent(fail(ErrDraftChanged, err))
}

// answered reports that the answer was checked; the runner refuses it when the job was cancelled or changed meanwhile,
// and then nothing is stored.
func (s *Drafts) answered(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	pr, err := job.NewProgress(answeredPercent, "")
	if err != nil {
		return s.permanent(snap, stepProgress, ErrDraftStorage, err)
	}
	if err := report(ctx, pr); err != nil {
		s.log.Info("draft generation stopped", slog.String("job", snap.ID().String()), slog.Any("reason", err))
		return fail(ErrDraftStopped, err)
	}
	return nil
}

// permanent fails the job at once; the cause goes to the log only.
func (s *Drafts) permanent(snap job.Snapshot, step string, msg, cause error) error {
	warnStep(s.log, "draft generation step failed", snap, step, msg, cause)
	return backoff.Permanent(fail(msg, cause))
}

// retry leaves the decision to the runner: a wait for the network or the quota, a permanent failure when the cause is
// classified so, otherwise a retry with backoff.
func (s *Drafts) retry(snap job.Snapshot, step string, msg, cause error) error {
	warnStep(s.log, "draft generation step failed", snap, step, msg, cause)
	return fail(msg, cause)
}
