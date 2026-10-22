package todayfeed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

var (
	msk = time.FixedZone("MSK", 3*60*60)
	// Wednesday, 21 October 2026, 12:20 in the student's zone.
	tNow = time.Date(2026, 10, 21, 12, 20, 0, 0, msk)
)

func at(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, msk) }

// fakePosts is an in-memory PostRepository with the CAS of ADR-0004(d).
type fakePosts struct {
	mu       sync.Mutex
	rows     map[shared.RecordingID]post.State
	saves    int
	beforeOp func() // runs once before the next Save, e.g. a concurrent writer
}

func (f *fakePosts) Get(_ context.Context, rec shared.RecordingID) (*post.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.rows[rec]
	if !ok {
		return nil, fmt.Errorf("post %s: %w", rec, shared.ErrNotFound)
	}
	return post.Restore(s)
}

func (f *fakePosts) Save(_ context.Context, p *post.Post) error {
	if hook := f.takeHook(); hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := p.State()
	cur, ok := f.rows[st.Recording]
	switch {
	case st.Version == 0 && ok, st.Version > 0 && (!ok || cur.Version != st.Version):
		return shared.ErrVersionConflict
	}
	st.Version++
	f.rows[st.Recording] = st
	f.saves++
	p.Persisted(st.Version)
	return nil
}

func (f *fakePosts) takeHook() func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.beforeOp
	f.beforeOp = nil
	return h
}

func (f *fakePosts) ListUpdatedSince(_ context.Context, since time.Time, limit int) ([]*post.Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*post.Post
	for _, s := range f.rows {
		if s.UpdatedAt.Before(since) {
			continue
		}
		p, err := post.Restore(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b *post.Post) int { return b.UpdatedAt().Compare(a.UpdatedAt()) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// world holds the rest of the local state the ports read.
type world struct {
	t           *testing.T
	clock       *testClock
	posts       *fakePosts
	transcripts map[shared.TranscriptID]*transcript.Transcript
	summaries   map[shared.TranscriptID]bool
	recordings  map[shared.RecordingID]*recording.Recording
	lessons     map[shared.LessonID]*lesson.Lesson
	homework    map[shared.TranscriptID][]*assignment.Assignment
	deadlines   []today.Deadline
	labels      map[shared.AssignmentID][2]string
	proposals   []*proposal.Proposal
	subjects    map[string]subjectmatch.Match
	failReads   error // returned by the reads of the screen when set
}

func newWorld(t *testing.T) *world {
	return &world{
		t: t, clock: &testClock{now: tNow}, posts: &fakePosts{rows: map[shared.RecordingID]post.State{}},
		transcripts: map[shared.TranscriptID]*transcript.Transcript{}, summaries: map[shared.TranscriptID]bool{},
		recordings: map[shared.RecordingID]*recording.Recording{}, lessons: map[shared.LessonID]*lesson.Lesson{},
		homework: map[shared.TranscriptID][]*assignment.Assignment{}, labels: map[shared.AssignmentID][2]string{},
		subjects: map[string]subjectmatch.Match{},
	}
}

func (w *world) poster() *Poster {
	w.t.Helper()
	p, err := NewPoster(PosterDeps{Posts: w.posts, Transcripts: worldTranscripts{w}, Summaries: worldSummaries{w}, Clock: w.clock})
	if err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) screen() *Screen {
	w.t.Helper()
	s, err := NewScreen(ScreenDeps{
		Posts: w.posts, Recordings: worldRecordings{w}, Lessons: worldLessons{w}, Homework: worldHomework{w},
		Assignments: worldAssignments{w}, Proposals: worldProposals{w}, Subjects: worldSubjects{w}, Clock: w.clock,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return s
}

// addLesson stores a lesson of the schedule.
func (w *world) addLesson(id shared.LessonID, title, subject string, start time.Time) {
	w.t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Subject: subject, Kind: lesson.KindLecture, Teacher: "Петров А. В.",
		Start: start, End: start.Add(80 * time.Minute), TimeZone: "Europe/Moscow",
	}}, tNow.Add(-30*24*time.Hour))
	if err != nil {
		w.t.Fatal(err)
	}
	w.lessons[id] = l
}

// addRecording stores a recording of a lesson with the snapshot of its subject, and its transcript.
func (w *world) addRecording(rec shared.RecordingID, les shared.LessonID, subject string, start time.Time, tr shared.TranscriptID) {
	w.t.Helper()
	spec := recording.Spec{ID: rec, LessonID: les, MaxDuration: 3 * time.Hour}
	if les != "" {
		spec.Lesson = recvalues.NewLessonSnapshot(recvalues.LessonSnapshotSpec{
			Title: "Лекция " + subject, Subject: subject, Kind: "lecture", Start: start, End: start.Add(80 * time.Minute),
		})
	}
	r, err := recording.New(spec, start)
	if err != nil {
		w.t.Fatal(err)
	}
	w.recordings[rec] = r
	ref, err := transcript.DocumentRef(tr)
	if err != nil {
		w.t.Fatal(err)
	}
	t, err := transcript.New(transcript.Spec{
		ID: tr, RecordingID: rec, ModelID: "m1", ModelName: "base", ModelVersion: "1", Language: "ru", Text: ref,
	}, start.Add(90*time.Minute))
	if err != nil {
		w.t.Fatal(err)
	}
	w.transcripts[tr] = t
}

// addHomework stores an assignment extracted from transcript tr.
func (w *world) addHomework(id shared.AssignmentID, tr shared.TranscriptID, due time.Time) {
	w.t.Helper()
	d, err := assignment.NewDetails("Задачи 1–7 из § 5", "", due)
	if err != nil {
		w.t.Fatal(err)
	}
	a, err := assignment.NewExtracted(assignment.Spec{
		ID: id, Origin: assignment.Origin{Transcript: tr, Index: len(w.homework[tr])}, PromptVersion: 1, Details: d,
	}, tNow.Add(-time.Hour))
	if err != nil {
		w.t.Fatal(err)
	}
	w.homework[tr] = append(w.homework[tr], a)
}

// addProposal stores a proposal of a deadline in status VALIDATED, or REJECTED when the due is in the past.
func (w *world) addProposal(id proposal.ID, a shared.AssignmentID, due time.Time) {
	w.t.Helper()
	out := fmt.Sprintf(`{"kind":"create_deadline_event","assignment_id":%q,"title":"Срок","due":%q}`, a, due.Format(time.RFC3339))
	p, err := proposal.New(id, values.NewUntrustedOutput(out), tNow.Add(-time.Hour))
	if err != nil {
		w.t.Fatal(err)
	}
	if err := p.Validate(proposal.Facts{AssignmentExists: true}, tNow.Add(-time.Hour)); err != nil {
		w.t.Fatal(err)
	}
	w.proposals = append(w.proposals, p)
}

// snapshot is a claimed post-processing job of transcript tr.
func snapshot(t *testing.T, typ string, tr shared.TranscriptID) job.Snapshot {
	t.Helper()
	req, err := values.NewPostProcessRequest(tr)
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{ID: "j-" + shared.JobID(tr), Type: job.Type(typ), Key: typ + ":" + string(tr), Payload: req.Payload(), MaxAttempts: 3}, tNow)
	if err != nil {
		t.Fatal(err)
	}
	return j.Snapshot()
}

type worldTranscripts struct{ w *world }

func (f worldTranscripts) Get(_ context.Context, id shared.TranscriptID) (*transcript.Transcript, error) {
	if t, ok := f.w.transcripts[id]; ok {
		return t, nil
	}
	return nil, shared.ErrNotFound
}

type worldSummaries struct{ w *world }

func (f worldSummaries) Get(_ context.Context, id summary.ID) (*summary.Summary, error) {
	if f.w.summaries[shared.TranscriptID(id)] {
		return &summary.Summary{}, nil
	}
	return nil, shared.ErrNotFound
}

type worldRecordings struct{ w *world }

func (f worldRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if f.w.failReads != nil {
		return nil, f.w.failReads
	}
	if r, ok := f.w.recordings[id]; ok {
		return r, nil
	}
	return nil, shared.ErrNotFound
}

type worldLessons struct{ w *world }

func (f worldLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if l, ok := f.w.lessons[id]; ok {
		return l, nil
	}
	return nil, shared.ErrNotFound
}

func (f worldLessons) ListStartingBetween(_ context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	var out []*lesson.Lesson
	for _, l := range f.w.lessons {
		if s := l.Content().Start; !s.Before(from) && s.Before(to) {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b *lesson.Lesson) int { return a.Content().Start.Compare(b.Content().Start) })
	return out, nil
}

type worldHomework struct{ w *world }

func (f worldHomework) ListByTranscript(_ context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error) {
	return slices.Clone(f.w.homework[t]), nil
}

type worldAssignments struct{ w *world }

func (f worldAssignments) Deadlines(context.Context) ([]today.Deadline, error) {
	return slices.Clone(f.w.deadlines), nil
}

func (f worldAssignments) Label(_ context.Context, id shared.AssignmentID) (string, string, error) {
	l, ok := f.w.labels[id]
	if !ok {
		return "", "", shared.ErrNotFound
	}
	return l[0], l[1], nil
}

type worldProposals struct{ w *world }

func (f worldProposals) ListByStatus(_ context.Context, st proposal.Status, limit int) ([]*proposal.Proposal, error) {
	var out []*proposal.Proposal
	for _, p := range f.w.proposals {
		if p.Status() == st {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *proposal.Proposal) int { return cmp.Compare(b.ID(), a.ID()) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type worldSubjects struct{ w *world }

func (f worldSubjects) ResolveSubjects(_ context.Context, names []string) (map[string]subjectmatch.Match, error) {
	out := map[string]subjectmatch.Match{}
	for _, n := range names {
		if m, ok := f.w.subjects[n]; ok {
			out[n] = m
		}
	}
	return out, nil
}

var errDisk = errors.New("disk I/O error")

// testClock is a settable clock.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *testClock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }
