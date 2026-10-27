package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var mentionT0 = time.Date(2026, 10, 20, 21, 30, 0, 0, time.UTC) // 21.10 in Moscow

type mentionClock struct{}

func (mentionClock) Now() time.Time { return mentionT0.In(time.FixedZone("MSK", 3*3600)) }

type mentionLibrary struct {
	subjects  []*subject.Subject
	materials map[shared.SubjectID][]*material.Material
	err       error
}

func (l *mentionLibrary) Subjects(context.Context) ([]*subject.Subject, error) {
	return l.subjects, l.err
}

func (l *mentionLibrary) Materials(_ context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	return l.materials[sub], l.err
}

type mentionLectures struct {
	groups  []kb.Subject
	lessons map[string][]kb.Lesson
	err     error
}

func (l *mentionLectures) Subjects(context.Context) ([]kb.Subject, error) { return l.groups, l.err }

func (l *mentionLectures) Lectures(_ context.Context, key string) ([]kb.Lesson, error) {
	return l.lessons[key], l.err
}

type mentionAssignments struct {
	list []homeworkflow.AssignmentView
	err  error
}

func (a *mentionAssignments) ListAssignments(context.Context) ([]homeworkflow.AssignmentView, error) {
	return a.list, a.err
}

// mentionExists answers false for the lessons in gone and counts the checks.
type mentionExists struct {
	gone   map[string]bool
	checks int
	err    error
}

func (e *mentionExists) Exists(_ context.Context, ref values.Ref) (bool, error) {
	e.checks++
	return !e.gone[ref.ID()], e.err
}

type mentionFixture struct {
	lib      *mentionLibrary
	lectures *mentionLectures
	tasks    *mentionAssignments
	exists   *mentionExists
}

func newMentionFixture() *mentionFixture {
	return &mentionFixture{
		lib:      &mentionLibrary{materials: map[shared.SubjectID][]*material.Material{}},
		lectures: &mentionLectures{lessons: map[string][]kb.Lesson{}},
		tasks:    &mentionAssignments{},
		exists:   &mentionExists{gone: map[string]bool{}},
	}
}

func (f *mentionFixture) service(t *testing.T) (*AgentService, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	s := NewAgentService(AgentCore{
		Driver: &fakeAgentDriver{}, Link: readyLink(), Panel: NewAgentPanel(discard()), Life: context.Background(),
		Journal: &fakeJournal{}, Clock: fixedClock{}, Chats: &fakeChats{},
		Mentions: AgentMentionSources{
			Library: f.lib, Lectures: f.lectures, Assignments: f.tasks, Exists: f.exists, Local: mentionClock{},
		},
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	return s, &logs
}

func (f *mentionFixture) subject(t *testing.T, id shared.SubjectID, name string, at time.Time, aliases ...string) {
	t.Helper()
	s, err := subject.New(subject.Spec{ID: id, Name: name, Aliases: aliases, Semester: libvalues.SemesterOf(at)}, at)
	if err != nil {
		t.Fatal(err)
	}
	f.lib.subjects = append(f.lib.subjects, s)
}

// material files a material in sub ("" is «Входящие»); removed puts it in the bin.
func (f *mentionFixture) material(t *testing.T, id shared.MaterialID, sub shared.SubjectID, name string, at time.Time, removed bool) {
	t.Helper()
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypePDF, Name: name}, at)
	if err != nil {
		t.Fatal(err)
	}
	if sub != "" {
		if _, err := m.Place(sub, "f1", at); err != nil {
			t.Fatal(err)
		}
	}
	if removed {
		if _, err := m.Remove(at); err != nil {
			t.Fatal(err)
		}
	}
	f.lib.materials[sub] = append(f.lib.materials[sub], m)
}

func (f *mentionFixture) lecture(key string, l kb.Lesson) {
	if _, ok := f.lectures.lessons[key]; !ok {
		f.lectures.groups = append(f.lectures.groups, kb.Subject{Key: key})
	}
	f.lectures.lessons[key] = append(f.lectures.lessons[key], l)
}

func kinds(cs []AgentMentionCandidate) string {
	var b []string
	for _, c := range cs {
		b = append(b, c.Kind+":"+c.ID)
	}
	return strings.Join(b, " ")
}

// One object of each kind is found by its name, with its subject and date as the hint.
func TestMentionCandidatesEachKind(t *testing.T) {
	f := newMentionFixture()
	f.subject(t, "s1", "Физика", mentionT0.Add(-4*time.Hour))
	f.subject(t, "s2", "История", mentionT0)
	f.material(t, "m1", "s1", "Физика конспект.pdf", mentionT0.Add(-3*time.Hour), false)
	f.material(t, "m2", "", "Физика из почты.pdf", mentionT0.Add(-5*time.Hour), false)
	f.lecture("ID:s1", kb.Lesson{
		Key: "recording:r1", Recording: "r1", ScheduleLesson: "l1", StartedAt: mentionT0.Add(-2 * time.Hour),
		Title: "Физика: волны", Subject: "физика", Library: kb.LibrarySubject{ID: "s1", Name: "Физика"},
	})
	f.tasks.list = []homeworkflow.AssignmentView{
		{ID: "a1", Title: "Физика: задачи 1–5", Subject: "Физика", Due: mentionT0.Add(48 * time.Hour), UpdatedAt: mentionT0.Add(-time.Hour)},
		{ID: "a2", Title: "Физика: реферат", Subject: "Физика", LessonDate: mentionT0.Add(-30 * time.Hour), UpdatedAt: mentionT0.Add(-6 * time.Hour)},
	}
	s, _ := f.service(t)

	got := s.MentionCandidates(t.Context(), "физика")
	want := []AgentMentionCandidate{
		{Kind: "assignment", ID: "a1", Label: "Физика: задачи 1–5", Hint: "Физика · до 23.10.2026"},
		{Kind: "lesson", ID: "l1", Label: "Физика: волны", Hint: "Физика · 20.10.2026"},
		{Kind: "material", ID: "m1", Label: "Физика конспект.pdf", Hint: "Физика · 20.10.2026"},
		{Kind: "subject", ID: "s1", Label: "Физика", Hint: ""},
		{Kind: "material", ID: "m2", Label: "Физика из почты.pdf", Hint: "Входящие · 20.10.2026"},
		{Kind: "assignment", ID: "a2", Label: "Физика: реферат", Hint: "Физика · 19.10.2026"},
	}
	if got.Problem != "" || fmt.Sprint(got.Candidates) != fmt.Sprint(want) {
		t.Fatalf("MentionCandidates =\n%+v\nwant\n%+v", got, want)
	}
	for _, c := range got.Candidates {
		ref, err := values.NewRef(values.IDKind(c.Kind), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := values.NewMention(ref, c.Label); err != nil {
			t.Errorf("%+v cannot go back in an Ask: %v", c, err)
		}
	}
}

// The match ignores case, also for Cyrillic, and a subject is found by its alias.
func TestMentionCandidatesIgnoreCase(t *testing.T) {
	f := newMentionFixture()
	f.subject(t, "s1", "Математический анализ", mentionT0, "Матан")
	f.tasks.list = []homeworkflow.AssignmentView{{ID: "a1", Title: "ДЗ по МАТАНУ", UpdatedAt: mentionT0}}
	s, _ := f.service(t)

	for _, q := range []string{"матан", "МАТАН", "  МаТаН  "} {
		if got := s.MentionCandidates(t.Context(), q); kinds(got.Candidates) != "assignment:a1 subject:s1" { // equal activity: by kind

			t.Errorf("%q = %+v", q, got)
		}
	}
	if got := s.MentionCandidates(t.Context(), "анализ"); kinds(got.Candidates) != "subject:s1" {
		t.Errorf("by name = %+v", got)
	}
	if got := s.MentionCandidates(t.Context(), "химия"); got.Problem != "" || got.Candidates == nil || len(got.Candidates) != 0 {
		t.Errorf("no match = %+v, want an empty list", got)
	}
}

// At most 20 candidates come back, the most recently active ones; an empty query lists every kind by activity.
func TestMentionCandidatesLimitAndOrder(t *testing.T) {
	f := newMentionFixture()
	for i := range 30 {
		f.tasks.list = append(f.tasks.list, homeworkflow.AssignmentView{
			ID: shared.AssignmentID(fmt.Sprintf("a%02d", i)), Title: "Задача", UpdatedAt: mentionT0.Add(-time.Duration(i) * time.Hour),
		})
	}
	s, _ := f.service(t)
	got := s.MentionCandidates(t.Context(), "")
	if len(got.Candidates) != MaxMentionCandidates || got.Candidates[0].ID != "a00" || got.Candidates[19].ID != "a19" {
		t.Fatalf("limit = %d: %s", len(got.Candidates), kinds(got.Candidates))
	}

	f = newMentionFixture()
	f.subject(t, "s1", "Физика", mentionT0.Add(-3*time.Hour))
	f.material(t, "m1", "s1", "Лаба.pdf", mentionT0.Add(-time.Hour), false)
	f.lecture("ID:s1", kb.Lesson{Key: "lesson:l1", ScheduleLesson: "l1", StartedAt: mentionT0, Title: "Лекция 3"})
	f.tasks.list = []homeworkflow.AssignmentView{{ID: "a1", Title: "Отчёт", UpdatedAt: mentionT0.Add(-2 * time.Hour)}}
	s, _ = f.service(t)
	if got := s.MentionCandidates(t.Context(), ""); kinds(got.Candidates) != "lesson:l1 material:m1 assignment:a1 subject:s1" {
		t.Fatalf("empty query = %s", kinds(got.Candidates))
	}
}

// Deleted assignments, removed materials, deleted schedule lessons and lectures without a schedule lesson are never
// found.
func TestMentionCandidatesSkipGone(t *testing.T) {
	f := newMentionFixture()
	f.subject(t, "s1", "Физика", mentionT0)
	f.material(t, "m1", "s1", "Физика.pdf", mentionT0, true)
	f.material(t, "m2", "", "Физика 2.pdf", mentionT0, true)
	f.lecture("ID:s1", kb.Lesson{Key: "recording:r1", Recording: "r1", ScheduleLesson: "l1", StartedAt: mentionT0, Title: "Физика"})
	f.lecture("ID:s1", kb.Lesson{Key: "recording:r2", Recording: "r2", StartedAt: mentionT0, Title: "Физика ad hoc"})
	f.lecture("физика", kb.Lesson{Key: "note:n1", Title: "Физика конспект"})
	f.exists.gone["l1"] = true
	f.tasks.list = []homeworkflow.AssignmentView{{ID: "a1", Title: "Физика", Deleted: true, UpdatedAt: mentionT0}}
	s, _ := f.service(t)

	if got := s.MentionCandidates(t.Context(), "физика"); kinds(got.Candidates) != "subject:s1" {
		t.Fatalf("MentionCandidates = %+v", got)
	}
	if f.exists.checks != 1 {
		t.Errorf("lesson checks = %d, want 1", f.exists.checks)
	}
}

// A lesson recorded twice is one candidate, at its newest lecture.
func TestMentionCandidatesLessonOnce(t *testing.T) {
	f := newMentionFixture()
	f.lecture("ID:s1", kb.Lesson{Key: "recording:r2", Recording: "r2", ScheduleLesson: "l1", StartedAt: mentionT0, Title: "Волны (2)"})
	f.lecture("ID:s1", kb.Lesson{Key: "recording:r1", Recording: "r1", ScheduleLesson: "l1", StartedAt: mentionT0.Add(-time.Hour), Title: "Волны"})
	f.lecture("волны", kb.Lesson{Key: "recording:r3", Recording: "r3", ScheduleLesson: "l1", StartedAt: mentionT0.Add(-2 * time.Hour), Title: "Волны"})
	s, _ := f.service(t)
	got := s.MentionCandidates(t.Context(), "волны")
	if len(got.Candidates) != 1 || got.Candidates[0].Label != "Волны (2)" {
		t.Fatalf("MentionCandidates = %+v", got)
	}
}

// The page gets ids, names and hints only: no folder of a material name, no other field.
func TestMentionCandidatesNoPaths(t *testing.T) {
	f := newMentionFixture()
	f.material(t, "m1", "", `C:\Users\student\Documents\Физика.pdf`, mentionT0, false)
	f.material(t, "m2", "", "home/student/Физика 2.pdf", mentionT0, false)
	s, _ := f.service(t)
	got := s.MentionCandidates(t.Context(), "физика")
	if len(got.Candidates) != 2 {
		t.Fatalf("MentionCandidates = %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(data), `/\`) || strings.Contains(string(data), "student") {
		t.Errorf("a path reached the page: %s", data)
	}
	var view struct {
		Candidates []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	for _, c := range view.Candidates {
		if len(c) != 4 || c["kind"] == nil || c["id"] == nil || c["label"] == nil || c["hint"] == nil {
			t.Errorf("candidate fields = %v", c)
		}
	}
}

// Names and hints are untrusted: one line without control or format characters, bounded.
func TestMentionCandidatesCleanText(t *testing.T) {
	f := newMentionFixture()
	f.tasks.list = []homeworkflow.AssignmentView{
		{ID: "a1", Title: "Задача\n1\u202e\u200bx\x07y", Subject: "Физ\r\nика\u2028!", UpdatedAt: mentionT0},
		{ID: "a2", Title: "Задача " + strings.Repeat("я", 300), Subject: strings.Repeat("ф", 300), UpdatedAt: mentionT0.Add(-time.Hour)},
		{ID: "a3", Title: "\u202e\n\t", UpdatedAt: mentionT0.Add(-2 * time.Hour)},
	}
	s, _ := f.service(t)
	got := s.MentionCandidates(t.Context(), "")
	if len(got.Candidates) != 3 {
		t.Fatalf("MentionCandidates = %+v", got)
	}
	if c := got.Candidates[0]; c.Label != "Задача 1x y" || c.Hint != "Физ ика !" {
		t.Errorf("cleaned = %+v", c)
	}
	if c := got.Candidates[1]; utf8.RuneCountInString(c.Label) != maxMentionLabelRunes || utf8.RuneCountInString(c.Hint) > maxMentionHintRunes {
		t.Errorf("long label %d runes, hint %d runes", utf8.RuneCountInString(c.Label), utf8.RuneCountInString(c.Hint))
	}
	if c := got.Candidates[2]; c.Label != mentionAssignmentLabel {
		t.Errorf("empty label = %+v", c)
	}
}

// A query is cut to 100 characters; the longer tail does not change the match.
func TestMentionCandidatesQueryCut(t *testing.T) {
	long := strings.Repeat("Я", maxMentionQueryRunes)
	if q := mentionQuery(long + "zzz\n"); q != strings.ToLower(long) {
		t.Fatalf("mentionQuery = %d runes %q", utf8.RuneCountInString(q), q)
	}
	f := newMentionFixture()
	f.tasks.list = []homeworkflow.AssignmentView{{ID: "a1", Title: long, UpdatedAt: mentionT0}}
	s, _ := f.service(t)
	if got := s.MentionCandidates(t.Context(), long+"хвост, которого нет в названии"); kinds(got.Candidates) != "assignment:a1" {
		t.Fatalf("MentionCandidates = %+v", got)
	}
}

// Without its sources the search is unavailable; a failing source or check is logged and failed, never a partial list.
func TestMentionCandidatesProblems(t *testing.T) {
	s, _, _, _ := newAgent(t, readyLink())
	if got := s.MentionCandidates(t.Context(), "x"); got.Problem != AgentProblemUnavailable || got.Candidates == nil {
		t.Fatalf("not wired = %+v", got)
	}

	boom := errors.New("db down")
	cases := map[string]func(f *mentionFixture){
		"library":     func(f *mentionFixture) { f.lib.err = boom },
		"lectures":    func(f *mentionFixture) { f.lectures.err = boom },
		"assignments": func(f *mentionFixture) { f.tasks.err = boom },
		"exists":      func(f *mentionFixture) { f.exists.err = boom },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newMentionFixture()
			f.lecture("k", kb.Lesson{Key: "lesson:l1", ScheduleLesson: "l1", StartedAt: mentionT0, Title: "Лекция"})
			breakIt(f)
			s, logs := f.service(t)
			got := s.MentionCandidates(t.Context(), "")
			if got.Problem != AgentProblemFailed || got.Candidates == nil || len(got.Candidates) != 0 {
				t.Fatalf("MentionCandidates = %+v", got)
			}
			if !strings.Contains(logs.String(), opAgentMentions) {
				t.Errorf("log = %s", logs)
			}
		})
	}
}

// Every source is read at most up to its cap.
func TestMentionCandidatesReadCaps(t *testing.T) {
	f := newMentionFixture()
	for i := range maxMentionReads + 10 {
		f.lecture("k", kb.Lesson{
			Key: fmt.Sprintf("lesson:l%d", i), ScheduleLesson: shared.LessonID(fmt.Sprintf("l%d", i)),
			StartedAt: mentionT0.Add(-time.Duration(i) * time.Minute), Title: "Лекция",
		})
	}
	f.exists.gone = map[string]bool{}
	for i := range maxMentionLessonCheck + 5 {
		f.exists.gone[fmt.Sprintf("l%d", i)] = true
	}
	s, _ := f.service(t)
	got := s.MentionCandidates(t.Context(), "лекция")
	if got.Problem != "" || len(got.Candidates) != 0 || f.exists.checks != maxMentionLessonCheck {
		t.Fatalf("got %d candidates after %d checks", len(got.Candidates), f.exists.checks)
	}
}
