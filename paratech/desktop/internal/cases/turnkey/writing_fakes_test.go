package turnkey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/coursework/assignment"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

const (
	outlineAnswer = `{"topic":"История Древней Руси","sections":[{"title":"Введение","words":200},` +
		`{"title":"Основная часть","words":800},{"title":"Заключение","words":200}]}`
	sectionAnswer = `{"paragraphs":["Текст раздела."],"sources":[{"material":"m1","text":""},` +
		`{"material":"","text":"Иванов И. И. История. М., 2020"}]}`
)

// fakeWriter answers like the Hub: a key seen before replays its first answer for free; otherwise the next scripted
// answer, the answer of answer when set, or the outline or a section by the task of the data.
type fakeWriter struct {
	mu       sync.Mutex
	scripted []string
	answer   func(req cvalues.LLMRequest) string
	seen     map[string]string
	reqs     []cvalues.LLMRequest
	err      error
}

func (f *fakeWriter) Complete(_ context.Context, req cvalues.LLMRequest) (cvalues.LLMResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return cvalues.LLMResult{}, f.err
	}
	if f.seen == nil {
		f.seen = map[string]string{}
	}
	out, ok := f.seen[req.IdempotencyKey()]
	switch {
	case ok:
	case len(f.scripted) > 0:
		out, f.scripted = f.scripted[0], f.scripted[1:]
	case f.answer != nil:
		out = f.answer(req)
	case strings.Contains(req.UntrustedData(), `"task":"outline"`):
		out = outlineAnswer
	default:
		out = sectionAnswer
	}
	f.seen[req.IdempotencyKey()] = out
	return cvalues.NewLLMResult(cvalues.NewUntrustedOutput(out), 10, 10)
}

// paid is the number of distinct keys asked.
func (f *fakeWriter) paid() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

// fakeJobs keeps jobs by key like the sqlite repository.
type fakeJobs struct {
	mu    sync.Mutex
	byKey map[string]*job.Job
	order []string
}

func (f *fakeJobs) Enqueue(_ context.Context, j *job.Job) (shared.JobID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byKey == nil {
		f.byKey = map[string]*job.Job{}
	}
	if old, ok := f.byKey[j.Key()]; ok {
		return old.ID(), false, nil
	}
	f.byKey[j.Key()] = j
	f.order = append(f.order, j.Key())
	return j.ID(), true, nil
}

func (f *fakeJobs) Get(_ context.Context, id shared.JobID) (*job.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.byKey {
		if j.ID() == id {
			return j, nil
		}
	}
	return nil, shared.ErrNotFound
}

func (f *fakeJobs) LatestByKeyPrefix(_ context.Context, typ job.Type, prefix string) (*job.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.order) - 1; i >= 0; i-- {
		if j := f.byKey[f.order[i]]; j.Type() == typ && strings.HasPrefix(j.Key(), prefix) {
			return j, nil
		}
	}
	return nil, shared.ErrNotFound
}

// fakeCanceller cancels a job of jobs as the runner does once the handler returned; a finished job is refused. before
// (when set) runs on the job first, as a run that goes on meanwhile; err (when set) then refuses the job, as for a
// RUNNING job no handler of this process runs.
type fakeCanceller struct {
	jobs   *fakeJobs
	before func(j *job.Job)
	err    error
	calls  []shared.JobID
}

func (f *fakeCanceller) Cancel(ctx context.Context, id shared.JobID) error {
	f.calls = append(f.calls, id)
	j, err := f.jobs.Get(ctx, id)
	if err != nil {
		return err
	}
	f.jobs.mu.Lock()
	defer f.jobs.mu.Unlock()
	if f.before != nil {
		f.before(j)
	}
	if f.err != nil {
		return f.err
	}
	return j.Cancel(t0)
}

type seqJobIDs struct{ n int }

func (s *seqJobIDs) NewJobID() shared.JobID {
	s.n++
	return shared.JobID(fmt.Sprintf("j%d", s.n))
}

type fakeProfiles struct{}

func (fakeProfiles) Load(context.Context) (*settings.Settings, error) { return settings.New(), nil }

// fakeRenderer renders the document as its block texts, so a test can read what the paper holds.
type fakeRenderer struct{ docs []cvalues.Document }

func (f *fakeRenderer) Render(_ context.Context, doc cvalues.Document) ([]byte, error) {
	f.docs = append(f.docs, doc)
	var b strings.Builder
	b.WriteString(doc.Title.Heading + "\n" + doc.Title.Topic + "\n")
	for _, bl := range doc.Blocks {
		b.WriteString(strings.Join(bl.Lines, "\n") + "\n")
	}
	return []byte(b.String()), nil
}

// fakeSlides renders a presentation as its slide titles and notes, so a test can read what the deck holds.
type fakeSlides struct{ decks []cvalues.Presentation }

func (f *fakeSlides) Render(_ context.Context, p cvalues.Presentation) ([]byte, error) {
	f.decks = append(f.decks, p)
	var b strings.Builder
	b.WriteString("pptx:" + string(p.Template) + "\n")
	for _, s := range p.Slides {
		b.WriteString(s.Title + "\n" + s.Notes + "\n")
	}
	return []byte(b.String()), nil
}

type fakeLabels struct{}

func (fakeLabels) LessonLabel(_ context.Context, id shared.AssignmentID) (cvalues.LessonLabel, error) {
	if id != "a1" {
		return cvalues.LessonLabel{}, shared.ErrNotFound
	}
	return cvalues.NewLessonLabel(cvalues.ScheduledLesson{Subject: "История", Teacher: "Петров П. П."}, cvalues.LessonNames{},
		cvalues.LessonNames{}), nil
}

// fakeShelf is the library: subject s1 «История»; FileHomework records the files and keeps one paper per content
// (the first is mat-paper), filed into «ДЗ» of the subject; MoveToInbox takes one back to «Входящие».
type fakeShelf struct {
	mu     sync.Mutex
	files  []filed
	papers map[shared.MaterialID]*material.Material
	byData map[string]shared.MaterialID
	moves  []shared.MaterialID
	err    error
}

// paper returns a paper the shelf keeps.
func (f *fakeShelf) paper(id shared.MaterialID) (*material.Material, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.papers[id]
	return m, ok
}

func (f *fakeShelf) MoveToInbox(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.papers[id]
	if !ok {
		return nil, errors.New("fake shelf: no such material")
	}
	if _, err := m.MoveToInbox(t0); err != nil {
		return nil, err
	}
	f.moves = append(f.moves, id)
	return m, nil
}

type filed struct {
	sub  shared.SubjectID
	name string
	data string
}

func (f *fakeShelf) Subjects(context.Context) ([]*subject.Subject, error) {
	s, err := subject.New(subject.Spec{ID: "s1", Name: "История", Semester: libvalues.SemesterOf(t0)}, t0)
	if err != nil {
		return nil, err
	}
	return []*subject.Subject{s}, nil
}

func (f *fakeShelf) ResolveSubjects(_ context.Context, names []string) (map[string]subjectmatch.Match, error) {
	out := map[string]subjectmatch.Match{}
	for _, n := range names {
		if n == "История" {
			out[n] = subjectmatch.Match{ID: "s1"}
		}
	}
	return out, nil
}

func (f *fakeShelf) FileHomework(_ context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.files = append(f.files, filed{sub: sub, name: name, data: string(data)})
	if f.papers == nil {
		f.papers, f.byData = map[shared.MaterialID]*material.Material{}, map[string]shared.MaterialID{}
	}
	id, ok := f.byData[string(data)]
	if !ok {
		id = "mat-paper"
		if n := len(f.papers); n > 0 {
			id = shared.MaterialID(fmt.Sprintf("mat-paper-%d", n+1))
		}
		m, err := material.New(material.Spec{
			ID: id, Digest: material.DigestOf(data), Size: int64(len(data)), Type: material.TypeDOCX, Name: name,
		}, t0)
		if err != nil {
			return nil, err
		}
		f.papers[id], f.byData[string(data)] = m, id
	}
	m := f.papers[id]
	if sub != "" {
		if _, err := m.Place(sub, subject.SystemFolderID(sub, subject.SystemHomework), t0); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// writeMaterials is the library's MaterialText: the papers of the shelf, else the materials of the triage env.
type writeMaterials struct {
	base  fakeMaterials
	shelf *fakeShelf
}

func (f writeMaterials) MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	if m, ok := f.shelf.paper(id); ok {
		text, err := material.NewText(nil, false)
		return m, text, err
	}
	return f.base.MaterialText(ctx, id)
}

// failingAssignments is the assignments port whose reads fail with err once set.
type failingAssignments struct {
	Assignments
	err *error
}

func (f failingAssignments) Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	if *f.err != nil {
		return nil, *f.err
	}
	return f.Assignments.Get(ctx, id)
}

// writeEnv is a service with the writing over fakes, and a work w1 of assignment a1 with confirmed requirements.
type writeEnv struct {
	*env
	writer        *fakeWriter
	jobs          *fakeJobs
	canceller     *fakeCanceller
	renderer      *fakeRenderer
	slides        *fakeSlides
	shelf         *fakeShelf
	materials     fakeMaterials
	assignmentErr error // the next reads of the assignment fail with it
}

func newWriteEnv(t *testing.T) *writeEnv {
	t.Helper()
	e := newEnv(t)
	we := &writeEnv{
		env: e, writer: &fakeWriter{}, jobs: &fakeJobs{}, renderer: &fakeRenderer{}, slides: &fakeSlides{}, shelf: &fakeShelf{},
	}
	we.materials = e.svc.materials.(fakeMaterials)
	we.canceller = &fakeCanceller{jobs: we.jobs}
	svc, err := New(Deps{
		Works: e.works, Assignments: failingAssignments{Assignments: e.svc.assignments, err: &we.assignmentErr},
		Materials: writeMaterials{base: we.materials, shelf: we.shelf}, Reader: e.reader,
		Writing: &WritingDeps{
			Writer: we.writer, Jobs: we.jobs, JobIDs: &seqJobIDs{}, Canceller: we.canceller, Profiles: fakeProfiles{},
			Renderer: we.renderer,
			Slides:   we.slides, Lessons: fakeLabels{}, Shelf: we.shelf, ModelAlias: "default",
		},
		Clock: fixedClock{}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	we.svc = svc
	return we
}

// confirmed triages a1 and confirms its requirements.
func (e *writeEnv) confirmed(t *testing.T) shared.WorkID {
	t.Helper()
	e.reader.then(out(work.FeasibleFull, 0))
	st, err := e.svc.Triage(t.Context(), work.Source{Assignment: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmRequirements(t.Context(), st.View.ID); err != nil {
		t.Fatal(err)
	}
	return st.View.ID
}

func (e *writeEnv) job(t *testing.T, id shared.JobID) *job.Job {
	t.Helper()
	j, err := e.jobs.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// approved starts the writing of confirmed work id, runs its outline job (it ends at the outline) and approves the
// outline as the student does; it returns the job of the sections.
func (e *writeEnv) approved(t *testing.T, id shared.WorkID) WritingStarted {
	t.Helper()
	st, err := e.svc.StartWriting(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if steps, err := e.run(t, st.Job, 10); err != nil || steps != 1 {
		t.Fatalf("outline job = %v after %d steps", err, steps)
	}
	ap, err := e.svc.ApproveOutline(t.Context(), id)
	if err != nil || !ap.Created || ap.Job == st.Job {
		t.Fatalf("ApproveOutline = %+v, %v", ap, err)
	}
	return ap
}

// run runs the steps of job id until it ends; it returns the result of the last step and the steps run.
func (e *writeEnv) run(t *testing.T, id shared.JobID, maxSteps int) (int, error) {
	t.Helper()
	j := e.job(t, id)
	for step := 1; step <= maxSteps; step++ {
		err := e.svc.Handle(t.Context(), j.Snapshot(), nil)
		if !errors.Is(err, job.ErrContinueLater) {
			if err == nil { // as the runner does, so the job is not active any more
				if cerr := errors.Join(j.Claim(t0), j.Complete(t0)); cerr != nil {
					t.Fatal(cerr)
				}
			}
			return step, err
		}
	}
	return maxSteps, errors.New("still running")
}
