package homeworkflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/services/contextselection"
	"paratech/desktop/internal/domain/coursework/services/lectureparts"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// LectureTextReminders bounds the repeats with a format reminder of one step of a lecture_text job: a step makes one
// paid call for its part plus at most these (TASK-090, TASK-097).
const LectureTextReminders = 3

// MaxLectureTextCalls bounds the LLM calls of one step of a lecture_text job. The app sizes the job timeout by it.
const MaxLectureTextCalls = 1 + LectureTextReminders

// lectureText runs one step of a lecture_text job (TASK-090, TASK-097): the transcript is planned into parts
// (lectureparts.Plan) and the first part not stored yet is formatted by the LLM, checked and stored. While parts are
// left the step returns job.ErrContinueLater, so the runner puts the job at the end of the queue and the jobs that
// waited meanwhile (drafts, emails, the calendar) run before the next part. A step that stops (timeout, crash,
// network) is repeated from the same part. The LLM keys are derived from the job and the request (llmKey), so a
// repeated step is replayed by the Hub for free. The parts belong to a run named by a digest of its requests
// (lectureRequests): a new job with the same requests (a retry after a failure) keeps the parts made, while a changed
// transcript or prompt starts afresh. The step that stores the last part publishes the merged text in place of the one
// shown before (lecturetext.Publish) and completes the job. The answer is model output (hard rule 3): stored as data,
// it triggers nothing.
func (p *PostProcessor) lectureText(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	in lessonInput) error {
	sel, err := contextselection.Select(values.OperationLectureText, in.material)
	switch {
	case errors.Is(err, contextselection.ErrMissingContext):
		return p.permanent(snap, stepContext, ErrNothingToProcess, err)
	case err != nil:
		return p.permanent(snap, stepContext, ErrContextRefused, err)
	}
	segments := make([]lectureparts.Segment, len(in.material.Transcript))
	for i, s := range in.material.Transcript {
		segments[i] = lectureparts.Segment{StartMs: s.StartMs, Text: s.Text}
	}
	plan, err := lectureparts.Plan(segments)
	switch {
	case errors.Is(err, lectureparts.ErrEmpty):
		return p.permanent(snap, stepContext, ErrNothingToProcess, err)
	case err != nil:
		return p.permanent(snap, stepContext, ErrContextRefused, err)
	}
	reqs, run, err := p.lectureRequests(snap.ID(), sel, plan)
	if err != nil {
		return p.permanent(snap, stepContext, ErrContextRefused, err)
	}

	lt, err := p.lectureTextOf(ctx, snap, in)
	if err != nil {
		return err
	}
	total := len(plan)
	next := lt.NextPart(run, total)
	if err := p.partsDone(ctx, snap, report, next, total); err != nil {
		return err
	}
	if next == total {
		// Every part was stored but the text was not published: only the merge is left.
		return p.saveLecturePart(ctx, snap, in, run, total, -1, nil)
	}

	reminders := LectureTextReminders
	part := plan[next]
	blocks, err := askChecked(ctx, p, snap, reqs[next], &reminders, func(out values.UntrustedOutput) ([]lecturetext.Block, error) {
		return lectureBlocks(part, out)
	})
	if err != nil {
		return err
	}
	// The checkpoint never gets ahead of the stored parts: a step that dies in the save would leave "next+1" with
	// next parts stored, and the repeated step could not yield (job.ErrNoProgress). The unchanged checkpoint is
	// reported again first, so a job cancelled during the call stores nothing. A report that fails after the save
	// stops the step with the part stored; the next step continues after it.
	if err := p.partsDone(ctx, snap, report, next, total); err != nil {
		return err
	}
	if err := p.saveLecturePart(ctx, snap, in, run, total, next, blocks); err != nil {
		return err
	}
	if err := p.partsDone(ctx, snap, report, next+1, total); err != nil {
		return err
	}
	if next+1 < total {
		return job.ErrContinueLater
	}
	return nil
}

// lectureRequests builds the requests of all parts, keyed with llmKey, and the id of their run: a digest of the
// model alias and of every request without its key and without the lesson context. The context comes from the
// schedule, so a re-import between attempts must not drop the stored parts and repeat paid calls (ADR-0017); the
// kind still selects the instructions, which are in the digest.
func (p *PostProcessor) lectureRequests(id shared.JobID, sel contextselection.Context, plan []lectureparts.Part) (
	[]values.LLMRequest, string, error) {
	h := sha256.New()
	h.Write([]byte(p.modelAlias))
	reqs := make([]values.LLMRequest, len(plan))
	for i, part := range plan {
		items := part.Items()
		lp := prompts.LecturePart{
			Lesson: prompts.LessonOf(sel), Part: i + 1, Parts: len(plan), ContextBefore: part.Context(), Segments: make([]prompts.LectureSegment, len(items)),
		}
		for j, it := range items {
			lp.Segments[j] = prompts.LectureSegment{Index: it.Index, StartMs: it.StartMs, Text: it.Text}
		}
		draft, err := prompts.BuildLectureText(lp, p.modelAlias, pendingKey)
		if err != nil {
			return nil, "", err
		}
		bare := lp
		bare.Lesson = prompts.LessonInfo{}
		plain, err := prompts.BuildLectureText(bare, p.modelAlias, pendingKey)
		if err != nil {
			return nil, "", err
		}
		for _, s := range []string{draft.Instructions(), plain.UntrustedData()} {
			h.Write([]byte{0})
			h.Write([]byte(s))
		}
		if reqs[i], err = values.NewLLMRequest(draft.Operation(), draft.ModelAlias(), draft.Instructions(),
			draft.UntrustedData(), llmKey(id, draft)); err != nil {
			return nil, "", err
		}
	}
	return reqs, fmt.Sprintf("lt:v%d:%x", prompts.LectureTextVersion, h.Sum(nil)[:16]), nil
}

// lectureBlocks checks the answer of a part and turns it into blocks; the segment indexes the model named become start
// times inside the part (lectureparts.Part.Starts). An answer far shorter than the part (lectureparts.Part.Condensed)
// is rejected with prompts.ErrCondensedOutput, so it is asked again to keep all content.
func lectureBlocks(part lectureparts.Part, out values.UntrustedOutput) ([]lecturetext.Block, error) {
	answer, err := prompts.ParseLectureText(out)
	if err != nil {
		return nil, err
	}
	items := answer.Blocks()
	froms := make([]int, len(items))
	var paragraphs []string
	for i, b := range items {
		froms[i] = b.From()
		paragraphs = append(paragraphs, b.Paragraphs()...)
	}
	if part.Condensed(paragraphs) {
		return nil, fmt.Errorf("%w: %d paragraphs", prompts.ErrCondensedOutput, len(paragraphs))
	}
	starts := part.Starts(froms)
	blocks := make([]lecturetext.Block, len(items))
	for i, b := range items {
		if blocks[i], err = lecturetext.NewBlock(b.Heading(), starts[i], b.Paragraphs()); err != nil {
			return nil, err
		}
	}
	return blocks, nil
}

// lectureTextOf loads the lecture text of the transcript, or a new one.
func (p *PostProcessor) lectureTextOf(ctx context.Context, snap job.Snapshot, in lessonInput) (*lecturetext.LectureText, error) {
	lt, err := p.lectures.Get(ctx, lecturetext.IDOf(in.transcript))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		lt, err = lecturetext.New(lecturetext.Spec{Transcript: in.transcript, Lesson: in.lesson}, p.clock.Now())
		if err != nil {
			return nil, p.permanent(snap, stepSave, ErrStorage, err)
		}
	case err != nil:
		return nil, p.retry(snap, stepSave, ErrStorage, err)
	}
	return lt, nil
}

// saveLecturePart stores the blocks of part index of run and, once the run is complete, publishes the merged text in
// the same save. index -1 only publishes a run whose parts are all stored. A conflict reloads and decides again; a
// part another attempt stored meanwhile is not stored twice.
func (p *PostProcessor) saveLecturePart(ctx context.Context, snap job.Snapshot, in lessonInput, run string, total, index int,
	blocks []lecturetext.Block) error {
	var last error
	for range saveAttempts {
		lt, err := p.lectureTextOf(ctx, snap, in)
		if err != nil {
			return err
		}
		if index >= 0 {
			if lt.NextPart(run, total) > index {
				return nil
			}
			err := lt.AddPart(run, total, index, blocks, p.clock.Now())
			switch {
			case errors.Is(err, lecturetext.ErrPartOutOfOrder):
				// Another run was stored meanwhile: the next attempt plans again.
				return p.retry(snap, stepSave, ErrStorage, err)
			case err != nil:
				return p.permanent(snap, stepSave, ErrStorage, err)
			}
		}
		if lt.Complete() {
			if err := p.publishLectureText(snap, lt, run); err != nil {
				return err
			}
		}

		if last = p.lectures.Save(ctx, lt); last == nil || !errors.Is(last, shared.ErrConflict) {
			break
		}
	}
	if last != nil {
		return p.retry(snap, stepSave, ErrStorage, last)
	}
	return nil
}

// publishLectureText merges the parts of the complete run and makes them the text shown. A run whose parts hold no
// block at all (no speech worth keeping) has nothing to show.
func (p *PostProcessor) publishLectureText(snap job.Snapshot, lt *lecturetext.LectureText, run string) error {
	merged, err := lectureparts.Merge(lt.Parts())
	if err != nil {
		return p.permanent(snap, stepSave, ErrStorage, err)
	}
	if len(merged) == 0 {
		return p.permanent(snap, stepAnswer, ErrNothingToProcess, nil)
	}
	if err := lt.Publish(run, merged, prompts.LectureTextVersion, p.clock.Now()); err != nil {
		return p.permanent(snap, stepSave, ErrStorage, err)
	}
	return nil
}

// partsDone reports how many parts are stored, as the percentage and the checkpoint "done/total"
// (values.LectureTextCheckpoint) that the recordings screen shows. The runner refuses it when the job was cancelled
// or changed meanwhile.
func (p *PostProcessor) partsDone(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	done, total int) error {
	pr, err := job.NewProgress(done*100/total, values.LectureTextCheckpoint(done, total))
	if err != nil {
		return p.permanent(snap, stepProgress, ErrStorage, err)
	}
	if err := report(ctx, pr); err != nil {
		p.log.Info("post-processing job stopped", slog.String("job", snap.ID().String()), slog.Any("reason", err))
		return fail(ErrStopped, err)
	}
	return nil
}
