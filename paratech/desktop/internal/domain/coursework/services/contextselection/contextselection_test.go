package contextselection

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

var (
	lessonStart = time.Date(2026, 10, 14, 10, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	nextStart   = lessonStart.Add(7 * 24 * time.Hour)
)

// full returns material with every field set, so each test can see what an operation drops.
func full() Material {
	return Material{
		Lesson: Lesson{
			Title: "  Linear algebra  ", Start: lessonStart, Subject: " Mathematics ", Teacher: " Ivanova ", Kind: values.LessonKindSeminar,
			NextStart: nextStart,
		},
		Transcript:     []Segment{{StartMs: 0, EndMs: 900, Text: " Hello. "}, {StartMs: 900, EndMs: 950, Text: "  "}, {StartMs: 1000, EndMs: 2000, Text: "Solve 1-5."}},
		Summary:        "SUMMARY-TEXT",
		Assignment:     Assignment{Title: "Exercises 1-5", Description: "From chapter 2.", Due: lessonStart.Add(48 * time.Hour)},
		PreviousDraft:  "PREVIOUS-DRAFT",
		StudentRequest: "make it shorter",
	}
}

func TestSelectKeepsOnlyWhatTheOperationUses(t *testing.T) {
	cases := []struct {
		op   values.Operation
		want Context
	}{
		{values.OperationSummary, Context{
			operation: values.OperationSummary, lessonTitle: "Linear algebra", lessonSubject: "Mathematics",
			lessonTeacher: "Ivanova", lessonKind: values.LessonKindSeminar, lessonStart: lessonStart,
			nextLessonStart: nextStart, transcript: "Hello.\nSolve 1-5.",
		}},
		{values.OperationExtractHomework, Context{
			operation: values.OperationExtractHomework, lessonTitle: "Linear algebra", lessonSubject: "Mathematics",
			lessonTeacher: "Ivanova", lessonKind: values.LessonKindSeminar, lessonStart: lessonStart,
			nextLessonStart: nextStart, transcript: "Hello.\nSolve 1-5.",
		}},
		{values.OperationHomeworkDraft, Context{
			operation: values.OperationHomeworkDraft, lessonTitle: "Linear algebra", lessonSubject: "Mathematics",
			lessonTeacher: "Ivanova", summary: "SUMMARY-TEXT",
			assignmentTitle: "Exercises 1-5", assignmentDescription: "From chapter 2.", previousDraft: "PREVIOUS-DRAFT",
			studentRequest: "make it shorter",
		}},
		{values.OperationLectureText, Context{
			operation: values.OperationLectureText, lessonTitle: "Linear algebra", lessonSubject: "Mathematics",
			lessonTeacher: "Ivanova", lessonKind: values.LessonKindSeminar, lessonStart: lessonStart,
			nextLessonStart: nextStart, transcript: "Hello.\nSolve 1-5.",
		}},
	}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			got, err := Select(tc.op, full())
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Select =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestSelectRejects(t *testing.T) {
	with := func(f func(*Material)) Material {
		m := full()
		f(&m)
		return m
	}
	cases := []struct {
		name string
		op   values.Operation
		m    Material
		want error
	}{
		{"unknown operation", values.Operation("send_email"), full(), ErrUnknownOperation},
		{"summary without transcript", values.OperationSummary, with(func(m *Material) { m.Transcript = nil }), ErrMissingContext},
		{"summary with blank transcript", values.OperationSummary, with(func(m *Material) { m.Transcript = []Segment{{Text: " "}} }), ErrMissingContext},
		{"extract without lesson start", values.OperationExtractHomework, with(func(m *Material) { m.Lesson.Start = time.Time{} }), ErrMissingContext},
		{"draft without assignment", values.OperationHomeworkDraft, with(func(m *Material) { m.Assignment = Assignment{} }), ErrMissingContext},
		{"segment with newline", values.OperationSummary, with(func(m *Material) { m.Transcript[0].Text = "a\nb" }), ErrInvalidMaterial},
		{"segment invalid utf8", values.OperationExtractHomework, with(func(m *Material) { m.Transcript[0].Text = "\xff" }), ErrInvalidMaterial},
		{"transcript too large", values.OperationSummary, with(func(m *Material) {
			m.Transcript = []Segment{{Text: strings.Repeat("a", MaxTranscriptBytes/2)}, {Text: strings.Repeat("b", MaxTranscriptBytes/2)}}
		}), ErrInvalidMaterial},
		{"title with line separator", values.OperationSummary, with(func(m *Material) { m.Lesson.Title = "a\u2028b" }), ErrInvalidMaterial},
		{"segment with paragraph separator", values.OperationSummary, with(func(m *Material) { m.Transcript[0].Text = "a\u2029b" }), ErrInvalidMaterial},
		{"assignment title with line separator", values.OperationHomeworkDraft, with(func(m *Material) { m.Assignment.Title = "a\u2028b" }), ErrInvalidMaterial},
		{"title with control", values.OperationSummary, with(func(m *Material) { m.Lesson.Title = "a\tb" }), ErrInvalidMaterial},
		{"title too long", values.OperationHomeworkDraft, with(func(m *Material) { m.Lesson.Title = strings.Repeat("я", MaxTitleRunes+1) }), ErrInvalidMaterial},
		{"assignment title with newline", values.OperationHomeworkDraft, with(func(m *Material) { m.Assignment.Title = "a\nb" }), ErrInvalidMaterial},
		{"lecture text without transcript", values.OperationLectureText, with(func(m *Material) { m.Transcript = nil }), ErrMissingContext},
		{"lecture text subject with newline", values.OperationLectureText, with(func(m *Material) { m.Lesson.Subject = "a\nb" }), ErrInvalidMaterial},
		{"lecture text unknown kind", values.OperationLectureText, with(func(m *Material) { m.Lesson.Kind = "exam" }), ErrInvalidMaterial},
		{"summary unknown kind", values.OperationSummary, with(func(m *Material) { m.Lesson.Kind = "exam" }), ErrInvalidMaterial},
		{"extract teacher with newline", values.OperationExtractHomework, with(func(m *Material) { m.Lesson.Teacher = "a\nb" }), ErrInvalidMaterial},
		{"summary subject with line separator", values.OperationSummary, with(func(m *Material) { m.Lesson.Subject = "a b" }), ErrInvalidMaterial},
		{"next lesson at the lesson start", values.OperationExtractHomework, with(func(m *Material) { m.Lesson.NextStart = lessonStart }), ErrInvalidMaterial},
		{"next lesson before the lesson", values.OperationLectureText, with(func(m *Material) { m.Lesson.NextStart = lessonStart.Add(-time.Hour) }), ErrInvalidMaterial},
		{"description with NUL", values.OperationHomeworkDraft, with(func(m *Material) { m.Assignment.Description = "a\x00b" }), ErrInvalidMaterial},
		{"request too large", values.OperationHomeworkDraft, with(func(m *Material) { m.StudentRequest = strings.Repeat("x", MaxStudentRequestBytes+1) }), ErrInvalidMaterial},
		{"summary invalid utf8", values.OperationHomeworkDraft, with(func(m *Material) { m.Summary = "\xff" }), ErrInvalidMaterial},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Select(tc.op, tc.m)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if !c.IsZero() {
				t.Fatalf("a refused selection returned a non-zero context")
			}
		})
	}
}

func TestSelectTranscriptSizeBoundary(t *testing.T) {
	// Two segments joined by one newline.
	at := []Segment{{Text: strings.Repeat("a", 1000)}, {Text: strings.Repeat("b", MaxTranscriptBytes-1001)}}
	c, err := Select(values.OperationSummary, Material{Transcript: at})
	if err != nil {
		t.Fatalf("transcript of exactly %d bytes: %v", MaxTranscriptBytes, err)
	}
	if len(c.Transcript()) != MaxTranscriptBytes {
		t.Fatalf("transcript is %d bytes, want %d", len(c.Transcript()), MaxTranscriptBytes)
	}

	over := []Segment{at[0], {Text: at[1].Text + "b"}}
	if _, err := Select(values.OperationSummary, Material{Transcript: over}); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("transcript of %d bytes: error = %v, want ErrInvalidMaterial", MaxTranscriptBytes+1, err)
	}
}

func TestSelectIgnoresFieldsTheOperationDropsEvenWhenInvalid(t *testing.T) {
	m := full()
	m.Summary = "\xff"
	m.PreviousDraft = "\x00"
	m.Assignment.Title = "a\nb"
	if _, err := Select(values.OperationSummary, m); err != nil {
		t.Fatalf("summary validated a field it does not send: %v", err)
	}

	m = full()
	m.Transcript = []Segment{{Text: "\xff"}}
	if _, err := Select(values.OperationHomeworkDraft, m); err != nil {
		t.Fatalf("draft validated the transcript it does not send: %v", err)
	}
}

func TestSelectDraftWithDescriptionOnly(t *testing.T) {
	m := Material{Assignment: Assignment{Description: "Read chapter 3."}}
	c, err := Select(values.OperationHomeworkDraft, m)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if c.AssignmentDescription() != "Read chapter 3." || c.AssignmentTitle() != "" || c.Operation() != values.OperationHomeworkDraft {
		t.Fatalf("unexpected context %+v", c)
	}
}

// TASK-092 sent the subject and teacher to the draft; TASK-093 sends them with every operation on the lesson.
func TestSubjectAndTeacherGoToEveryLessonOperation(t *testing.T) {
	start := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	m := Material{
		Lesson:     Lesson{Title: "L", Start: start, Subject: " Физика ", Teacher: "Петров"},
		Transcript: []Segment{{Text: "x"}},
		Assignment: Assignment{Title: "T"},
	}
	for _, op := range []values.Operation{
		values.OperationHomeworkDraft, values.OperationSummary, values.OperationExtractHomework, values.OperationLectureText,
	} {
		c, err := Select(op, m)
		if err != nil || c.LessonSubject() != "Физика" || c.LessonTeacher() != "Петров" {
			t.Fatalf("%s: %q %q %v", op, c.LessonSubject(), c.LessonTeacher(), err)
		}
		for _, bad := range []Lesson{{Subject: "a\nb", Start: start}, {Teacher: strings.Repeat("я", MaxTitleRunes+1), Start: start}} {
			b := m
			b.Lesson = bad
			if _, err := Select(op, b); !errors.Is(err, ErrInvalidMaterial) {
				t.Fatalf("%s: lesson %+v: error = %v, want ErrInvalidMaterial", op, bad, err)
			}
		}
	}
}

// The next lesson anchors "by the next class"; without the lesson start it means nothing and is dropped.
func TestSelectNextLessonStart(t *testing.T) {
	m := full()
	m.Lesson.Start = time.Time{}
	c, err := Select(values.OperationSummary, m)
	if err != nil || !c.NextLessonStart().IsZero() || !c.LessonStart().IsZero() {
		t.Fatalf("summary without a start: next %v, err %v", c.NextLessonStart(), err)
	}
	c, err = Select(values.OperationExtractHomework, full())
	if err != nil || !c.NextLessonStart().Equal(nextStart) {
		t.Fatalf("extract: next %v, err %v", c.NextLessonStart(), err)
	}
	if c, err := Select(values.OperationHomeworkDraft, full()); err != nil || !c.NextLessonStart().IsZero() || !c.LessonStart().IsZero() {
		t.Fatalf("draft keeps the lesson times: %v %v %v", c.LessonStart(), c.NextLessonStart(), err)
	}
}

// TASK-094: the student's instructions reach their own prompt section without bidi controls.
func TestSelectStripsBidiFromTheStudentRequest(t *testing.T) {
	m := full()
	m.StudentRequest = " \u202emake it shorter\u202c "
	c, err := Select(values.OperationHomeworkDraft, m)
	if err != nil || c.StudentRequest() != "make it shorter" {
		t.Fatalf("Select = %q, %v", c.StudentRequest(), err)
	}
}
