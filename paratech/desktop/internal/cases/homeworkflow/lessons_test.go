package homeworkflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// keptStart is the scheduled start of the lesson a recording kept (12:00 MSK on 13.10).
var keptStart = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

// hintsAnswer is an extraction answer that names a subject and a teacher heard in the transcript.
const hintsAnswer = `{"subject_hint":"Алгебра","teacher_hint":"Петров","assignments":[` +
	`{"title":"Read chapter 4","description":"Pages 40-52","due":"2026-10-16T23:59:00+03:00"}]}`

// keepSnapshot gives recording r1 of the post-processing fakes a lesson snapshot.
func keepSnapshot(t *testing.T, e *ppEnv, spec recvalues.LessonSnapshotSpec) {
	t.Helper()
	r, err := recording.New(recording.Spec{
		ID: "r1", LessonID: "l1", MaxDuration: 3 * time.Hour, Lesson: recvalues.NewLessonSnapshot(spec),
	}, lessonStart.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	e.recordings["r1"] = r
}

// setLiveLesson gives the live lesson l1 a subject, kind and teacher.
func setLiveLesson(t *testing.T, e *ppEnv, subject string, kind lesson.Kind, teacher string) {
	t.Helper()
	l := e.lessons["l1"]
	c := l.Content()
	c.Subject, c.Kind, c.Teacher = subject, kind, teacher
	if _, err := l.ChangeContent(c, ppT0); err != nil {
		t.Fatal(err)
	}
}

// lessonEnv is the Service over the store of the post-processing fakes; out holds its side-effect fakes.
type lessonEnv struct {
	*editEnv
	out *env
}

// newLessonEnv runs one extraction of t1 with answer after setup and returns the Service over the same store.
func newLessonEnv(t *testing.T, setup func(*ppEnv), answer string) *lessonEnv {
	t.Helper()
	pe := newPPEnv(t)
	if setup != nil {
		setup(pe)
	}
	pe.llm.Answer(values.OperationExtractHomework, answer)
	if err := pe.handle(pe.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	e.recipient = "teacher@school.example"
	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: ppAssignments{pe.ppStore},
		AssignmentList: ppAssignmentList{pe.ppStore}, Artifacts: e.files,
		Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Recordings: ppTranscriptRecordings{pe.ppStore}, Lessons: ppLessons{pe.ppStore},
		Clock: pe.clock, IDs: ids{e.store}, Tx: tx{e.store},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &lessonEnv{editEnv: &editEnv{ppEnv: pe, svc: svc}, out: e}
}

func TestAssignmentShowsTheLesson(t *testing.T) {
	full := recvalues.LessonSnapshotSpec{
		Title: "Матанализ (лекция)", Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов И. И.",
		Start: keptStart, End: keptStart.Add(90 * time.Minute),
	}
	noTeacher := full
	noTeacher.Teacher = ""
	recorded := lessonStart.Add(2 * time.Minute)

	cases := []struct {
		name       string
		setup      func(*ppEnv)
		answer     string
		subject    string
		subjectSrc values.NameSource
		kind       string
		teacher    string
		teacherSrc values.NameSource
		date       time.Time
	}{
		{"the snapshot wins over hints", func(e *ppEnv) { keepSnapshot(t, e, full) }, hintsAnswer,
			"Матанализ", values.SourceSchedule, "lecture", "Иванов И. И.", values.SourceSchedule, keptStart},
		{"a hint fills only what the snapshot lacks", func(e *ppEnv) { keepSnapshot(t, e, noTeacher) }, hintsAnswer,
			"Матанализ", values.SourceSchedule, "lecture", "Петров", values.SourceTranscript, keptStart},
		{"the snapshot over a changed live lesson", func(e *ppEnv) {
			keepSnapshot(t, e, full)
			setLiveLesson(t, e, "География", lesson.KindSeminar, "Сидоров")
		}, extractAnswer, "Матанализ", values.SourceSchedule, "lecture", "Иванов И. И.", values.SourceSchedule, keptStart},
		{"no snapshot: the live lesson", func(e *ppEnv) { setLiveLesson(t, e, "География", lesson.KindSeminar, "Сидоров") },
			hintsAnswer, "География", values.SourceSchedule, "seminar", "Сидоров", values.SourceSchedule, lessonStart},
		{"no schedule: the hints and the recording date", func(e *ppEnv) { delete(e.lessons, "l1") }, hintsAnswer,
			"Алгебра", values.SourceTranscript, "", "Петров", values.SourceTranscript, recorded},
		{"nothing known", func(e *ppEnv) { delete(e.lessons, "l1") }, extractAnswer,
			"", values.SourceNone, "", "", values.SourceNone, recorded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newLessonEnv(t, tc.setup, tc.answer)
			list, err := e.svc.ListAssignments(context.Background())
			if err != nil || len(list) == 0 {
				t.Fatalf("ListAssignments = %v, %v", list, err)
			}
			one, err := e.svc.Assignment(context.Background(), list[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []AssignmentView{list[0], one} {
				if v.Subject != tc.subject || v.SubjectSource != tc.subjectSrc || v.Kind != tc.kind ||
					v.Teacher != tc.teacher || v.TeacherSource != tc.teacherSrc {
					t.Fatalf("view = %q (%s) %q %q (%s)", v.Subject, v.SubjectSource, v.Kind, v.Teacher, v.TeacherSource)
				}
				if !v.LessonDate.Equal(tc.date) || v.LessonDate.Location() != e.clock.Now().Location() {
					t.Fatalf("lesson date = %v, want %v in the clock's zone", v.LessonDate, tc.date)
				}
			}
		})
	}
}

func TestCorrectLesson(t *testing.T) {
	e := newLessonEnv(t, func(e *ppEnv) {
		keepSnapshot(t, e, recvalues.LessonSnapshotSpec{Title: "Матанализ", Subject: "Матанализ", Start: keptStart})
	}, hintsAnswer)
	ctx := context.Background()
	id := e.first().ID
	e.clock.Advance(time.Hour)

	v, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: id, Subject: "  Геометрия\r\n", Teacher: "Сидоров"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Subject != "Геометрия" || v.SubjectSource != values.SourceUser || v.Teacher != "Сидоров" ||
		v.TeacherSource != values.SourceUser || v.CorrectedSubject != "Геометрия" || v.Version != 2 || v.UserEdited {
		t.Fatalf("corrected view = %+v", v)
	}
	if st := e.first(); st.UserSubject != "Геометрия" || st.UserTeacher != "Сидоров" || st.HintTeacher != "Петров" {
		t.Fatalf("stored = %+v", st)
	}

	// Clearing the subject shows the schedule again, clearing the teacher the hint.
	v, err = e.svc.CorrectLesson(ctx, LessonCorrection{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if v.Subject != "Матанализ" || v.SubjectSource != values.SourceSchedule || v.Teacher != "Петров" ||
		v.TeacherSource != values.SourceTranscript || v.CorrectedSubject != "" {
		t.Fatalf("cleared view = %+v", v)
	}

	// The same correction again writes nothing.
	before := e.first().Version
	if _, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: id}); err != nil || e.first().Version != before {
		t.Fatalf("unchanged correction: %v, version %d -> %d", err, before, e.first().Version)
	}

	if _, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: "a-missing", Subject: "x"}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}
}

// Hints are not user data: a new extraction replaces them even for an assignment the user edited, whose details stay.
func TestReextractRefreshesHints(t *testing.T) {
	e := newLessonEnv(t, nil, hintsAnswer)
	ctx := context.Background()
	id := e.first().ID
	if _, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: id, Title: "My title"}); err != nil {
		t.Fatal(err)
	}

	e.llm.Answer(values.OperationExtractHomework, `{"subject_hint":"Физика","teacher_hint":"","assignments":[`+
		`{"title":"Other","description":"","due":null}]}`)
	if err := e.handle(e.snapshot("j2", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	st := e.first()
	if st.Title != "My title" || !st.UserEdited || st.HintSubject != "Физика" || st.HintTeacher != "" {
		t.Fatalf("after reextraction: %+v", st)
	}
}

// The confirmation dialog shows the subject the email gets, and the deadline event names the lesson (TASK-092).
func TestSendSubjectAndDeadlineLesson(t *testing.T) {
	e := newLessonEnv(t, func(e *ppEnv) {
		keepSnapshot(t, e, recvalues.LessonSnapshotSpec{
			Title: "Матанализ", Subject: "Матанализ", Kind: "lecture", Start: keptStart,
		})
	}, hintsAnswer)
	ctx := context.Background()
	id := e.first().ID
	ref, err := proposal.ExportRef(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.out.files.Write(ctx, ref, strings.NewReader("my homework")); err != nil {
		t.Fatal(err)
	}

	const want = "Матанализ: домашнее задание к 16.10"
	v, err := e.svc.ProposeSend(ctx, id, proposal.FormatText)
	if err != nil || v.Status != proposal.StatusValidated || v.Subject != want {
		t.Fatalf("ProposeSend = %+v, %v; want subject %q", v, err, want)
	}
	if got, err := e.svc.Get(ctx, v.ID); err != nil || got.Subject != want {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, proposal.FormatText); err != nil {
		t.Fatal(err)
	}
	if calls := e.out.mailer.Calls(); len(calls) != 1 || calls[0].Subject() != want {
		t.Fatalf("mailer calls = %+v", calls)
	}

	due := ppT0.Add(72 * time.Hour).UTC().Format(time.RFC3339)
	p, err := e.svc.Propose(ctx, values.NewUntrustedOutput(`{"kind":"create_deadline_event","assignment_id":"`+
		string(id)+`","title":"Read chapter 4","due":"`+due+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "" {
		t.Fatalf("a deadline proposal has an email subject %q", p.Subject)
	}
	if _, err := e.svc.Confirm(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Execute(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	calls := e.out.calendar.Calls()
	if len(calls) != 1 {
		t.Fatalf("calendar calls = %d", len(calls))
	}
	l := calls[0].Lesson()
	if l.Subject() != "Матанализ" || l.SubjectSource() != values.SourceSchedule || l.Kind() != "lecture" ||
		l.Teacher() != "Петров" || l.TeacherSource() != values.SourceTranscript || !l.Date().Equal(keptStart) {
		t.Fatalf("event lesson = %+v", l)
	}
}

// TASK-088 medium: a draft names the lesson the recording kept, not the live schedule, with its subject and teacher.
func TestDraftUsesTheRecordedLesson(t *testing.T) {
	e := newDraftEnv(t)
	keepSnapshot(t, e.ppEnv, recvalues.LessonSnapshotSpec{
		Title: strings.Repeat("Л", 250), Subject: "Матанализ", Teacher: "Иванов", Start: keptStart,
	})
	e.llm.Answer(values.OperationHomeworkDraft, answer("Chapter 4", "Text."))
	if _, err := e.generate(""); err != nil {
		t.Fatal(err)
	}
	data := e.llm.RequestsFor(values.OperationHomeworkDraft)[0].UntrustedData()
	for _, want := range []string{
		`"lesson_title":"` + strings.Repeat("Л", values.MaxNameRunes) + `"`, `"lesson_subject":"Матанализ"`,
		`"lesson_teacher":"Иванов"`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("request lacks %s: %s", want, data)
		}
	}
	if strings.Contains(data, "Geography") {
		t.Fatalf("the live lesson reached the request: %s", data)
	}
}

// Without a snapshot the live lesson names the draft; a correction of the user is the subject sent.
func TestDraftFallsBackToTheLiveLesson(t *testing.T) {
	e := newDraftEnv(t)
	setLiveLesson(t, e.ppEnv, "География", lesson.KindSeminar, "Сидоров")
	st := e.assignments["a1"]
	st.UserSubject = "Физическая география"
	e.assignments["a1"] = st

	e.llm.Answer(values.OperationHomeworkDraft, answer("Chapter 4", "Text."))
	if _, err := e.generate(""); err != nil {
		t.Fatal(err)
	}
	data := e.llm.RequestsFor(values.OperationHomeworkDraft)[0].UntrustedData()
	for _, want := range []string{`"lesson_title":"Geography"`, `"lesson_subject":"Физическая география"`, `"lesson_teacher":"Сидоров"`} {
		if !strings.Contains(data, want) {
			t.Errorf("request lacks %s: %s", want, data)
		}
	}
}

// A subject change between the dialog and the click makes the send STALE: no email with a subject the user did not
// see (hard rule 3).
func TestSendSubjectChangedAfterTheDialog(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		change func(t *testing.T, e *lessonEnv, id shared.AssignmentID)
	}{
		{"user correction", func(t *testing.T, e *lessonEnv, id shared.AssignmentID) {
			if _, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: id, Subject: "Геометрия"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"new extraction", func(t *testing.T, e *lessonEnv, _ shared.AssignmentID) {
			e.llm.Answer(values.OperationExtractHomework, `{"subject_hint":"Физика","teacher_hint":"","assignments":[`+
				`{"title":"Read chapter 4","description":"Pages 40-52","due":"2026-10-16T23:59:00+03:00"}]}`)
			if err := e.handle(e.snapshot("j2", ExtractHomeworkJobType, "t1")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newLessonEnv(t, nil, hintsAnswer)
			id := e.first().ID
			ref, err := proposal.ExportRef(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.out.files.Write(ctx, ref, strings.NewReader("my homework")); err != nil {
				t.Fatal(err)
			}
			v, err := e.svc.ProposeSend(ctx, id, proposal.FormatText)
			if err != nil || v.Status != proposal.StatusValidated {
				t.Fatalf("ProposeSend = %+v, %v", v, err)
			}

			tc.change(t, e, id)
			if now, err := e.svc.emailSubject(ctx, id); err != nil || now == v.Subject {
				t.Fatalf("subject after the change = %q, %v; want other than %q", now, err, v.Subject)
			}

			if _, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, proposal.FormatText); !errors.Is(err, proposal.ErrStale) {
				t.Fatalf("ConfirmSend = %v, want ErrStale", err)
			}
			if got, err := e.svc.Get(ctx, v.ID); err != nil || got.Status != proposal.StatusStale ||
				got.Reason != proposal.ReasonSubjectChanged {
				t.Fatalf("proposal = %+v, %v; want STALE %s", got, err, proposal.ReasonSubjectChanged)
			}
			if n := len(e.out.mailer.Calls()); n != 0 {
				t.Fatalf("mailer calls = %d, want none", n)
			}
		})
	}
}
