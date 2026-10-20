package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// The student's zone in these tests; lesson times reach the model with this offset.
var msk = time.FixedZone("MSK", 3*60*60)

// ppT0 is "now" of the post-processing tests: the evening of the lesson day, in the student's zone.
var ppT0 = time.Date(2026, 10, 14, 20, 0, 0, 0, msk)

// lessonStart is the start of lesson l1 (Wednesday 14 October 2026, 10:00 MSK).
var lessonStart = time.Date(2026, 10, 14, 10, 0, 0, 0, msk)

// ppStore is the in-memory local database of the post-processing fakes. Aggregates are kept as State and rebuilt with
// Restore, as the sqlite adapter does, and Save is the version compare-and-swap of ADR-0004(d).
type ppStore struct {
	mu          sync.Mutex
	transcripts map[shared.TranscriptID]*transcript.Transcript
	recordings  map[shared.RecordingID]*recording.Recording
	lessons     map[shared.LessonID]*lesson.Lesson
	summaries   map[summary.ID]summary.State
	lectures    map[lecturetext.ID]lecturetext.State
	lectureSave error // returned by every lecture text save while set
	assignments map[shared.AssignmentID]assignment.State
	jobs        map[string]*job.Job // by idempotency key
	saves       int                 // successful summary and assignment saves

	failNextSave   error                        // returned once by the next summary or assignment save
	failNextDelete error                        // returned once by the next assignment delete
	beforeDelete   func()                       // run once before the next assignment delete, without the lock
	inUse          map[shared.AssignmentID]bool // assignments a delivery references
	readErr        error                        // returned by every transcript Get when set
	listErr        error                        // returned by every lesson ListStartingBetween when set
	listCalls      int                          // calls of lesson ListStartingBetween
	jobGetErr      error                        // returned by every job Get when set
	seq            int
}

func newPPStore() *ppStore {
	return &ppStore{
		transcripts: map[shared.TranscriptID]*transcript.Transcript{}, recordings: map[shared.RecordingID]*recording.Recording{},
		lessons: map[shared.LessonID]*lesson.Lesson{}, summaries: map[summary.ID]summary.State{},
		lectures:    map[lecturetext.ID]lecturetext.State{},
		assignments: map[shared.AssignmentID]assignment.State{}, jobs: map[string]*job.Job{},
		inUse: map[shared.AssignmentID]bool{},
	}
}

type ppTranscripts struct{ s *ppStore }

func (r ppTranscripts) Get(_ context.Context, id shared.TranscriptID) (*transcript.Transcript, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.readErr != nil {
		return nil, r.s.readErr
	}
	t, ok := r.s.transcripts[id]
	if !ok {
		return nil, fmt.Errorf("transcript %s: %w", id, shared.ErrNotFound)
	}
	return t, nil
}

// LatestByRecording returns the transcript of rec with the newest creation time.
func (r ppTranscripts) LatestByRecording(_ context.Context, rec shared.RecordingID) (*transcript.Transcript, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out *transcript.Transcript
	for _, t := range r.s.transcripts {
		if t.RecordingID() == rec && (out == nil || t.CreatedAt().After(out.CreatedAt())) {
			out = t
		}
	}
	if out == nil {
		return nil, fmt.Errorf("transcript of %s: %w", rec, shared.ErrNotFound)
	}
	return out, nil
}

type ppRecordings struct{ s *ppStore }

func (r ppRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rec, ok := r.s.recordings[id]
	if !ok {
		return nil, fmt.Errorf("recording %s: %w", id, shared.ErrNotFound)
	}
	return rec, nil
}

// ppTranscriptRecordings joins a transcript to its recording, like the sqlite adapter.
type ppTranscriptRecordings struct{ s *ppStore }

func (r ppTranscriptRecordings) RecordingOf(ctx context.Context, t shared.TranscriptID) (*recording.Recording, error) {
	tr, err := ppTranscripts(r).Get(ctx, t)
	if err != nil {
		return nil, err
	}
	return ppRecordings(r).Get(ctx, tr.RecordingID())
}

type ppLessons struct{ s *ppStore }

func (r ppLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	l, ok := r.s.lessons[id]
	if !ok {
		return nil, fmt.Errorf("lesson %s: %w", id, shared.ErrNotFound)
	}
	return l, nil
}

// ListStartingBetween lists the live lessons starting in [from, to) ordered by start, like the sqlite adapter.
func (r ppLessons) ListStartingBetween(_ context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.listCalls++
	if r.s.listErr != nil {
		return nil, r.s.listErr
	}
	var out []*lesson.Lesson
	for _, l := range r.s.lessons {
		if s := l.Content().Start; !l.Deleted() && !s.Before(from) && s.Before(to) {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b *lesson.Lesson) int { return a.Content().Start.Compare(b.Content().Start) })
	return out, nil
}

type ppSummaries struct{ s *ppStore }

func (r ppSummaries) Get(_ context.Context, id summary.ID) (*summary.Summary, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.summaries[id]
	if !ok {
		return nil, fmt.Errorf("summary %s: %w", id, shared.ErrNotFound)
	}
	return summary.Restore(st)
}

func (r ppSummaries) Save(_ context.Context, s *summary.Summary) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.takeSaveErr(); err != nil {
		return err
	}
	stored, exists := r.s.summaries[s.ID()]
	if (s.Version() == 0) == exists || (exists && stored.Version != s.Version()) {
		return shared.ErrVersionConflict
	}
	st := s.State()
	st.Version++
	r.s.summaries[s.ID()] = st
	r.s.saves++
	s.Persisted(st.Version)
	return nil
}

type ppLectures struct{ s *ppStore }

func (r ppLectures) Get(_ context.Context, id lecturetext.ID) (*lecturetext.LectureText, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.lectures[id]
	if !ok {
		return nil, fmt.Errorf("lecture text %s: %w", id, shared.ErrNotFound)
	}
	return lecturetext.Restore(st)
}

func (r ppLectures) Save(_ context.Context, l *lecturetext.LectureText) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.lectureSave != nil {
		return r.s.lectureSave
	}
	stored, exists := r.s.lectures[l.ID()]
	if (l.Version() == 0) == exists || (exists && stored.Version != l.Version()) {
		return shared.ErrVersionConflict
	}
	st := l.State()
	st.Version++
	r.s.lectures[l.ID()] = st
	l.Persisted(st.Version)
	return nil
}

type ppAssignments struct{ s *ppStore }

func (r ppAssignments) Get(_ context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	st, ok := r.s.assignments[id]
	if !ok {
		return nil, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	return assignment.Restore(st)
}

func (r ppAssignments) GetBySource(_ context.Context, t shared.TranscriptID, index int) (*assignment.Assignment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, st := range r.s.assignments {
		if st.Transcript == t && st.SourceIndex == index {
			return assignment.Restore(st)
		}
	}
	return nil, fmt.Errorf("assignment %s/%d: %w", t, index, shared.ErrNotFound)
}

func (r ppAssignments) ListByTranscript(_ context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error) {
	out := []*assignment.Assignment{}
	for _, st := range r.s.assignmentsOf(t) {
		a, err := assignment.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r ppAssignments) Delete(_ context.Context, a *assignment.Assignment) error {
	if hook := r.s.beforeDelete; hook != nil {
		r.s.beforeDelete = nil
		hook()
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.failNextDelete != nil {
		err := r.s.failNextDelete
		r.s.failNextDelete = nil
		return err
	}
	stored, ok := r.s.assignments[a.ID()]
	if !ok || stored.Version != a.Version() {
		return shared.ErrVersionConflict
	}
	if r.s.inUse[a.ID()] {
		return assignment.ErrInUse
	}
	delete(r.s.assignments, a.ID())
	return nil
}

func (r ppAssignments) Save(_ context.Context, a *assignment.Assignment) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err := r.s.takeSaveErr(); err != nil {
		return err
	}
	stored, exists := r.s.assignments[a.ID()]
	if (a.Version() == 0) == exists || (exists && stored.Version != a.Version()) {
		return shared.ErrVersionConflict
	}
	st := a.State()
	if !exists {
		for _, other := range r.s.assignments {
			if st.SourceIndex >= 0 && other.Transcript == st.Transcript && other.SourceIndex == st.SourceIndex {
				return shared.ErrDuplicate
			}
		}
	}
	st.Version++
	r.s.assignments[a.ID()] = st
	r.s.saves++
	a.Persisted(st.Version)
	return nil
}

// takeSaveErr returns and clears failNextSave; the caller holds mu.
func (s *ppStore) takeSaveErr() error {
	err := s.failNextSave
	s.failNextSave = nil
	return err
}

type ppJobs struct{ s *ppStore }

func (r ppJobs) Enqueue(_ context.Context, j *job.Job) (shared.JobID, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if old, ok := r.s.jobs[j.Key()]; ok {
		return old.ID(), false, nil
	}
	r.s.jobs[j.Key()] = j
	j.Persisted(1)
	return j.ID(), true, nil
}

// Get returns the stored job itself, so a test moves it through its statuses with the job methods.
func (r ppJobs) Get(_ context.Context, id shared.JobID) (*job.Job, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.jobGetErr != nil {
		return nil, r.s.jobGetErr
	}
	for _, j := range r.s.jobs {
		if j.ID() == id {
			return j, nil
		}
	}
	return nil, fmt.Errorf("job %s: %w", id, shared.ErrNotFound)
}

// LatestByKeyPrefix returns the stored job itself, newest by creation time, then id.
func (r ppJobs) LatestByKeyPrefix(_ context.Context, typ job.Type, prefix string) (*job.Job, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out *job.Job
	for key, j := range r.s.jobs {
		if j.Type() != typ || !strings.HasPrefix(key, prefix) {
			continue
		}
		if out == nil || j.CreatedAt().After(out.CreatedAt()) || (j.CreatedAt().Equal(out.CreatedAt()) && j.ID() > out.ID()) {
			out = j
		}
	}
	if out == nil {
		return nil, fmt.Errorf("job %s %s: %w", typ, prefix, shared.ErrNotFound)
	}
	return out, nil
}

type ppIDs struct{ s *ppStore }

func (g ppIDs) next(prefix string) string {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()
	g.s.seq++
	return fmt.Sprintf("%s%d", prefix, g.s.seq)
}

func (g ppIDs) NewAssignmentID() shared.AssignmentID { return shared.AssignmentID(g.next("a")) }
func (g ppIDs) NewJobID() shared.JobID               { return shared.JobID(g.next("j")) }
func (g ppIDs) NewProposalID() proposal.ID           { return proposal.ID(g.next("p")) }

// assignmentsOf returns the stored assignments of a transcript ordered by source index.
func (s *ppStore) assignmentsOf(t shared.TranscriptID) []assignment.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []assignment.State
	for _, st := range s.assignments {
		if st.Transcript == t && st.SourceIndex >= 0 {
			out = append(out, st)
		}
	}
	slices.SortFunc(out, func(a, b assignment.State) int { return a.SourceIndex - b.SourceIndex })
	return out
}

func (s *ppStore) summaryOf(t shared.TranscriptID) (summary.State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.summaries[summary.IDOf(t)]
	return st, ok
}

func (s *ppStore) records() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.summaries) + len(s.assignments)
}

// ppEnv is a PostProcessor over the fakes with lesson l1, its recording r1 and transcript t1.
type ppEnv struct {
	*ppStore
	t     *testing.T
	llm   *testenv.FakeLLM
	files *testenv.MemArtifacts
	clock *testenv.FakeClock
	pp    *PostProcessor
}

const testAlias = "default"

func newPPEnv(t *testing.T) *ppEnv {
	t.Helper()
	e := &ppEnv{
		ppStore: newPPStore(), t: t, llm: testenv.NewFakeLLM(), files: testenv.NewMemArtifacts(),
		clock: testenv.NewFakeClock(ppT0),
	}
	pp, err := NewPostProcessor(e.deps())
	if err != nil {
		t.Fatal(err)
	}
	e.pp = pp

	l, err := lesson.New(lesson.Spec{ID: "l1", Content: lesson.Content{
		Title: "Geography", Start: lessonStart.UTC(), End: lessonStart.Add(90 * time.Minute).UTC(), TimeZone: "Europe/Moscow",
	}}, ppT0)
	if err != nil {
		t.Fatal(err)
	}
	e.lessons["l1"] = l
	e.addTranscript("r1", "l1", "t1", lessonStart.Add(2*time.Minute), "Today we talk about rivers.", "Homework: read chapter 4 by Friday.")
	return e
}

func (e *ppEnv) deps() PostProcessDeps {
	return PostProcessDeps{
		LLM: e.llm, Transcripts: ppTranscripts{e.ppStore}, Recordings: ppRecordings{e.ppStore}, Lessons: ppLessons{e.ppStore},
		Artifacts: e.files, Summaries: ppSummaries{e.ppStore}, Assignments: ppAssignments{e.ppStore},
		LectureTexts: ppLectures{e.ppStore}, Jobs: ppJobs{e.ppStore}, Clock: e.clock, IDs: ppIDs{e.ppStore},
		ModelAlias: testAlias,
	}
}

// addTranscript stores a completed recording started at start, its transcript and the document with lines.
func (e *ppEnv) addTranscript(rec shared.RecordingID, les shared.LessonID, id shared.TranscriptID, start time.Time, lines ...string) {
	e.t.Helper()
	r, err := recording.New(recording.Spec{ID: rec, LessonID: les, MaxDuration: 3 * time.Hour}, start)
	if err != nil {
		e.t.Fatal(err)
	}
	e.recordings[rec] = r

	ref, err := transcript.DocumentRef(id)
	if err != nil {
		e.t.Fatal(err)
	}
	tr, err := transcript.New(transcript.Spec{
		ID: id, RecordingID: rec, ModelID: "m1", ModelName: "base", ModelVersion: "1", Language: "ru", Text: ref,
	}, start.Add(time.Hour))
	if err != nil {
		e.t.Fatal(err)
	}
	e.transcripts[id] = tr
	e.writeDocument(ref, lines...)
}

func (e *ppEnv) writeDocument(ref shared.ArtifactRef, lines ...string) {
	e.t.Helper()
	segs := make([]transcript.Segment, len(lines))
	for i, l := range lines {
		segs[i] = transcript.Segment{StartMs: int64(i) * 1000, EndMs: int64(i)*1000 + 900, Text: l}
	}
	doc, err := transcript.NewDocument("ru", segs)
	if err != nil {
		e.t.Fatal(err)
	}
	data, err := doc.Encode()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, strings.NewReader(string(data))); err != nil {
		e.t.Fatal(err)
	}
}

// snapshot builds the job a worker would run for typ and transcript t.
func (e *ppEnv) snapshot(id shared.JobID, typ job.Type, t shared.TranscriptID) job.Snapshot {
	e.t.Helper()
	j, err := job.New(job.Spec{
		ID: id, Type: typ, Key: string(id), Payload: fmt.Sprintf(`{"transcript_id":%q}`, t), MaxAttempts: 3,
	}, ppT0)
	if err != nil {
		e.t.Fatal(err)
	}
	return j.Snapshot()
}

// handle runs the job as the runner does: a step that returns job.ErrContinueLater (a lecture text part, TASK-097) is
// followed by the next one until the job ends.
func (e *ppEnv) handle(snap job.Snapshot) error {
	for {
		if err := e.pp.Handle(context.Background(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
			return err
		}
	}
}

// Compile-time checks: the fakes satisfy the ports they stand for.
var (
	_ TranscriptReader      = ppTranscripts{}
	_ RecordingReader       = ppRecordings{}
	_ TranscriptRecordings  = ppTranscriptRecordings{}
	_ LessonReader          = ppLessons{}
	_ SummaryRepository     = ppSummaries{}
	_ LectureTextRepository = ppLectures{}
	_ AssignmentRepository  = ppAssignments{}
	_ JobEnqueuer           = ppJobs{}
	_ JobReader             = ppJobs{}
	_ PostJobs              = ppJobs{}
	_ LatestTranscripts     = ppTranscripts{}
	_ SummaryReader         = ppSummaries{}
	_ IDGenerator           = ppIDs{}
	_ LLMClient             = (*testenv.FakeLLM)(nil)
)
