package turnkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// «ДЗ». The lesson snapshot of the title page and the subject are taken now. A job of the same writing that runs or
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
	case errors.Is(err, work.ErrNotWritable):
		return nil, fmt.Errorf("%w: %w", ErrNotWritable, err)
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
	case work.StepOutline, work.StepSection:
		return s.writeStep(ctx, snap, report, w, st)
	case work.StepAssemble:
		return s.assemble(ctx, snap, report, w)
	case work.StepApprove:
		return nil // the outline waits for the student
	default:
		return s.permanent(snap, ErrWriteBlocked, nil)
	}
}

// writeStep asks the model for the outline or a section, checks the answer and stores it.
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
	step := prompts.WorkSection
	if st.Kind == work.StepOutline {
		step = prompts.WorkOutline
	}
	key := w.StepKey(st, prompts.WorkInstructions(step), data+"\x00"+note)
	req, err := prompts.BuildWork(step, data, note, s.writing.ModelAlias, key)
	if err != nil {
		return s.permanent(snap, ErrWriteGone, err)
	}
	res, err := s.writing.Writer.Complete(ctx, req)
	if err != nil {
		return s.retry(snap, ErrWriteLLM, err)
	}

	raw := res.Output.Unvalidated()
	var change func(*work.Work) error
	switch st.Kind {
	case work.StepOutline:
		o, perr := work.ParseOutline(raw)
		if perr != nil {
			return s.refusedStep(ctx, snap, w, st, perr)
		}
		change = func(w *work.Work) error { return w.SetOutline(o, s.clock.Now()) }
	default:
		t, perr := work.ParseSection(raw, texts.Materials)
		if perr != nil {
			return s.refusedStep(ctx, snap, w, st, perr)
		}
		change = func(w *work.Work) error { return w.WriteSection(st.Section, st.Revision, t, s.clock.Now()) }
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
	prefs, err := s.writing.Profiles.Load(ctx)
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	p := prefs.Profile().Fields()
	brief, _ := w.Brief()
	in := homeworkdoc.PaperInput{
		Student: homeworkdoc.Student{
			FullName: p.FullName, Group: p.Group, University: p.University, Faculty: p.Faculty, City: p.City,
		},
		Subject: wr.Snapshot.SubjectName, Teacher: wr.Snapshot.Teacher, Kind: work.TypeName(brief.WorkType),
		Topic: wr.Topic, Year: s.clock.Now().Year(),
	}
	for _, sec := range wr.Sections {
		in.Sections = append(in.Sections, homeworkdoc.PaperSection{Title: sec.Title, Paragraphs: sec.Text.Paragraphs})
		for _, r := range sec.Text.Sources {
			in.Sources = append(in.Sources, homeworkdoc.PaperSource{Text: r.Text, Verify: r.Material == ""})
		}
	}
	doc := homeworkdoc.BuildPaper(in)
	if err := doc.Validate(); err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	content, err := s.writing.Renderer.Render(ctx, doc)
	if err != nil {
		return s.permanent(snap, ErrWriteFile, err)
	}
	if err := s.progress(ctx, snap, report, w); err != nil {
		return err
	}
	// The paper of the previous generation leaves «ДЗ» first: a crash after it repeats the step, never loses the
	// reference to it. The same bytes filed again put that material back into «ДЗ».
	if prev := wr.File; prev != nil {
		if err := s.unfile(ctx, prev.Material); err != nil {
			return s.retry(snap, ErrWriteStorage, err)
		}
	}
	name := homeworkdoc.PaperFileName(work.TypeName(brief.WorkType), wr.Topic)
	m, err := s.writing.Shelf.FileHomework(ctx, wr.Snapshot.Subject, name, content)
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	ref, err := material.BlobRef(m.Digest())
	if err != nil {
		return s.permanent(snap, ErrWriteStorage, err)
	}
	file := work.File{Material: m.ID(), Artifact: ref.String(), Content: w.ContentKey()}
	got, err := s.saveStep(ctx, w, func(w *work.Work) error { return w.Finish(file, s.clock.Now()) })
	if err != nil {
		return s.retry(snap, ErrWriteStorage, err)
	}
	if got.Status() != work.StatusDone {
		// The outline changed while the file was built: the paper just filed is no paper of the work; the next step
		// writes what changed.
		if cur, _ := got.Writing(); cur.File == nil || cur.File.Material != m.ID() {
			if err := s.unfile(ctx, m.ID()); err != nil {
				return s.retry(snap, ErrWriteStorage, err)
			}
		}
		return job.ErrContinueLater
	}
	s.log.InfoContext(ctx, opWriteDone, slog.String("work", string(w.ID())), slog.String("material", string(m.ID())))
	return nil
}

// writingTexts are the texts of a writing call: the assignment, the teacher's material and the student's materials
// that may be cited (the assignment's material, or the uploaded file that is the assignment). They are part of the
// key of the call, so a local failure to read them is retried (ErrWriteStorage), never taken for «no material»; only
// a material that is gone, removed or has no readable text is left out.
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
		return work.Texts{Assignment: text.String(), Materials: citable(m)}, nil
	}

	a, err := s.assignments.Get(ctx, src.Assignment)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return work.Texts{}, backoff.Permanent(fail(ErrWriteGone, err))
	case err != nil:
		return work.Texts{}, fail(ErrWriteStorage, err)
	}
	d := a.Details()
	t := work.Texts{Assignment: strings.TrimSpace(d.Title() + "\n\n" + d.Description())}
	if mid := a.MaterialID(); mid != "" {
		m, text, err := s.materials.MaterialText(ctx, mid)
		switch {
		case err == nil:
			t.Material, t.Materials = text.String(), citable(m)
		case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved), noMaterialText(err):
			// The assignment is written from alone, as the reader read it.
		default:
			return work.Texts{}, fail(ErrWriteStorage, err)
		}
	}
	return t, nil
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
