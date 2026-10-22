package homeworkflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Job types of the work on a library material (TASK-122). The app registers them like the post-processing types.
const (
	MaterialSummaryJobType  = job.Type(values.MaterialSummaryJobType)
	MaterialHomeworkJobType = job.Type(values.MaterialHomeworkJobType)
)

// stepMaterial is the step of a material job that reads the material's text, for the log.
const stepMaterial = "read material"

// materialJobType returns the job type of a work on a material; the lecture text is made only of a recording.
func materialJobType(w Work) (job.Type, bool) {
	switch w {
	case WorkSummary:
		return MaterialSummaryJobType, true
	case WorkHomework:
		return MaterialHomeworkJobType, true
	}
	return "", false
}

// isMaterialJob reports whether t is a job on a library material.
func isMaterialJob(t job.Type) bool {
	return t == MaterialSummaryJobType || t == MaterialHomeworkJobType
}

// materialJobKey is the idempotency key of the first job of type typ for material m and the prompts version.
func materialJobKey(typ job.Type, m shared.MaterialID) string {
	return fmt.Sprintf("%sv%d", values.MaterialJobKeyPrefix(string(typ), m), prompts.Version)
}

// MaterialJobsDeps are the ports of MaterialJobs; all but Log are required.
type MaterialJobsDeps struct {
	Materials MaterialLookup
	Jobs      PostJobs
	Summaries SummaryReader
	Clock     Clock
	IDs       IDGenerator
	Log       *slog.Logger

	// MaxAttempts of each job; 0 means DefaultPostProcessAttempts.
	MaxAttempts int
}

// MaterialJobs starts the summary of a library material and the homework search in it (TASK-122, ADR-0024): the
// jobs of the post-processing run on the material's text instead of a transcript (PostProcessor.Handle). Nothing is
// started on its own: the agent's make_summary and find_homework call Start.
//
// Idempotency (hard rule 7): the first job of a work has the key "<type>:<material id>:v<prompts version>"; a later
// start follows the newest job with a key derived from it (job.Rerun), as «Повторить» does for a recording, so its
// LLM keys are new and two starts after the same job create one. While a job of the work is pending or running a
// start is refused (job.ErrStillActive); the agent's journal makes one call start at most once.
type MaterialJobs struct {
	materials   MaterialLookup
	jobs        PostJobs
	summaries   SummaryReader
	clock       Clock
	ids         IDGenerator
	log         *slog.Logger
	maxAttempts int
}

// NewMaterialJobs checks the ports.
func NewMaterialJobs(d MaterialJobsDeps) (*MaterialJobs, error) {
	if d.Materials == nil || d.Jobs == nil || d.Summaries == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every material job port is required", ErrInvalidConfig)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	s := &MaterialJobs{
		materials: d.Materials, jobs: d.Jobs, summaries: d.Summaries, clock: d.Clock, ids: d.IDs, log: d.Log,
		maxAttempts: d.MaxAttempts,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.maxAttempts == 0 {
		s.maxAttempts = DefaultPostProcessAttempts
	}
	return s, nil
}

// Start starts work (WorkSummary or WorkHomework) of material id. created is false when a job of the same key (a
// concurrent start) was found instead. A summary that exists stays until the new answer passed the checks. Errors: the
// lookup's errors for a missing material, material.ErrRemoved, ErrNoMaterialText for a PDF, job.ErrStillActive while
// a job of the work is pending or running, ErrUnknownWork.
func (s *MaterialJobs) Start(ctx context.Context, id shared.MaterialID, work Work) (shared.JobID, bool, error) {
	typ, ok := materialJobType(work)
	if !ok {
		return "", false, fmt.Errorf("%w: %q", ErrUnknownWork, work)
	}
	m, err := s.materials.Material(ctx, id)
	switch {
	case err != nil:
		return "", false, err
	case !m.Active():
		return "", false, material.ErrRemoved
	case m.Type() == material.TypePDF:
		return "", false, ErrNoMaterialText
	}

	now := s.clock.Now()
	var next *job.Job
	prev, err := s.jobs.LatestByKeyPrefix(ctx, typ, values.MaterialJobKeyPrefix(string(typ), id))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		req, err := values.NewMaterialRequest(id)
		if err != nil {
			return "", false, err
		}
		next, err = job.New(job.Spec{
			ID: s.ids.NewJobID(), Type: typ, Key: materialJobKey(typ, id), Payload: req.Payload(), MaxAttempts: s.maxAttempts,
		}, now)
		if err != nil {
			return "", false, err
		}
	case err != nil:
		return "", false, err
	default:
		sum := sha256.Sum256([]byte(prev.ID()))
		if next, err = prev.Rerun(s.ids.NewJobID(), fmt.Sprintf("%s:after:%x", materialJobKey(typ, id), sum[:8]), now); err != nil {
			return "", false, err
		}
	}
	got, created, err := s.jobs.Enqueue(ctx, next)
	if err != nil {
		return "", false, err
	}
	s.log.Info("material work started", slog.String("job", got.String()), slog.String("type", string(typ)),
		slog.Bool("created", created))
	return got, created, nil
}

// SummaryReady reports whether a summary of material id is stored: making it again replaces a ready result.
func (s *MaterialJobs) SummaryReady(ctx context.Context, id shared.MaterialID) (bool, error) {
	_, err := s.summaries.Get(ctx, summary.IDOfMaterial(id))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// --- the handler side: PostProcessor on a material ---------------------------------------------------------

// loadMaterial reads the text of a material job and its lesson context. The lines of the text, with a heading per
// page, slide or fragment, stand in for the transcript segments; they carry no times. The lesson is the one the
// material is linked to when there is exactly one, else the upload time anchors relative dates.
func (p *PostProcessor) loadMaterial(ctx context.Context, snap job.Snapshot, id shared.MaterialID) (lessonInput, error) {
	if p.materials == nil || p.materialAssignments == nil {
		return lessonInput{}, p.permanent(snap, stepPayload, ErrUnknownJobType, nil)
	}
	m, text, err := p.materials.MaterialText(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved):
		return lessonInput{}, p.permanent(snap, stepMaterial, ErrMaterialNotFound, err)
	case errors.Is(err, material.ErrTextNotExtracted), errors.Is(err, material.ErrTextUnreadable),
		errors.Is(err, material.ErrDamaged):
		return lessonInput{}, p.permanent(snap, stepMaterial, ErrMaterialUnreadable, err)
	case err != nil:
		return lessonInput{}, p.retry(snap, stepMaterial, ErrStorage, err)
	case text.Empty():
		return lessonInput{}, p.permanent(snap, stepMaterial, ErrMaterialEmpty, nil)
	case text.Truncated():
		// The model never works on a silently cut text (as for a transcript above the limit).
		return lessonInput{}, p.permanent(snap, stepMaterial, ErrContextRefused, nil)
	}

	lines := text.Lines()
	segments := make([]contextselection.Segment, len(lines))
	for i, l := range lines {
		segments[i] = contextselection.Segment{Text: l}
	}
	loc := p.clock.Now().Location()
	in := lessonInput{
		doc: id,
		material: contextselection.Material{
			Lesson:     contextselection.Lesson{Start: m.CreatedAt().In(loc), Kind: values.LessonKindUnknown},
			Transcript: segments,
		},
	}
	var self shared.LessonID
	if ls := m.Lessons(); len(ls) == 1 {
		l, err := p.lessons.Get(ctx, ls[0])
		switch {
		case errors.Is(err, shared.ErrNotFound):
		case err != nil:
			return lessonInput{}, p.retry(snap, stepLesson, ErrStorage, err)
		default:
			c := l.Content()
			in.lesson, self = l.ID(), l.ID()
			in.material.Lesson = contextselection.Lesson{
				Title: c.Title, Start: c.Start.In(loc), Subject: c.Subject, Teacher: c.Teacher,
				Kind: values.LessonKindOf(c.Kind.String()),
			}
		}
	}
	return p.withNextLesson(ctx, snap, self, in)
}

// summarySpec is the id and the spec of the summary of in: of its transcript, or of its material (without a lesson:
// the summary of a material is not the summary of a lesson).
func (in lessonInput) summarySpec(c summary.Content) (summary.ID, summary.Spec) {
	if in.doc != "" {
		return summary.IDOfMaterial(in.doc), summary.Spec{Material: in.doc, PromptVersion: prompts.Version, Content: c}
	}
	return summary.IDOf(in.transcript), summary.Spec{
		Transcript: in.transcript, Lesson: in.lesson, PromptVersion: prompts.Version, Content: c,
	}
}

// origin is the origin of item index of the extraction of in.
func (in lessonInput) origin(index int) assignment.Origin {
	return assignment.Origin{Transcript: in.transcript, Material: in.doc, Index: index}
}

// assignmentBySource loads the assignment of item index of the extraction of in.
func (p *PostProcessor) assignmentBySource(ctx context.Context, in lessonInput, index int) (*assignment.Assignment, error) {
	if in.doc != "" {
		return p.materialAssignments.GetByMaterialSource(ctx, in.doc, index)
	}
	return p.assignments.GetBySource(ctx, in.transcript, index)
}

// extracted lists the assignments extracted from the source of in.
func (p *PostProcessor) extracted(ctx context.Context, in lessonInput) ([]*assignment.Assignment, error) {
	if in.doc != "" {
		return p.materialAssignments.ListByMaterial(ctx, in.doc)
	}
	return p.assignments.ListByTranscript(ctx, in.transcript)
}
