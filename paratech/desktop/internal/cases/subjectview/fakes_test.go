package subjectview

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/today"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var (
	msk = time.FixedZone("MSK", 3*60*60)
	// now is Wednesday 21.10.2026, 12:00 in the student's zone.
	now     = time.Date(2026, 10, 21, 12, 0, 0, 0, msk)
	errDisk = errors.New("disk failed")
)

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

type fakeSubjects struct {
	subs map[shared.SubjectID]*subject.Subject
	err  error
}

func (f *fakeSubjects) Get(_ context.Context, id shared.SubjectID) (*subject.Subject, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.subs[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return s, nil
}

type fakeMaterials struct {
	list map[shared.SubjectID][]*material.Material
	err  error
}

func (f *fakeMaterials) ListIn(_ context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	return f.list[sub], f.err
}

// fakeResolver resolves the strings it knows; calls counts its calls.
type fakeResolver struct {
	known map[string]shared.SubjectID
	calls int
	err   error
}

func (f *fakeResolver) ResolveSubjects(_ context.Context, names []string) (map[string]subjectmatch.Match, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]subjectmatch.Match{}
	for _, n := range names {
		if id, ok := f.known[n]; ok {
			out[n] = subjectmatch.Match{ID: id, Name: n}
		}
	}
	return out, nil
}

type fakeAssignments struct {
	list []today.Deadline
	err  error
}

func (f *fakeAssignments) Deadlines(context.Context) ([]today.Deadline, error) { return f.list, f.err }

// fakeSchedule returns the lessons starting in the asked window and records it.
type fakeSchedule struct {
	lessons  []*lesson.Lesson
	from, to time.Time
	err      error
}

func (f *fakeSchedule) ListStartingBetween(_ context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	f.from, f.to = from, to
	var out []*lesson.Lesson
	for _, l := range f.lessons {
		if s := l.Content().Start; !s.Before(from) && s.Before(to) {
			out = append(out, l)
		}
	}
	return out, f.err
}

type fakeKnowledge struct {
	byKey map[string][]kb.Lesson
	err   error
}

func (f *fakeKnowledge) Lessons(_ context.Context, key string) ([]kb.Lesson, error) {
	return f.byKey[key], f.err
}

type fakeSummaries struct {
	sums map[summary.ID]*summary.Summary
	err  error
}

func (f *fakeSummaries) Get(_ context.Context, id summary.ID) (*summary.Summary, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.sums[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return s, nil
}

type fakeFailures struct {
	ids []shared.RecordingID
	err error
}

func (f *fakeFailures) FailedRecordings(context.Context) ([]shared.RecordingID, error) {
	return f.ids, f.err
}

type fakeVisits struct {
	at      map[shared.SubjectID]time.Time
	readErr error
	saveErr error
}

func (f *fakeVisits) LastOpened(_ context.Context, id shared.SubjectID) (time.Time, error) {
	return f.at[id], f.readErr
}

func (f *fakeVisits) MarkOpened(_ context.Context, id shared.SubjectID, at time.Time) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.at[id] = at
	return nil
}

// env is a Service over fakes with subject s1 «Линейная алгебра» (alias «Линал») and s2 «Физика».
type env struct {
	subjects    *fakeSubjects
	materials   *fakeMaterials
	resolver    *fakeResolver
	assignments *fakeAssignments
	schedule    *fakeSchedule
	knowledge   *fakeKnowledge
	summaries   *fakeSummaries
	failures    *fakeFailures
	visits      *fakeVisits
	svc         *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		subjects:  &fakeSubjects{subs: map[shared.SubjectID]*subject.Subject{}},
		materials: &fakeMaterials{list: map[shared.SubjectID][]*material.Material{}},
		resolver: &fakeResolver{known: map[string]shared.SubjectID{
			"Линейная алгебра": "s1", "Линал": "s1", "Физика": "s2",
		}},
		assignments: &fakeAssignments{},
		schedule:    &fakeSchedule{},
		knowledge:   &fakeKnowledge{byKey: map[string][]kb.Lesson{}},
		summaries:   &fakeSummaries{sums: map[summary.ID]*summary.Summary{}},
		failures:    &fakeFailures{},
		visits:      &fakeVisits{at: map[shared.SubjectID]time.Time{}},
	}
	for id, name := range map[shared.SubjectID]string{"s1": "Линейная алгебра", "s2": "Физика"} {
		sub, err := subject.New(subject.Spec{ID: id, Name: name, Semester: libvalues.SemesterOf(now)}, now.AddDate(0, -1, 0))
		if err != nil {
			t.Fatal(err)
		}
		e.subjects.subs[id] = sub
	}
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Resolver: e.resolver, Assignments: e.assignments,
		Schedule: e.schedule, Knowledge: e.knowledge, Summaries: e.summaries, Failures: e.failures, Visits: e.visits,
		Clock: fakeClock{now},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// addLesson adds a scheduled lesson of subject string sub starting at start, 90 minutes long.
func (e *env) addLesson(t *testing.T, id shared.LessonID, sub string, kind lesson.Kind, start time.Time) {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: sub, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow", Subject: sub, Kind: kind,
	}}, now.AddDate(0, -1, 0))
	if err != nil {
		t.Fatal(err)
	}
	e.schedule.lessons = append(e.schedule.lessons, l)
}

// addSummary stores the summary of transcript tr, made at at.
func (e *env) addSummary(t *testing.T, tr shared.TranscriptID, at time.Time) {
	t.Helper()
	c, err := summary.NewContent("Текст конспекта.", nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: tr, PromptVersion: 1, Content: c}, at)
	if err != nil {
		t.Fatal(err)
	}
	e.summaries.sums[summary.IDOf(tr)] = s
}

// addMaterial files a material of subject sub in folder f, last changed at updated.
func (e *env) addMaterial(t *testing.T, id shared.MaterialID, sub shared.SubjectID, f shared.FolderID, status material.Status, updated time.Time) {
	t.Helper()
	m, err := material.Restore(material.State{
		Version: 1, ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeTXT, Name: string(id) + ".txt",
		Subject: sub, Folder: f, Status: status, Text: material.TextPending, CreatedAt: updated, UpdatedAt: updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.materials.list[sub] = append(e.materials.list[sub], m)
}
