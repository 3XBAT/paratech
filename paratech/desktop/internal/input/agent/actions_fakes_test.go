package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// writeCall is an admitted action call (class U) as agentturn returns it.
func writeCall(t *testing.T, id string, tool values.Tool, args string) agentturn.Call {
	t.Helper()
	r, err := actionpolicy.Of(tool)
	if err != nil {
		t.Fatal(err)
	}
	return agentturn.Call{Call: mustCall(t, id, tool, args), Class: actionpolicy.ClassU, Paid: r.Paid, PlanStep: -1}
}

// --- the library -------------------------------------------------------------------------------------

// memLibrary is the library on real aggregates; every change bumps the version like the store does.
type memLibrary struct {
	subjects  map[shared.SubjectID]*subject.Subject
	materials map[shared.MaterialID]*material.Material
	next      int
	calls     []string
	// onCreate, when set, runs inside CreateSubject (to end the Driver's context meanwhile).
	onCreate func()
}

func newMemLibrary(t *testing.T) *memLibrary {
	t.Helper()
	l := &memLibrary{subjects: map[shared.SubjectID]*subject.Subject{}, materials: map[shared.MaterialID]*material.Material{}}
	phys := newSubject(t, "s1", "Физика")
	phys.Persisted(1)
	l.subjects["s1"] = phys
	for _, id := range []shared.MaterialID{"m1", "m2"} {
		m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 2, Type: material.TypeTXT, Name: "x.txt"}, now)
		if err != nil {
			t.Fatal(err)
		}
		m.Persisted(1)
		l.materials[id] = m
	}
	return l
}

func bump(v interface {
	Version() int64
	Persisted(int64)
}) {
	v.Persisted(v.Version() + 1)
}

func (l *memLibrary) Subjects(context.Context) ([]*subject.Subject, error) {
	out := make([]*subject.Subject, 0, len(l.subjects))
	for _, s := range l.subjects {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *subject.Subject) int { return compareStrings(string(a.ID()), string(b.ID())) })
	return out, nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (l *memLibrary) ResolveSubjects(_ context.Context, names []string) (map[string]subjectmatch.Match, error) {
	out := map[string]subjectmatch.Match{}
	for _, n := range names {
		for _, s := range l.subjects {
			if s.HasKey(libvalues.Key(n)) {
				out[n] = subjectmatch.Match{ID: s.ID(), Name: s.Name().String()}
			}
		}
	}
	return out, nil
}

func (l *memLibrary) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	if m, ok := l.materials[id]; ok {
		return m, nil
	}
	return nil, library.ErrMaterialNotFound
}

func (l *memLibrary) CreateSubject(_ context.Context, req library.NewSubject) (*subject.Subject, error) {
	l.calls = append(l.calls, "CreateSubject")
	if l.onCreate != nil {
		l.onCreate()
	}
	if m, _ := l.ResolveSubjects(context.Background(), []string{req.Name}); len(m) > 0 {
		return nil, library.ErrNameTaken
	}
	l.next++
	s, err := subject.New(subject.Spec{ID: shared.SubjectID(fmt.Sprintf("s-new%d", l.next)), Name: req.Name, Semester: libvalues.SemesterOf(now)}, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", library.ErrInvalidName, err)
	}
	s.Persisted(1)
	l.subjects[s.ID()] = s
	return s, nil
}

func (l *memLibrary) subject(id shared.SubjectID) (*subject.Subject, error) {
	if s, ok := l.subjects[id]; ok {
		return s, nil
	}
	return nil, library.ErrSubjectNotFound
}

func (l *memLibrary) CreateFolder(_ context.Context, id shared.SubjectID, name string) (subject.Folder, error) {
	s, err := l.subject(id)
	if err != nil {
		return subject.Folder{}, err
	}
	fn, err := subject.NewFolderName(name)
	if err != nil {
		return subject.Folder{}, library.ErrInvalidFolderName
	}
	l.next++
	f, err := s.AddFolder(shared.FolderID(fmt.Sprintf("f-new%d", l.next)), fn, now)
	if errors.Is(err, subject.ErrFolderExists) {
		return subject.Folder{}, library.ErrFolderTaken
	}
	if err != nil {
		return subject.Folder{}, err
	}
	bump(s)
	return f, nil
}

func (l *memLibrary) count(sub shared.SubjectID, folder shared.FolderID) int {
	n := 0
	for _, m := range l.materials {
		if m.Subject() == sub && (folder == "" || m.Folder() == folder) {
			n++
		}
	}
	return n
}

func (l *memLibrary) RemoveSubject(_ context.Context, id shared.SubjectID) error {
	s, err := l.subject(id)
	if err != nil {
		return err
	}
	if err := s.Removable(l.count(id, "")); err != nil {
		return library.ErrNotEmpty
	}
	delete(l.subjects, id)
	return nil
}

func (l *memLibrary) RemoveFolder(_ context.Context, id shared.SubjectID, folder shared.FolderID) error {
	s, err := l.subject(id)
	if err != nil {
		return err
	}
	if err := s.RemoveFolder(folder, l.count(id, folder), now); err != nil {
		if errors.Is(err, subject.ErrNotEmpty) {
			return library.ErrNotEmpty
		}
		return library.ErrFolderNotFound
	}
	bump(s)
	return nil
}

func (l *memLibrary) change(id shared.MaterialID, fn func(m *material.Material) (bool, error)) (*material.Material, error) {
	m, ok := l.materials[id]
	if !ok {
		return nil, library.ErrMaterialNotFound
	}
	changed, err := fn(m)
	if errors.Is(err, material.ErrRemoved) {
		return nil, library.ErrMaterialRemoved
	}
	if err != nil {
		return nil, err
	}
	if changed {
		bump(m)
	}
	return m, nil
}

func (l *memLibrary) Place(_ context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error) {
	s, err := l.subject(sub)
	if err != nil {
		return nil, err
	}
	f, ok := s.Folder(folder)
	if folder == "" {
		f, ok = s.SystemFolder(subject.SystemMaterials)
	}
	if !ok {
		return nil, library.ErrFolderNotFound
	}
	return l.change(id, func(m *material.Material) (bool, error) { return m.Place(sub, f.ID(), now) })
}

func (l *memLibrary) Link(_ context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error) {
	if lesson == "gone" {
		return nil, library.ErrLessonNotFound
	}
	return l.change(id, func(m *material.Material) (bool, error) { return m.Link(lesson, now) })
}

func (l *memLibrary) MoveToInbox(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	return l.change(id, func(m *material.Material) (bool, error) { return m.MoveToInbox(now) })
}

func (l *memLibrary) Unlink(_ context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error) {
	return l.change(id, func(m *material.Material) (bool, error) { return m.Unlink(lesson, now) })
}

// --- lessons, assignments, jobs ------------------------------------------------------------------------

type memLessons struct {
	lessons map[shared.RecordingID]kb.Lesson
}

func (l *memLessons) Lesson(_ context.Context, id shared.RecordingID) (kb.Lesson, error) {
	if x, ok := l.lessons[id]; ok {
		return x, nil
	}
	return kb.Lesson{}, knowledgebase.ErrRecordingNotFound
}

func (l *memLessons) SetLessonInfo(_ context.Context, id shared.RecordingID, subjectName, kind string) (kb.Lesson, error) {
	x, ok := l.lessons[id]
	if !ok {
		return kb.Lesson{}, knowledgebase.ErrRecordingNotFound
	}
	x.Subject, x.SubjectEdited, x.Kind, x.KindEdited = subjectName, subjectName != "", kind, kind != ""
	l.lessons[id] = x
	return x, nil
}

type memAssignments struct {
	views map[shared.AssignmentID]homeworkflow.AssignmentView
}

func (a *memAssignments) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	if v, ok := a.views[id]; ok {
		return v, nil
	}
	return homeworkflow.AssignmentView{}, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
}

func (a *memAssignments) EditAssignment(_ context.Context, e homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error) {
	v, ok := a.views[e.ID]
	if !ok {
		return homeworkflow.AssignmentView{}, shared.ErrNotFound
	}
	v.Title, v.Description, v.Due, v.UserEdited = e.Title, e.Description, e.Due.UTC(), true
	if e.Due.IsZero() {
		v.Due = time.Time{}
	}
	v.Version++
	a.views[e.ID] = v
	return v, nil
}

// memJobs starts and cancels jobs and answers JobStates through the activity fake.
type memJobs struct {
	activity *fakeActivity
	started  []string
	err      error
	stuck    bool // Cancel refuses (a running job of an uncancellable type)
}

func (j *memJobs) start(what string) (shared.JobID, error) {
	if j.err != nil {
		return "", j.err
	}
	id := shared.JobID(fmt.Sprintf("job%d", len(j.started)+1))
	j.started = append(j.started, what)
	j.activity.jobs = append(j.activity.jobs, activity.JobState{ID: id, Type: "x", Status: job.StatusQueued})
	return id, nil
}

func (j *memJobs) Retry(_ context.Context, rec shared.RecordingID, work homeworkflow.Work) (shared.JobID, error) {
	return j.start(string(work) + ":" + string(rec))
}

func (j *memJobs) RedoSummary(_ context.Context, rec shared.RecordingID) (shared.JobID, error) {
	return j.start("redo:" + string(rec))
}

func (j *memJobs) set(id shared.JobID, st job.Status) {
	for i := range j.activity.jobs {
		if j.activity.jobs[i].ID == id {
			j.activity.jobs[i].Status = st
		}
	}
}

func (j *memJobs) Cancel(_ context.Context, id shared.JobID) error {
	for _, s := range j.activity.jobs {
		if s.ID != id {
			continue
		}
		if j.stuck || s.Status.IsTerminal() {
			return errors.New("job cannot be cancelled")
		}
		j.set(id, job.StatusCancelled)
		return nil
	}
	return shared.ErrNotFound
}

type memDrafts struct {
	jobs    *memJobs
	active  map[shared.AssignmentID]shared.JobID
	created bool
}

func (d *memDrafts) ActiveGeneration(_ context.Context, a shared.AssignmentID) (homeworkflow.GenerationView, bool, error) {
	id, ok := d.active[a]
	return homeworkflow.GenerationView{JobID: id, Status: job.StatusRunning}, ok, nil
}

func (d *memDrafts) Generate(_ context.Context, a shared.AssignmentID, instruction string) (homeworkflow.GenerateResult, error) {
	id, err := d.jobs.start("draft:" + string(a) + ":" + instruction)
	return homeworkflow.GenerateResult{JobID: id, Created: !d.created}, err
}

type directTx struct{ n int }

func (tx *directTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	tx.n++
	return fn(ctx)
}

// --- the journal -------------------------------------------------------------------------------------

// memActions is the journal store: one entry per call of a request, CAS on the version.
type memActions struct {
	mu     sync.Mutex
	states map[shared.ActionID]action.State
	order  []shared.ActionID
}

func (r *memActions) Get(_ context.Context, id shared.ActionID) (*action.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return action.Restore(st)
}

func (r *memActions) list(ctx context.Context) []*action.Action {
	r.mu.Lock()
	ids := slices.Clone(r.order)
	r.mu.Unlock()
	slices.Reverse(ids)
	out := make([]*action.Action, 0, len(ids))
	for _, id := range ids {
		a, _ := r.Get(ctx, id)
		out = append(out, a)
	}
	return out
}

func (r *memActions) GetByCall(ctx context.Context, th shared.ThreadID, turn int, callID string) (*action.Action, error) {
	for _, a := range r.list(ctx) {
		if a.Thread() == th && a.Turn() == turn && a.CallID() == callID {
			return a, nil
		}
	}
	return nil, shared.ErrNotFound
}

func (r *memActions) Save(_ context.Context, a *action.Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states == nil {
		r.states = map[shared.ActionID]action.State{}
	}
	st := a.State()
	stored, ok := r.states[st.ID]
	if (st.Version == 0) == ok || (ok && stored.Version != st.Version) {
		return shared.ErrVersionConflict
	}
	if !ok {
		r.order = append(r.order, st.ID)
	}
	st.Version++
	r.states[st.ID] = st
	a.Persisted(st.Version)
	return nil
}

func (r *memActions) ListByThread(ctx context.Context, th shared.ThreadID, limit int) ([]*action.Action, error) {
	var out []*action.Action
	for _, a := range r.list(ctx) {
		if a.Thread() == th && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memActions) ListRecent(ctx context.Context, limit int) ([]*action.Action, error) {
	all := r.list(ctx)
	return all[:min(limit, len(all))], nil
}

func (r *memActions) ListUnfinished(ctx context.Context) ([]*action.Action, error) {
	var out []*action.Action
	for _, a := range r.list(ctx) {
		if a.Status() == action.StatusPending || a.Status() == action.StatusUndoing {
			out = append(out, a)
		}
	}
	return out, nil
}

type seqActionIDs struct{ n int }

func (g *seqActionIDs) NewActionID() shared.ActionID {
	g.n++
	return shared.ActionID(fmt.Sprintf("act%d", g.n))
}

// world is everything the actions touch, with the real journal on a memory store and Actions as its compensator.
type world struct {
	*reads
	lib         *memLibrary
	lessons     *memLessons
	assignments *memAssignments
	jobs        *memJobs
	drafts      *memDrafts
	tx          *directTx
	store       *memActions
	journal     *agentjournal.Service
	actions     *Actions
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{reads: newReads(), lib: newMemLibrary(t), tx: &directTx{}, store: &memActions{}}
	w.lessons = &memLessons{lessons: map[shared.RecordingID]kb.Lesson{
		"r1": {Recording: "r1", Subject: "Физика", Kind: "lecture"},
		"r2": {Recording: "r2", Subject: "физ-ра (лаб)", SubjectEdited: true, Kind: "lab", KindEdited: true, Ready: kb.Readiness{Summary: true}},
	}}
	w.assignments = &memAssignments{views: map[shared.AssignmentID]homeworkflow.AssignmentView{
		"a1": {ID: "a1", Title: "Задачи 1-5", Due: time.Date(2026, 10, 24, 15, 30, 0, 0, time.UTC), Version: 3},
		"a2": {ID: "a2", Title: "Реферат", Version: 1},
	}}
	w.jobs = &memJobs{activity: w.reads.activity}
	w.drafts = &memDrafts{jobs: w.jobs, active: map[shared.AssignmentID]shared.JobID{}}
	var err error
	if w.actions, err = NewActions(ActionDeps{
		Library: w.lib, Lessons: w.lessons, Assignments: w.assignments, Rerunner: w.jobs, Drafts: w.drafts,
		Jobs: w.reads.activity, Canceller: w.jobs, Tx: w.tx, Clock: fixedClock{},
	}); err != nil {
		t.Fatal(err)
	}
	if w.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: w.store, Compensator: w.actions, Resolver: w.actions, Clock: fixedClock{}, IDs: &seqActionIDs{},
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) deps(turns Turns, sink Sink) Deps {
	d := w.reads.deps(turns, sink)
	d.Journal, d.Actions = w.journal, w.actions
	return d
}

func (w *world) tools(t *testing.T) tools {
	t.Helper()
	drv, err := New(w.deps(&scriptedTurns{}, &chanSink{}))
	if err != nil {
		t.Fatal(err)
	}
	return drv.tools
}

// req is the request the calls of an actions test belong to.
var req = agentturn.Step{Thread: th1, Turn: 1}
