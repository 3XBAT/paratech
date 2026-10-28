package homeworkflow

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// fakeSubjects is the library of the tests: subject ID -> name.
type fakeSubjects struct {
	names map[shared.SubjectID]string
	err   error
}

func (f fakeSubjects) SubjectName(_ context.Context, id shared.SubjectID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	name, ok := f.names[id]
	if !ok {
		return "", fmt.Errorf("subject %s: %w", id, shared.ErrNotFound)
	}
	return name, nil
}

// allAssignments lists the stored assignments that are not deleted, filtered by done before the limit, open ones
// first, each group newest first, like the sqlite adapter.
type allAssignments struct{ s *ppStore }

func (l allAssignments) ListRecentByState(_ context.Context, open, done bool, limit int) ([]*assignment.Assignment, error) {
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	var states []assignment.State
	for _, st := range l.s.assignments {
		isDone := !st.DoneAt.IsZero()
		if st.DeletedAt.IsZero() && ((isDone && done) || (!isDone && open)) {
			states = append(states, st)
		}
	}
	slices.SortFunc(states, func(a, b assignment.State) int {
		return cmp.Or(cmp.Compare(boolRank(!a.DoneAt.IsZero()), boolRank(!b.DoneAt.IsZero())),
			b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	out := []*assignment.Assignment{}
	for _, st := range states[:min(limit, len(states))] {
		a, err := assignment.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// boolRank orders false before true.
func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

type createEnv struct {
	*ppEnv
	svc *Service
}

// newCreateEnv is the Service over the post-processing fakes with subject s1 «Матанализ» and lesson l2 of «Физика»
// (l1 of newPPEnv has no subject). Now is ppT0, 14 October 2026 20:00 MSK.
func newCreateEnv(t *testing.T, subjects SubjectNames) *createEnv {
	t.Helper()
	pe := newPPEnv(t)
	start := lessonStart.Add(24 * time.Hour)
	l, err := lesson.New(lesson.Spec{ID: "l2", Content: lesson.Content{
		Title: "Физика", Subject: "Физика", Start: start.UTC(), End: start.Add(90 * time.Minute).UTC(), TimeZone: "Europe/Moscow",
	}}, ppT0)
	if err != nil {
		t.Fatal(err)
	}
	pe.lessons["l2"] = l

	e := newEnv(t)
	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: ppAssignments{pe.ppStore},
		AssignmentList: allAssignments{pe.ppStore}, Artifacts: e.files,
		Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Recordings: ppTranscriptRecordings{pe.ppStore}, Lessons: ppLessons{pe.ppStore},
		Clock: pe.clock, IDs: ppIDs{pe.ppStore}, Tx: tx{e.store}, Subjects: subjects,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &createEnv{ppEnv: pe, svc: svc}
}

var testSubjects = fakeSubjects{names: map[shared.SubjectID]string{"s1": "Матанализ"}}

func TestCreateAssignment(t *testing.T) {
	cases := []struct {
		name        string
		in          NewAssignment
		wantSubject string
		wantSource  values.NameSource
		wantLesson  shared.LessonID
		wantDue     time.Time
	}{
		{
			name: "with a subject and a date", in: NewAssignment{Title: "Решить номер 5", Subject: "s1", Date: "2026-10-30"},
			wantSubject: "Матанализ", wantSource: values.SourceUser, wantDue: time.Date(2026, 10, 30, 23, 59, 0, 0, msk),
		},
		{
			name: "a reminder at nine", in: NewAssignment{Title: "Скинуть проверочную Иванову", Date: "2026-10-15", Time: "09:00"},
			wantDue: time.Date(2026, 10, 15, 9, 0, 0, 0, msk),
		},
		{
			name: "with a lesson: the subject of the lesson", in: NewAssignment{Title: "Задача 3", Lesson: "l2"},
			wantSubject: "Физика", wantSource: values.SourceSchedule, wantLesson: "l2",
		},
		{
			name: "the subject wins over the lesson", in: NewAssignment{Title: "Задача 3", Lesson: "l2", Subject: "s1"},
			wantSubject: "Матанализ", wantSource: values.SourceUser, wantLesson: "l2",
		},
		{name: "no subject, no lesson", in: NewAssignment{Title: "Купить тетрадь"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCreateEnv(t, testSubjects)
			v, err := e.svc.CreateAssignment(context.Background(), c.in)
			if err != nil {
				t.Fatal(err)
			}
			if v.ID == "" || v.Title != c.in.Title || v.Subject != c.wantSubject || v.SubjectSource != c.wantSource ||
				v.LessonID != c.wantLesson || !v.Due.Equal(c.wantDue) || !v.UserEdited || v.Version != 1 || v.Teacher != "" {
				t.Fatalf("view = %+v", v)
			}

			// Visibility: the list of «Задания», «Сегодня» and list_assignments shows it with its subject.
			list, err := e.svc.ListAssignments(context.Background())
			if err != nil || len(list) != 1 || list[0] != v {
				t.Fatalf("ListAssignments = %+v, %v; want %+v", list, err, v)
			}
		})
	}
}

func TestCreateAssignmentRefusals(t *testing.T) {
	cases := []struct {
		name     string
		subjects SubjectNames
		in       NewAssignment
		want     error
	}{
		{"no title", testSubjects, NewAssignment{Title: " "}, assignment.ErrInvalidTitle},
		{"bidi in the note", testSubjects, NewAssignment{Title: "ok", Note: "a\u202Eb"}, assignment.ErrInvalidNote},
		{"time without a date", testSubjects, NewAssignment{Title: "ok", Time: "09:00"}, assignment.ErrInvalidTime},
		{"bad time", testSubjects, NewAssignment{Title: "ok", Date: "2026-10-15", Time: "24:00"}, assignment.ErrInvalidTime},
		{"bad date", testSubjects, NewAssignment{Title: "ok", Date: "15.10.2026"}, assignment.ErrInvalidDue},
		{"due far ahead", testSubjects, NewAssignment{Title: "ok", Date: "2029-01-01"}, assignment.ErrInvalidDue},
		{"due long ago", testSubjects, NewAssignment{Title: "ok", Date: "2026-09-01"}, assignment.ErrInvalidDue},
		{"unknown subject", testSubjects, NewAssignment{Title: "ok", Subject: "s9"}, ErrSubjectNotFound},
		{"malformed subject", testSubjects, NewAssignment{Title: "ok", Subject: "s 9"}, ErrSubjectNotFound},
		{"unknown lesson", testSubjects, NewAssignment{Title: "ok", Lesson: "l9"}, shared.ErrNotFound},
		{"malformed lesson", testSubjects, NewAssignment{Title: "ok", Lesson: "l 9"}, shared.ErrNotFound},
		{"subjects not wired", nil, NewAssignment{Title: "ok", Subject: "s1"}, ErrInvalidConfig},
		{"library fails", fakeSubjects{err: errors.New("disk")}, NewAssignment{Title: "ok", Subject: "s1"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCreateEnv(t, c.subjects)
			_, err := e.svc.CreateAssignment(context.Background(), c.in)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if errors.Is(err, ErrSubjectNotFound) && errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("an unknown subject reads as an unknown lesson: %v", err)
			}
			if n := e.records(); n != 0 {
				t.Fatalf("%d records written", n)
			}
		})
	}
}

// The same ID again is the same assignment: one row, no second write; other content under it is a duplicate.
func TestCreateAssignmentIsIdempotentByID(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	in := NewAssignment{ID: "ag-1", Title: "Решить номер 5", Note: "из задачника", Subject: "s1", Date: "2026-10-30", Time: "18:00"}

	first, err := e.svc.CreateAssignment(ctx, in)
	if err != nil || first.ID != "ag-1" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	saves := e.saves
	e.clock.Advance(time.Minute)
	again, err := e.svc.CreateAssignment(ctx, in)
	if err != nil || again != first || e.saves != saves || e.records() != 1 {
		t.Fatalf("again = %+v, %v (saves %d -> %d, records %d)", again, err, saves, e.saves, e.records())
	}

	other := in
	other.Title = "Решить номер 6"
	if _, err := e.svc.CreateAssignment(ctx, other); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("other content: err = %v", err)
	}

	// An extracted assignment under that ID.
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	extractedID := e.assignmentsOf("t1")[0].ID
	if _, err := e.svc.CreateAssignment(ctx, NewAssignment{ID: extractedID, Title: "Read chapter 4"}); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("extracted id: err = %v", err)
	}
}

func TestRemoveCreatedAssignment(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	v, err := e.svc.CreateAssignment(ctx, NewAssignment{ID: "ag-1", Title: "Задание"})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, v.Version+1); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("other version: err = %v", err)
	}
	e.inUse[v.ID] = true
	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, v.Version); !errors.Is(err, assignment.ErrInUse) {
		t.Fatalf("in use: err = %v", err)
	}
	e.inUse[v.ID] = false
	if _, err := e.svc.Assignment(ctx, v.ID); err != nil {
		t.Fatalf("refused removals removed it: %v", err)
	}

	// An edit by the student since moves the version on.
	edited, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "Задание 2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, v.Version); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("after an edit: err = %v", err)
	}

	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, edited.Version); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := e.svc.Assignment(ctx, v.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("after the removal: err = %v", err)
	}
	if err := e.svc.RemoveCreatedAssignment(ctx, v.ID, edited.Version); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("already removed: err = %v", err)
	}
}

func TestRemoveCreatedAssignmentKeepsAnExtractedOne(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	st := e.assignmentsOf("t1")[0]
	if err := e.svc.RemoveCreatedAssignment(context.Background(), st.ID, st.Version); !errors.Is(err, ErrNotByStudent) {
		t.Fatalf("err = %v", err)
	}
	if len(e.assignmentsOf("t1")) == 0 {
		t.Fatal("the extracted assignment was removed")
	}
}
