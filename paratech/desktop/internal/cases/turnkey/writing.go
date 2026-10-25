package turnkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/services/prompts"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// WritingJobType is the job type of the writing (cvalues.WorkWritingJobType).
const WritingJobType job.Type = cvalues.WorkWritingJobType

// DefaultJobAttempts is the attempt limit of a work_writing job when WritingDeps.MaxAttempts is 0.
const DefaultJobAttempts = 5

// writingRounds bounds how often the same writing (generation) starts a new job after a failed or cancelled one.
const writingRounds = 10

// Log lines of the writing; none holds a text of the work or of the model.
const (
	opWriteStep    = "turnkey: writing step failed"
	opWriteRefused = "turnkey: writing answer refused"
	opWriteStale   = "turnkey: writing answer dropped, the step is not due any more"
	opWriteStopped = "turnkey: writing stopped"
	opWriteDone    = "turnkey: work written" //nolint:gosec // G101 false positive: a log line, not a credential
)

// StartWriting starts (or resumes) the writing of a confirmed work as work_writing jobs (TASK-131): the outline, then
// — once the student approved it (ApproveOutline) — one section per job step and the .docx filed into the subject's
// «ДЗ». A presentation (TASK-106) is written the same way with slides for sections, then its likely questions, and
// files a .pptx and the speaker text .docx. A lab report (TASK-107) is a paper with placeholders for the student's data
// and «Что доделать самому». The lesson snapshot of the title page and the subject are taken now. A job of the same writing that runs or
// finished is returned with Created false; after a failed or cancelled one a new job continues from the sections
// stored (none is asked again). An outline that waits for the student, or a step that used its attempts (the student
// regenerates first), is ErrNotWritable. Errors: ErrNotConfigured, ErrNotFound, ErrNotWritable, ErrAlreadyWritten,
// ErrWritingRetries, ErrUnavailable.
func (s *Service) StartWriting(ctx context.Context, id shared.WorkID) (WritingStarted, error) {
	if s.writing == nil {
		return WritingStarted{}, ErrNotConfigured
	}
	w, err := s.get(ctx, id)
	if err != nil {
		return WritingStarted{}, err
	}
	switch w.Status() {
	case work.StatusDone:
		return WritingStarted{View: viewOf(w)}, ErrAlreadyWritten
	case work.StatusConfirmed:
		if u, _ := w.Understanding(); u.WorkType == work.TypePresentation && s.writing.Slides == nil {
			return WritingStarted{View: viewOf(w)}, fmt.Errorf("%w: no presentation renderer", ErrNotWritable)
		}
		if w, err = s.startWriting(ctx, w); err != nil {
			return WritingStarted{}, err
		}
	case work.StatusWriting:
		switch w.Next().Kind {
		case work.StepApprove:
			return WritingStarted{View: viewOf(w)}, fmt.Errorf("%w: the outline waits for the student", ErrNotWritable)
		case work.StepBlocked:
			return WritingStarted{View: viewOf(w)}, fmt.Errorf("%w: a step used its attempts, regenerate it", ErrNotWritable)
		}
	default:
		return WritingStarted{View: viewOf(w)}, fmt.Errorf("%w: the requirements are not confirmed", ErrNotWritable)
	}
	return s.enqueueWriting(ctx, w)
}

// ApproveOutline is the student's «Утвердить план» (input/wails only, never the agent): the sections are written by
// a new job. Errors: those of EditOutline and StartWriting.
func (s *Service) ApproveOutline(ctx context.Context, id shared.WorkID) (WritingStarted, error) {
	if err := s.idle(ctx, id); err != nil {
		return WritingStarted{}, err
	}
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.ApproveOutline(s.clock.Now()) })
	if err != nil {
		return WritingStarted{}, err
	}
	return s.enqueueWriting(ctx, w)
}

// WritingGeneration returns the generation of the writing a work_writing job wrote (its key), so the Undo of an older
// write_work leaves the paper of a newer one alone. Errors: ErrNotConfigured, ErrNotFound, ErrInvalidRequest (not a
// writing job), ErrUnavailable.
func (s *Service) WritingGeneration(ctx context.Context, id shared.JobID) (int, error) {
	if s.writing == nil {
		return 0, ErrNotConfigured
	}
	j, err := s.writing.Jobs.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return 0, fmt.Errorf("%w: %w", ErrNotFound, err)
	case err != nil:
		return 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	g, ok := generationOf(j)
	if !ok {
		return 0, fmt.Errorf("%w: not a writing job", ErrInvalidRequest)
	}
	return g, nil
}

// EditOutline keeps the student's outline: unchanged sections keep their text, changed and new ones are written
// again by the next StartWriting; an outline that waits for the approval still waits. Refused while the writing job
// runs (ErrWritingRunning). Errors: ErrNotConfigured, ErrNotFound, ErrInvalidRequest, ErrWritingRunning,
// ErrUnavailable.
func (s *Service) EditOutline(ctx context.Context, id shared.WorkID, items []work.OutlineItem) (View, error) {
	if err := s.idle(ctx, id); err != nil {
		return View{}, err
	}
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.EditOutline(items, s.clock.Now()) })
	if err != nil {
		return View{}, err
	}
	return viewOf(w), nil
}

// Regenerate is «Дополнительные указания → Сгенерировать заново» for section number (0: every section) with the
// student's note, then the writing job (paid again for those sections only); it approves an outline that waits.
// Number 0 on an outline call that used its attempts asks for the outline again (it then waits for the approval).
// Errors: those of EditOutline and StartWriting.
func (s *Service) Regenerate(ctx context.Context, id shared.WorkID, number int, note string) (WritingStarted, error) {
	if err := s.idle(ctx, id); err != nil {
		return WritingStarted{}, err
	}
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.Regenerate(number, note, s.clock.Now()) })
	if err != nil {
		return WritingStarted{}, err
	}
	return s.enqueueWriting(ctx, w)
}

// ActiveWriting returns the writing job of a work that is pending or running; ok is false when there is none.
func (s *Service) ActiveWriting(ctx context.Context, id shared.WorkID) (shared.JobID, bool, error) {
	if s.writing == nil {
		return "", false, ErrNotConfigured
	}
	j, err := s.writing.Jobs.LatestByKeyPrefix(ctx, WritingJobType, writingKeyPrefix(id))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case j.Status().IsTerminal():
		return "", false, nil
	}
	return j.ID(), true, nil
}

// idle refuses a change of the outline while a writing job of the work runs.
func (s *Service) idle(ctx context.Context, id shared.WorkID) error {
	_, running, err := s.ActiveWriting(ctx, id)
	switch {
	case err != nil:
		return err
	case running:
		return ErrWritingRunning
	}
	return nil
}

// startWriting takes the snapshot and moves the work to writing; a work another call started meanwhile is returned.
func (s *Service) startWriting(ctx context.Context, w *work.Work) (*work.Work, error) {
	snap, err := s.snapshot(ctx, w)
	if err != nil {
		return nil, err
	}
	got, err := s.save(ctx, w, func(w *work.Work) error { return w.StartWriting(snap, s.clock.Now()) })
	switch {
	case err != nil:
		return nil, err
	case got.Status() != work.StatusWriting:
		return nil, fmt.Errorf("%w: the work is %s", ErrNotWritable, got.Status())
	}
	return got, nil
}

// snapshot is what the paper takes from the lesson of the work: the subject and the teacher of the assignment as it
// shows them (cases/homeworkflow), the library subject they name; for an uploaded file its subject. Local names only.
func (s *Service) snapshot(ctx context.Context, w *work.Work) (work.Snapshot, error) {
	src := w.Source()
	var snap work.Snapshot
	if src.Material != "" {
		m, _, err := s.materials.MaterialText(ctx, src.Material)
		if err != nil {
			return work.Snapshot{}, sourceError(err)
		}
		if m != nil && m.Subject() != "" {
			list, err := s.writing.Shelf.Subjects(ctx)
			if err != nil {
				return work.Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
			}
			for _, sub := range list {
				if sub.ID() == m.Subject() {
					snap.Subject, snap.SubjectName = sub.ID(), sub.Name().String()
				}
			}
		}
		return cleanSnapshot(snap), nil
	}

	a, err := s.assignments.Get(ctx, src.Assignment)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return work.Snapshot{}, fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	case err != nil:
		return work.Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	label, err := s.writing.Lessons.LessonLabel(ctx, a.ID())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return work.Snapshot{}, fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	case err != nil:
		return work.Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	snap = work.Snapshot{SubjectName: label.Subject(), Teacher: label.Teacher(), Title: a.Details().Title()}
	if name := strings.TrimSpace(snap.SubjectName); name != "" {
		matches, err := s.writing.Shelf.ResolveSubjects(ctx, []string{name})
		if err != nil {
			return work.Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if m, ok := matches[name]; ok {
			snap.Subject = m.ID
		}
	}
	return cleanSnapshot(snap), nil
}

// cleanSnapshot keeps each name on one line within work.MaxSnapshotRunes.
func cleanSnapshot(s work.Snapshot) work.Snapshot {
	clean := func(v string) string {
		v = cvalues.CleanName(v)
		if utf8.RuneCountInString(v) > work.MaxSnapshotRunes {
			v = strings.TrimSpace(string([]rune(v)[:work.MaxSnapshotRunes]))
		}
		return v
	}
	s.SubjectName, s.Teacher, s.Title = clean(s.SubjectName), clean(s.Teacher), clean(s.Title)
	return s
}

// writingKeyPrefix starts the key of every writing job of a work; a work id holds no ':'.
func writingKeyPrefix(id shared.WorkID) string { return fmt.Sprintf("%s:%s:", WritingJobType, id) }

// The key of a writing job is work_writing:<work>:g<generation>[:a][:r<round>]: ":a" marks the jobs after the
// student's approval of the outline (the job before it ends once the outline is stored).
const approvedKeyPart = "a"

// generationOf reads the generation from the key of a writing job.
func generationOf(j *job.Job) (int, bool) {
	parts := strings.Split(j.Key(), ":")
	if j.Type() != WritingJobType || len(parts) < 3 || !strings.HasPrefix(parts[2], "g") {
		return 0, false
	}
	g, err := strconv.Atoi(strings.TrimPrefix(parts[2], "g"))
	return g, err == nil && g > 0
}

// enqueueWriting creates the job of the current generation under the first round of its key whose job did not end
// without a result, or returns the live or finished job of that round.
func (s *Service) enqueueWriting(ctx context.Context, w *work.Work) (WritingStarted, error) {
	wr, _ := w.Writing()
	payload, err := cvalues.NewWorkJob(w.ID(), w.Source().Assignment)
	if err != nil {
		return WritingStarted{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	for round := range writingRounds {
		key := fmt.Sprintf("%sg%d", writingKeyPrefix(w.ID()), wr.Generation)
		if wr.Approved {
			key += ":" + approvedKeyPart
		}
		if round > 0 {
			key += fmt.Sprintf(":r%d", round)
		}
		j, err := job.New(job.Spec{
			ID: s.writing.JobIDs.NewJobID(), Type: WritingJobType, Key: key, Payload: payload.Payload(),
			MaxAttempts: s.writing.MaxAttempts,
		}, s.clock.Now())
		if err != nil {
			return WritingStarted{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		id, created, err := s.writing.Jobs.Enqueue(ctx, j)
		if err != nil {
			return WritingStarted{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if created {
			return WritingStarted{View: viewOf(w), Job: id, Created: true}, nil
		}
		existing, err := s.writing.Jobs.Get(ctx, id)
		if err != nil {
			return WritingStarted{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if st := existing.Status(); !st.IsTerminal() || st == job.StatusCompleted {
			return WritingStarted{View: viewOf(w), Job: id}, nil
		}
	}
	return WritingStarted{}, ErrWritingRetries
}

// --- the job --------------------------------------------------------------------------------------------

// Handle runs one step of a work_writing job (jobrunner.Handler): the outline, the first section without its text,
// or the file. Once the outline is stored the job ends: it waits for the student (ApproveOutline starts the next job;
// no attempt is used meanwhile). After each section the job yields (job.ErrContinueLater), so other jobs run in
// between; a repeated step asks the Hub under the same key (a free replay) and a stored section is never asked again.
// The model answer is data (hard rule 3): it is checked strictly and only rendered into the paper. A refused answer
// uses an attempt of the step (a new key); after the last one the job fails. A work gone (its assignment deleted)
// ends the job.
func (s *Service) Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, pr job.Progress) error) error {
	if report == nil {
		report = func(context.Context, job.Progress) error { return nil }
	}
	if s.writing == nil || snap.Type() != WritingJobType {
		return s.permanent(snap, ErrWritePayload, nil)
	}
	p, err := cvalues.ParseWorkJob(snap.Payload())
	if err != nil {
		return s.permanent(snap, ErrWritePayload, err)
	}
	w, err := s.works.Get(ctx, p.Work())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return s.permanent(snap, ErrWriteGone, err)
	case err != nil:
		return s.retry(snap, ErrWriteStorage, err)
	}
	switch w.Status() {
	case work.StatusDone:
		return nil // an earlier step filed the paper
	case work.StatusWriting:
	default:
		return s.permanent(snap, ErrWriteGone, fmt.Errorf("work in %s", w.Status()))
	}

	switch st := w.Next(); st.Kind {
	case work.StepOutline, work.StepSection, work.StepQuestions:
		return s.writeStep(ctx, snap, report, w, st)
	case work.StepAssemble:
		if w.Presentation() {
			return s.assembleDeck(ctx, snap, report, w)
		}
		return s.assemble(ctx, snap, report, w)
	case work.StepApprove:
		return nil // the outline waits for the student
	default:
		return s.permanent(snap, ErrWriteBlocked, nil)
	}
}

// writeStep asks the model for the outline, a section (a slide) or the likely questions of a presentation, checks the
// answer and stores it.
func (s *Service) writeStep(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	w *work.Work, st work.Step) error {
	texts, err := s.writingTexts(ctx, w)
	if err != nil {
		return err
	}
	data, note, err := w.StepData(st, texts)
	if err != nil {
		return s.permanent(snap, ErrWriteGone, err)
	}
	step := promptOf(w, st)
	key := w.StepKey(st, prompts.WorkInstructions(step), data+"\x00"+note)
	req, err := prompts.BuildWork(step, data, note, s.writing.ModelAlias, key)
	if err != nil {
		return s.permanent(snap, ErrWriteGone, err)
	}
	res, err := s.writing.Writer.Complete(ctx, req)
	if err != nil {
		return s.retry(snap, ErrWriteLLM, err)
	}

	change, perr := s.parseStep(w, st, res.Output.Unvalidated(), texts)
	if perr != nil {
		return s.refusedStep(ctx, snap, w, st, perr)
	}

	// The unchanged progress first: a job cancelled during the call stores nothing.
	if err := s.progress(ctx, snap, report, w); err != nil {
		return err
	}
	got, err := s.saveStep(ctx, w, change)
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	if err := s.progress(ctx, snap, report, got); err != nil {
		return err
	}
	if got.Next().Kind == work.StepApprove {
		return nil // the outline waits for the student
	}
	return job.ErrContinueLater
}

// promptOf is the prompt of step st of w: a paper's outline or section, a presentation's slides, slide or questions,
// a lab report's outline or section.
func promptOf(w *work.Work, st work.Step) prompts.WorkStep {
	deck, lab, outline := w.Presentation(), w.LabReport(), st.Kind == work.StepOutline
	switch {
	case st.Kind == work.StepQuestions:
		return prompts.WorkQuestions
	case outline && deck:
		return prompts.WorkSlideOutline
	case outline && lab:
		return prompts.WorkLabOutline
	case outline:
		return prompts.WorkOutline
	case deck:
		return prompts.WorkSlide
	case lab:
		return prompts.WorkLabSection
	}
	return prompts.WorkSection
}

// parseStep checks the untrusted answer of step st strictly and returns the move that stores it. The outline of a
// lab report keeps what became of the manual of texts.
func (s *Service) parseStep(w *work.Work, st work.Step, raw string, texts work.Texts) (func(*work.Work) error, error) {
	switch {
	case st.Kind == work.StepOutline:
		parse := work.ParseOutline
		switch {
		case w.Presentation():
			parse = work.ParseSlideOutline
		case w.LabReport():
			parse = work.ParseLabOutline
		}
		o, err := parse(raw)
		if w.LabReport() {
			o.Manual = texts.Manual
		}
		return func(w *work.Work) error { return w.SetOutline(o, s.clock.Now()) }, err
	case st.Kind == work.StepQuestions:
		qs, err := work.ParseQuestions(raw)
		return func(w *work.Work) error { return w.SetQuestions(st.Revision, qs, s.clock.Now()) }, err
	case w.Presentation():
		sec, _ := w.Section(st.Section)
		t, err := work.ParseSlide(raw, sec.Kind)
		return func(w *work.Work) error { return w.WriteSection(st.Section, st.Revision, t, s.clock.Now()) }, err
	case w.LabReport():
		sec, _ := w.Section(st.Section)
		t, err := work.ParseLabSection(raw, texts.Materials, sec.Part)
		return func(w *work.Work) error { return w.WriteSection(st.Section, st.Revision, t, s.clock.Now()) }, err
	}
	t, err := work.ParseSection(raw, texts.Materials)
	return func(w *work.Work) error { return w.WriteSection(st.Section, st.Revision, t, s.clock.Now()) }, err
}

// refusedStep records a refused answer of step st (an attempt used) and leaves the retry to the runner.
func (s *Service) refusedStep(ctx context.Context, snap job.Snapshot, w *work.Work, st work.Step, cause error) error {
	s.log.WarnContext(ctx, opWriteRefused, slog.String("work", string(w.ID())), slog.String("step", string(st.Kind)),
		slog.Int("section", st.Section), slog.Int("attempt", st.Attempt))
	got, err := s.saveStep(ctx, w, func(w *work.Work) error { return w.RefuseStep(st, s.clock.Now()) })
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	if got.Next().Kind == work.StepBlocked {
		return s.permanent(snap, ErrWriteBlocked, cause)
	}
	return s.retry(snap, ErrWriteAnswer, cause)
}

// assemble builds the paper of the written sections, files it into the subject's «ДЗ» and marks the work done. The
// same sections give the same bytes, so a repeated step finds the material it filed.
func (s *Service) assemble(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	w *work.Work) error {
	wr, _ := w.Writing()
	student, err := s.student(ctx)
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	brief, _ := w.Brief()
	in := homeworkdoc.PaperInput{
		Student: student, Subject: wr.Snapshot.SubjectName, Teacher: wr.Snapshot.Teacher, Kind: work.TypeName(brief.WorkType),
		Topic: wr.Topic, Year: s.clock.Now().Year(),
	}
	for _, sec := range wr.Sections {
		in.Sections = append(in.Sections, homeworkdoc.PaperSection{Title: sec.Title, Paragraphs: sec.Text.Paragraphs})
		for _, r := range sec.Text.Sources {
			in.Sources = append(in.Sources, homeworkdoc.PaperSource{Text: r.Text, Verify: r.Material == ""})
		}
	}
	var doc cvalues.Document
	if w.LabReport() {
		doc = homeworkdoc.BuildLabReport(labInput(w, in))
	} else {
		doc = homeworkdoc.BuildPaper(in)
	}
	if err := doc.Validate(); err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	content, err := s.writing.Renderer.Render(ctx, doc)
	if err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	name := homeworkdoc.PaperFileName(work.TypeName(brief.WorkType), wr.Topic)
	return s.fileWriting(ctx, snap, report, w, []output{{name, content}}, func(w *work.Work, f []work.File) error {
		return w.Finish(f[0], s.clock.Now())
	})
}

// labInput is the lab report of the written sections of w: the placeholders the domain recognised, the conclusions
// and the note about a manual that was not read; the rest of the paper is paper.
func labInput(w *work.Work, paper homeworkdoc.PaperInput) homeworkdoc.LabInput {
	wr, _ := w.Writing()
	in := homeworkdoc.LabInput{Paper: paper}
	switch w.Manual() {
	case work.ManualNotRead:
		in.Manual = homeworkdoc.ManualNotRead
	case work.ManualNoText:
		in.Manual = homeworkdoc.ManualNoText
	}
	for _, sec := range wr.Sections {
		ls := homeworkdoc.LabSection{Title: sec.Title, Conclusions: sec.Part == work.LabConclusions}
		for _, p := range sec.Text.Paragraphs {
			if prompt, ok := work.PromptOf(p); ok {
				ls.Paragraphs = append(ls.Paragraphs, homeworkdoc.LabParagraph{Text: prompt, Placeholder: true})
			} else {
				ls.Paragraphs = append(ls.Paragraphs, homeworkdoc.LabParagraph{Text: p})
			}
		}
		in.Sections = append(in.Sections, ls)
	}
	return in
}

// assembleDeck builds the .pptx of the written slides and the .docx of the speaker text with the likely questions,
// files both into the subject's «ДЗ» and marks the work done, as assemble does for a paper.
func (s *Service) assembleDeck(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	w *work.Work) error {
	if s.writing.Slides == nil {
		return s.permanent(snap, ErrWriteFile, errors.New("no presentation renderer"))
	}
	wr, _ := w.Writing()
	student, err := s.student(ctx)
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	in := homeworkdoc.TalkInput{
		Student: student, Subject: wr.Snapshot.SubjectName, Teacher: wr.Snapshot.Teacher, Topic: wr.Topic,
		Year: s.clock.Now().Year(), Template: wr.Deck.Template,
	}
	for _, sec := range wr.Sections {
		sl, _ := sec.Slide()
		in.Slides = append(in.Slides, sl)
	}
	for _, q := range w.TalkQuestions() {
		in.Questions = append(in.Questions, homeworkdoc.TalkQuestion{Question: q.Question, Answer: q.Answer})
	}
	deck := homeworkdoc.BuildPresentation(in)
	if err := deck.Validate(); err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	slides, err := s.writing.Slides.Render(ctx, deck)
	if err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	talk := homeworkdoc.BuildTalk(in)
	if err := talk.Validate(); err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	text, err := s.writing.Renderer.Render(ctx, talk)
	if err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	outs := []output{{homeworkdoc.PresentationFileName(wr.Topic), slides}, {homeworkdoc.TalkFileName(wr.Topic), text}}
	return s.fileWriting(ctx, snap, report, w, outs, func(w *work.Work, f []work.File) error {
		return w.FinishDeck(f[0], f[1], s.clock.Now())
	})
}

// output is a file the writing files into «ДЗ».
type output struct {
	name string
	data []byte
}

// fileWriting files outs into the subject's «ДЗ» and keeps them with finish (Finish or FinishDeck). The files of the
// previous generation leave «ДЗ» first: a crash after it repeats the step, never loses the reference to them. The same
// bytes filed again put that material back into «ДЗ», so a repeated step finds the materials it filed.
func (s *Service) fileWriting(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	w *work.Work, outs []output, finish func(*work.Work, []work.File) error) error {
	if err := s.progress(ctx, snap, report, w); err != nil {
		return err
	}
	wr, _ := w.Writing()
	for _, prev := range wr.Files() {
		if err := s.unfile(ctx, prev.Material); err != nil {
			return s.retry(snap, ErrWriteStorage, err)
		}
	}
	files := make([]work.File, 0, len(outs))
	for _, o := range outs {
		m, err := s.writing.Shelf.FileHomework(ctx, wr.Snapshot.Subject, o.name, o.data)
		if err != nil {
			return s.retry(snap, ErrWriteStorage, err)
		}
		ref, err := material.BlobRef(m.Digest())
		if err != nil {
			return s.permanent(snap, ErrWriteStorage, err)
		}
		files = append(files, work.File{Material: m.ID(), Artifact: ref.String(), Content: w.ContentKey()})
	}
	got, err := s.saveStep(ctx, w, func(w *work.Work) error { return finish(w, files) })
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	if got.Status() != work.StatusDone {
		// The outline changed while the files were built: the files just filed are no files of the work; the next
		// step writes what changed.
		cur, _ := got.Writing()
		for _, f := range files {
			if !slices.ContainsFunc(cur.Files(), func(c work.File) bool { return c.Material == f.Material }) {
				if err := s.unfile(ctx, f.Material); err != nil {
					return s.retry(snap, ErrWriteStorage, err)
				}
			}
		}
		return job.ErrContinueLater
	}
	s.log.InfoContext(ctx, opWriteDone, slog.String("work", string(w.ID())), slog.String("material", string(files[0].Material)))
	return nil
}

// student is the profile of the title page.
func (s *Service) student(ctx context.Context) (homeworkdoc.Student, error) {
	prefs, err := s.writing.Profiles.Load(ctx)
	if err != nil {
		return homeworkdoc.Student{}, err
	}
	p := prefs.Profile().Fields()
	return homeworkdoc.Student{FullName: p.FullName, Group: p.Group, University: p.University, Faculty: p.Faculty, City: p.City}, nil
}

// writingTexts are the texts of a writing call: the assignment, the teacher's material and the student's materials
// that may be cited (the assignment's material, or the uploaded file that is the assignment). They are part of the
// key of the call, so a local failure to read them is retried (ErrWriteStorage), never taken for «no material»; only
// a material that is gone, removed or has no readable text is left out. A lab report started from a material has
// its manual in Assignment, read; an uploaded file without text leaves nothing to write from, so it is gone.
func (s *Service) writingTexts(ctx context.Context, w *work.Work) (work.Texts, error) {
	src := w.Source()
	if src.Material != "" {
		m, text, err := s.materials.MaterialText(ctx, src.Material)
		switch {
		case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved), noMaterialText(err):
			return work.Texts{}, backoff.Permanent(fail(ErrWriteGone, err))
		case err != nil:
			return work.Texts{}, fail(ErrWriteStorage, err)
		case text.Empty():
			return work.Texts{}, backoff.Permanent(fail(ErrWriteGone, ErrNoText))
		}
		t := work.Texts{Assignment: text.String(), Materials: citable(m)}
		if w.LabReport() {
			t.Manual = work.ManualRead
		}
		return t, nil
	}

	a, err := s.assignments.Get(ctx, src.Assignment)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return work.Texts{}, backoff.Permanent(fail(ErrWriteGone, err))
	case err != nil:
		return work.Texts{}, fail(ErrWriteStorage, err)
	}
	d := a.Details()
	t := work.Texts{Assignment: strings.TrimSpace(d.Title() + "\n\n" + d.Description()), Manual: work.ManualNone}
	if mid := a.MaterialID(); mid != "" {
		m, gone, err := s.manualText(ctx, mid)
		switch {
		case gone:
			// The assignment is written from alone, as the reader read it.
		case err != nil:
			return work.Texts{}, fail(ErrWriteStorage, err)
		default:
			t.Material, t.Materials, t.Manual = m.Material, m.Materials, m.Manual
		}
	}
	return t, nil
}

// manualText reads material id as a manual: its text in Material with the file to cite (ManualRead), or what became
// of it without text, nothing to cite (ManualNotRead for a PDF, ADR-0027; ManualNoText). gone reports a material gone
// or removed; err alone is a local failure to retry.
func (s *Service) manualText(ctx context.Context, id shared.MaterialID) (t work.Texts, gone bool, err error) {
	m, text, err := s.materials.MaterialText(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved):
		return work.Texts{}, true, err
	case errors.Is(err, material.ErrTextNotExtracted):
		return work.Texts{Manual: work.ManualNotRead}, false, nil
	case noMaterialText(err):
		return work.Texts{Manual: work.ManualNoText}, false, nil
	case err != nil:
		return work.Texts{}, false, err
	case text.Empty():
		return work.Texts{Manual: work.ManualNoText}, false, nil
	}
	return work.Texts{Material: text.String(), Materials: citable(m), Manual: work.ManualRead}, false, nil
}

// noMaterialText reports an error of a material whose text cannot be read at all (a scan, a damaged file): it stays
// so, a retry would not help.
func noMaterialText(err error) bool {
	return errors.Is(err, material.ErrTextNotExtracted) || errors.Is(err, material.ErrTextUnreadable) ||
		errors.Is(err, material.ErrDamaged)
}

// citable is the material the writer may cite; none without a name.
func citable(m *material.Material) []work.MaterialRef {
	if m == nil || m.Name() == "" {
		return nil
	}
	return []work.MaterialRef{{ID: m.ID(), Name: m.Name()}}
}

// unfile takes a paper of the work back from «ДЗ» to «Входящие» (the file kept), as the Undo of write_work does. A
// paper gone, removed, or moved by the student elsewhere is left as it is.
func (s *Service) unfile(ctx context.Context, id shared.MaterialID) error {
	m, _, err := s.materials.MaterialText(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil
	case m == nil && err != nil:
		return err
	case m == nil, !m.Active(), m.InInbox(), m.Folder() != subject.SystemFolderID(m.Subject(), subject.SystemHomework):
		return nil
	}
	_, err = s.writing.Shelf.MoveToInbox(ctx, id)
	return err
}

// saveStep applies fn to w and saves it, again on the stored work after a lost save. A step that is not due any more
// (the student changed the outline meanwhile) is dropped: the stored work is returned as it is.
func (s *Service) saveStep(ctx context.Context, w *work.Work, fn func(*work.Work) error) (*work.Work, error) {
	for range saveAttempts {
		if err := fn(w); err != nil {
			if errors.Is(err, work.ErrStaleStep) || errors.Is(err, work.ErrInvalidTransition) {
				s.log.InfoContext(ctx, opWriteStale, slog.String("work", string(w.ID())))
				return w, nil
			}
			return nil, err
		}
		err := s.works.Save(ctx, w)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, err
		}
		if w, err = s.works.Get(ctx, w.ID()); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: the work keeps changing", ErrUnavailable)
}

// progress reports how many sections are written as the checkpoint «N/M» (cvalues.WorkCheckpoint); the runner
// refuses it when the job was cancelled or changed meanwhile.
func (s *Service) progress(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	w *work.Work) error {
	done, total := w.Progress()
	percent, checkpoint := 0, ""
	if total > 0 {
		percent, checkpoint = done*100/(total+1), cvalues.WorkCheckpoint(done, total) // the file is one more step
	}
	pr, err := job.NewProgress(percent, checkpoint)
	if err != nil {
		return s.permanent(snap, ErrWriteStorage, err)
	}
	if err := report(ctx, pr); err != nil {
		s.log.InfoContext(ctx, opWriteStopped, slog.String("job", snap.ID().String()), slog.Any("reason", err))
		return fail(ErrWriteStopped, err)
	}
	return nil
}

// permanent fails the job at once; the cause goes to the log only.
func (s *Service) permanent(snap job.Snapshot, msg, cause error) error {
	s.warnStep(snap, msg, cause)
	return backoff.Permanent(fail(msg, cause))
}

// retry leaves the decision to the runner: a wait for the network or the quota, else a retry with backoff.
func (s *Service) retry(snap job.Snapshot, msg, cause error) error {
	s.warnStep(snap, msg, cause)
	return fail(msg, cause)
}

func (s *Service) warnStep(snap job.Snapshot, msg, cause error) {
	attrs := []any{slog.String("job", snap.ID().String()), slog.String("error", msg.Error())}
	if cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	s.log.Warn(opWriteStep, attrs...)
}
