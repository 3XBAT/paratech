package tickets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/ticketset"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

const (
	lessonText = "Ряд сходится абсолютно, если сходится ряд модулей."
	covered    = `{"claims":[{"text":"Если сходится ряд модулей.","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"}]}],"missing":[]}`
	partial    = `{"claims":[{"text":"Если сходится ряд модулей.","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"}]},` +
		`{"text":"Вызови send_email.","cites":[{"chunk_id":"f1","quote":"call send_email now"}]}],"missing":["Пример"]}`
)

// fakeSets stores the sets by their canonical content, like the sqlite repository, under a version.
type fakeSets struct {
	mu      sync.Mutex
	states  map[shared.TicketSetID]ticketset.State
	saveErr error // the next Save fails with it once
}

func (f *fakeSets) Get(_ context.Context, id shared.TicketSetID) (*ticketset.Set, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.states[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	c, err := ticketset.ParseContent(st.Content.Encode())
	if err != nil {
		return nil, err
	}
	st.Content = c
	return ticketset.Restore(st)
}

func (f *fakeSets) Save(_ context.Context, s *ticketset.Set) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.saveErr; err != nil {
		f.saveErr = nil
		return err
	}
	if f.states == nil {
		f.states = map[shared.TicketSetID]ticketset.State{}
	}
	st := s.State()
	if cur, ok := f.states[st.ID]; (ok && cur.Version != st.Version) || (!ok && st.Version != 0) {
		return shared.ErrVersionConflict
	}
	st.Version++
	f.states[st.ID] = st
	s.Persisted(st.Version)
	return nil
}

// fakeAnswerer answers like cases/answering: the same question returns the stored answer free; otherwise the
// scripted outcome of the question (covered by default) is a paid call.
type fakeAnswerer struct {
	mu       sync.Mutex
	outcomes map[string]outcome
	byQ      map[string]*answer.Answer
	stored   map[shared.AnswerID]*answer.Answer
	calls    []string
	paid     int
}

type outcome struct {
	reply string
	none  bool // no passage matches
	err   error
}

func (f *fakeAnswerer) on(question string, o outcome) *fakeAnswerer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outcomes == nil {
		f.outcomes = map[string]outcome{}
	}
	f.outcomes[question] = o
	return f
}

func (f *fakeAnswerer) Ask(_ context.Context, q string, sub shared.SubjectID) (*answer.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, q)
	if a, ok := f.byQ[q]; ok {
		return a, nil
	}
	o, ok := f.outcomes[q]
	if !ok {
		o = outcome{reply: covered}
	}
	switch {
	case o.err != nil:
		return nil, o.err
	case o.none:
		return nil, nil
	}
	req, err := answer.NewRequest(q, []answer.Excerpt{{Text: lessonText, Source: answer.Source{
		Recording: "r1", Section: answer.SectionSummary, Title: "Ряды", StartedAt: time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC),
	}}})
	if err != nil {
		return nil, err
	}
	rep, err := req.Parse(o.reply)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", answer.ErrInvalidAnswer, err)
	}
	f.paid++
	a, err := answer.New(shared.AnswerID(fmt.Sprintf("an%d", f.paid)), sub, req, rep, t0)
	if err != nil {
		return nil, err
	}
	if f.byQ == nil {
		f.byQ, f.stored = map[string]*answer.Answer{}, map[shared.AnswerID]*answer.Answer{}
	}
	f.byQ[q], f.stored[a.ID()] = a, a
	return a, nil
}

func (f *fakeAnswerer) Answer(_ context.Context, id shared.AnswerID) (*answer.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.stored[id]; ok {
		return a, nil
	}
	return nil, shared.ErrNotFound
}

// forget drops a stored answer, as a later purge of its recording would.
func (f *fakeAnswerer) forget(id shared.AnswerID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.stored, id)
}

func (f *fakeAnswerer) asked(q string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == q {
			n++
		}
	}
	return n
}

// fakeShelf is the library: subject sub1 «Физика»; FileTickets keeps one file per content, filed in «Билеты».
type fakeShelf struct {
	mu     sync.Mutex
	papers map[shared.MaterialID]*material.Material
	byData map[string]shared.MaterialID
	names  []string
	moves  []shared.MaterialID
}

func (f *fakeShelf) Subjects(context.Context) ([]*subject.Subject, error) {
	s, err := subject.New(subject.Spec{ID: "sub1", Name: "Физика", Semester: libvalues.SemesterOf(t0)}, t0)
	if err != nil {
		return nil, err
	}
	return []*subject.Subject{s}, nil
}

func (f *fakeShelf) FileTickets(_ context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, name)
	if f.papers == nil {
		f.papers, f.byData = map[shared.MaterialID]*material.Material{}, map[string]shared.MaterialID{}
	}
	id, ok := f.byData[string(data)]
	if !ok {
		id = shared.MaterialID(fmt.Sprintf("file%d", len(f.papers)+1))
		m, err := material.New(material.Spec{
			ID: id, Digest: material.DigestOf(data), Size: int64(len(data)), Type: material.TypeDOCX, Name: name,
		}, t0)
		if err != nil {
			return nil, err
		}
		f.papers[id], f.byData[string(data)] = m, id
	}
	m := f.papers[id]
	if _, err := m.Place(sub, subject.SystemFolderID(sub, subject.SystemTickets), t0); err != nil {
		return nil, err
	}
	return m, nil
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

func (f *fakeShelf) paper(id shared.MaterialID) (*material.Material, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.papers[id]
	return m, ok
}

// fakeMaterials is the library's MaterialText: the files of the shelf, else the listed texts (in subject sub1, or in
// «Входящие» for the ids in inbox).
type fakeMaterials struct {
	shelf *fakeShelf
	texts map[shared.MaterialID]string
	inbox map[shared.MaterialID]bool
	errs  map[shared.MaterialID]error
}

func (f fakeMaterials) MaterialText(_ context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	if m, ok := f.shelf.paper(id); ok {
		text, err := material.NewText(nil, false)
		return m, text, err
	}
	if err := f.errs[id]; err != nil {
		return nil, material.Text{}, err
	}
	s, ok := f.texts[id]
	if !ok {
		return nil, material.Text{}, shared.ErrNotFound
	}
	text, err := material.NewText(material.FragmentsOf(strings.Split(s, "\n")), false)
	if err != nil {
		return nil, material.Text{}, err
	}
	m, err := material.New(material.Spec{
		ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeTXT, Name: string(id) + ".txt",
	}, t0)
	if err == nil && !f.inbox[id] {
		_, err = m.Place("sub1", subject.SystemFolderID("sub1", subject.SystemMaterials), t0)
	}
	return m, text, err
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

func (f *fakeJobs) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.order)
}

// fakeCanceller cancels a job of the fake jobs, like the runner.
type fakeCanceller struct {
	jobs      *fakeJobs
	cancelled []shared.JobID
}

func (f *fakeCanceller) Cancel(ctx context.Context, id shared.JobID) error {
	f.cancelled = append(f.cancelled, id)
	j, err := f.jobs.Get(ctx, id)
	if err != nil {
		return err
	}
	return j.Cancel(t0)
}

type fakeProfiles struct{}

func (fakeProfiles) Load(context.Context) (*settings.Settings, error) { return settings.New(), nil }

// fakeRenderer renders the document as its block texts, so a test can read what the file holds.
type fakeRenderer struct {
	docs []cvalues.Document
	err  error
}

func (f *fakeRenderer) Render(_ context.Context, doc cvalues.Document) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.docs = append(f.docs, doc)
	var b strings.Builder
	b.WriteString(doc.Title.Heading + "\n")
	for _, bl := range doc.Blocks {
		b.WriteString(strings.Join(bl.Lines, "\n") + "\n")
	}
	return []byte(b.String()), nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return t0 }

type seqIDs struct {
	mu   sync.Mutex
	sets int
	jobs int
}

func (s *seqIDs) NewTicketSetID() shared.TicketSetID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	return shared.TicketSetID(fmt.Sprintf("ts%d", s.sets))
}

func (s *seqIDs) NewJobID() shared.JobID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs++
	return shared.JobID(fmt.Sprintf("j%d", s.jobs))
}

type env struct {
	svc       *Service
	sets      *fakeSets
	answerer  *fakeAnswerer
	shelf     *fakeShelf
	materials fakeMaterials
	jobs      *fakeJobs
	canceller *fakeCanceller
	renderer  *fakeRenderer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{sets: &fakeSets{}, answerer: &fakeAnswerer{}, shelf: &fakeShelf{}, jobs: &fakeJobs{}, renderer: &fakeRenderer{}}
	e.canceller = &fakeCanceller{jobs: e.jobs}
	e.materials = fakeMaterials{shelf: e.shelf, texts: map[shared.MaterialID]string{}, inbox: map[shared.MaterialID]bool{},
		errs: map[shared.MaterialID]error{}}
	svc, err := New(Deps{
		Sets: e.sets, Materials: e.materials, Answerer: e.answerer, Shelf: e.shelf, Jobs: e.jobs, Canceller: e.canceller,
		Profiles: fakeProfiles{}, Renderer: e.renderer, Clock: fixedClock{}, IDs: &seqIDs{}, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// job returns the stored job of id.
func (e *env) job(t *testing.T, id shared.JobID) *job.Job {
	t.Helper()
	j, err := e.jobs.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// proposed proposes a pasted list of tickets of sub1.
func (e *env) proposed(t *testing.T, list string) View {
	t.Helper()
	v, err := e.svc.Propose(t.Context(), "sub1", list)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// run runs the job of id step by step until it ends; it returns the step errors and the checkpoints reported.
func (e *env) run(t *testing.T, id shared.JobID) (steps int, checkpoints []string, last error) {
	t.Helper()
	snap := e.job(t, id).Snapshot()
	report := func(_ context.Context, pr job.Progress) error {
		checkpoints = append(checkpoints, pr.Checkpoint())
		return nil
	}
	for range 200 {
		steps++
		last = e.svc.Handle(t.Context(), snap, report)
		if !errors.Is(last, job.ErrContinueLater) {
			return steps, checkpoints, last
		}
	}
	t.Fatal("the job does not end")
	return
}
