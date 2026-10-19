package contextselection

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// Limits of the selected fields. Together they stay below values.MaxRequestPayloadSize; a transcript above
// MaxTranscriptBytes is refused rather than cut, so that the model never works on a silently truncated lesson.
const (
	MaxTitleRunes          = 200       // lesson and assignment titles, the subject and the teacher
	MaxTranscriptBytes     = 512 << 10 // the joined transcript text
	MaxDescriptionBytes    = 16 << 10  // the assignment description
	MaxSummaryBytes        = 128 << 10 // the lesson summary
	MaxPreviousDraftBytes  = 128 << 10 // the draft being refined
	MaxStudentRequestBytes = 4 << 10   // the student's refinement request
)

// Lesson is what the caller knows about the lesson.
type Lesson struct {
	Title string
	// Start anchors relative dates ("by next Monday"). Pass it in the student's time zone: its UTC offset is the one
	// the model is told to use.
	Start time.Time
	// Subject and Teacher name the lesson as the schedule does; empty when unknown (TASK-092, TASK-093).
	Subject string
	Teacher string
	// Kind is the kind of the lesson, chosen by the app; it selects trusted wording of the summary and lecture_text
	// instructions and is sent as data too (TASK-093).
	Kind values.LessonKind
	// NextStart is the start of the next lesson of the same subject in the schedule, in the zone of Start; zero when
	// unknown. It resolves "by the next class" (TASK-093).
	NextStart time.Time
	// NextKind is the kind of that next lesson, chosen by the app; it resolves "by the next seminar" (TASK-093).
	NextKind values.LessonKind
}

// Assignment is what the caller knows about the assignment a draft is for.
type Assignment struct {
	Title       string
	Description string
	Due         time.Time // never sent: a draft does not need it
}

// Segment is one recognised piece of the transcript. Only the text is ever sent.
type Segment struct {
	StartMs int64
	EndMs   int64
	Text    string
}

// Material is everything the caller has at hand for an operation. Select decides what of it is sent.
type Material struct {
	Lesson         Lesson
	Transcript     []Segment
	Summary        string // the summary of the lesson, if one exists
	Assignment     Assignment
	PreviousDraft  string // the draft the student is refining, if any
	StudentRequest string // the student's additional instructions for a refinement (PRD section 10 step 4)
}

// Context is the minimal context of one operation, the result of Select. Fields the operation does not use are empty.
// The zero value has no operation and is refused by the prompt builder.
type Context struct {
	operation             values.Operation
	lessonTitle           string
	lessonSubject         string
	lessonTeacher         string
	lessonStart           time.Time
	nextLessonStart       time.Time
	nextLessonKind        values.LessonKind
	lessonKind            values.LessonKind
	transcript            string
	summary               string
	assignmentTitle       string
	assignmentDescription string
	previousDraft         string
	studentRequest        string
}

// Select validates the material and keeps only the fields operation uses (see the package comment). Required fields:
//
//   - summary and lecture_text: a non-empty transcript;
//   - extract_homework_deadlines: a non-empty transcript and the lesson start;
//   - homework_draft: an assignment title or description.
//
// The operations on the transcript also keep the lesson context: subject, teacher, kind, start and the start of the
// next lesson of the subject with its kind (dropped without a start; one not after the start is refused).
//
// Texts are trimmed. A missing required field wraps ErrMissingContext, a bad text ErrInvalidMaterial; fields that are
// not selected are not validated either, they are simply dropped.
func Select(operation values.Operation, m Material) (Context, error) {
	if !operation.Valid() {
		return Context{}, fmt.Errorf("%w: %q", ErrUnknownOperation, string(operation))
	}

	c := Context{operation: operation}
	var err error
	if c.lessonTitle, err = oneLine("lesson title", m.Lesson.Title); err != nil {
		return Context{}, err
	}

	switch operation {
	case values.OperationSummary, values.OperationExtractHomework, values.OperationLectureText:
		if c.transcript, err = joinTranscript(m.Transcript); err != nil {
			return Context{}, err
		}
		if c.transcript == "" {
			return Context{}, fmt.Errorf("%w: transcript is empty", ErrMissingContext)
		}
		if operation == values.OperationExtractHomework && m.Lesson.Start.IsZero() {
			return Context{}, fmt.Errorf("%w: lesson start", ErrMissingContext)
		}
		if err := c.selectLesson(m.Lesson); err != nil {
			return Context{}, err
		}

	case values.OperationHomeworkDraft:
		fields := []struct {
			name string
			in   string
			max  int
			out  *string
		}{
			{"assignment description", m.Assignment.Description, MaxDescriptionBytes, &c.assignmentDescription},
			{"summary", m.Summary, MaxSummaryBytes, &c.summary},
			{"previous draft", m.PreviousDraft, MaxPreviousDraftBytes, &c.previousDraft},
			{"student request", m.StudentRequest, MaxStudentRequestBytes, &c.studentRequest},
		}
		for _, f := range fields {
			if *f.out, err = multiLine(f.name, f.in, f.max); err != nil {
				return Context{}, err
			}
		}
		// The student's instructions go into a section of their own (TASK-094); bidi controls never reach it.
		c.studentRequest = strings.TrimSpace(values.StripBidi(c.studentRequest))
		if c.assignmentTitle, err = oneLine("assignment title", m.Assignment.Title); err != nil {
			return Context{}, err
		}
		if c.lessonSubject, err = oneLine("lesson subject", m.Lesson.Subject); err != nil {
			return Context{}, err
		}
		if c.lessonTeacher, err = oneLine("lesson teacher", m.Lesson.Teacher); err != nil {
			return Context{}, err
		}
		if c.assignmentTitle == "" && c.assignmentDescription == "" {
			return Context{}, fmt.Errorf("%w: assignment title or description", ErrMissingContext)
		}
	}
	return c, nil
}

// selectLesson keeps the lesson context of an operation on the transcript (TASK-093).
func (c *Context) selectLesson(l Lesson) error {
	var err error
	if c.lessonSubject, err = oneLine("lesson subject", l.Subject); err != nil {
		return err
	}
	if c.lessonTeacher, err = oneLine("lesson teacher", l.Teacher); err != nil {
		return err
	}
	if !l.Kind.Valid() {
		return invalid("lesson kind", "is not a known kind")
	}
	c.lessonKind = l.Kind
	c.lessonStart = l.Start
	if !l.Start.IsZero() && !l.NextStart.IsZero() {
		if !l.NextStart.After(l.Start) {
			return invalid("next lesson start", "is not after the lesson start")
		}
		if !l.NextKind.Valid() {
			return invalid("next lesson kind", "is not a known kind")
		}
		c.nextLessonStart, c.nextLessonKind = l.NextStart, l.NextKind
	}
	return nil
}

func invalid(field, why string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidMaterial, field, why)
}

func oneLine(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(s):
		return "", invalid(field, "is not valid UTF-8")
	case strings.ContainsFunc(s, notInLine):
		return "", invalid(field, "contains control characters")
	case utf8.RuneCountInString(s) > MaxTitleRunes:
		return "", invalid(field, fmt.Sprintf("is longer than %d characters", MaxTitleRunes))
	}
	return s, nil
}

func multiLine(field, s string, maxBytes int) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(s):
		return "", invalid(field, "is not valid UTF-8")
	case strings.ContainsFunc(s, badControl):
		return "", invalid(field, "contains control characters")
	case len(s) > maxBytes:
		return "", invalid(field, fmt.Sprintf("is larger than %d bytes", maxBytes))
	}
	return s, nil
}

func badControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }

// notInLine also rejects the line and paragraph separators, which break a line in many renderers.
func notInLine(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }

// joinTranscript keeps the segment texts only, one per line, without empty segments.
func joinTranscript(segments []Segment) (string, error) {
	var b strings.Builder
	for i, s := range segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		if !utf8.ValidString(text) || strings.ContainsFunc(text, notInLine) {
			return "", invalid(fmt.Sprintf("transcript segment %d", i), "is not one line of valid UTF-8 text")
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
		if b.Len() > MaxTranscriptBytes {
			return "", invalid("transcript", fmt.Sprintf("is larger than %d bytes", MaxTranscriptBytes))
		}
	}
	return b.String(), nil
}

// Operation returns the operation the context was selected for.
func (c Context) Operation() values.Operation { return c.operation }

// IsZero reports whether c is the zero value, which no operation was selected for.
func (c Context) IsZero() bool { return c == Context{} }

// LessonTitle returns the lesson title; empty when unknown.
func (c Context) LessonTitle() string { return c.lessonTitle }

// LessonSubject returns the subject of the lesson; empty when unknown.
func (c Context) LessonSubject() string { return c.lessonSubject }

// LessonTeacher returns the teacher of the lesson; empty when unknown.
func (c Context) LessonTeacher() string { return c.lessonTeacher }

// LessonStart returns the lesson start (the operations on the transcript; always set for extract_homework_deadlines).
func (c Context) LessonStart() time.Time { return c.lessonStart }

// NextLessonStart returns the start of the next lesson of the same subject; zero when unknown.
func (c Context) NextLessonStart() time.Time { return c.nextLessonStart }

// NextLessonKind returns the kind of the next lesson of the same subject; unknown without a next lesson.
func (c Context) NextLessonKind() values.LessonKind { return c.nextLessonKind }

// LessonKind returns the lesson kind (the operations on the transcript).
func (c Context) LessonKind() values.LessonKind { return c.lessonKind }

// Transcript returns the transcript text, one segment per line (summary, extract_homework_deadlines and lecture_text).
func (c Context) Transcript() string { return c.transcript }

// Summary returns the lesson summary (homework_draft only).
func (c Context) Summary() string { return c.summary }

// AssignmentTitle returns the assignment title (homework_draft only).
func (c Context) AssignmentTitle() string { return c.assignmentTitle }

// AssignmentDescription returns the assignment description (homework_draft only).
func (c Context) AssignmentDescription() string { return c.assignmentDescription }

// PreviousDraft returns the draft being refined (homework_draft only).
func (c Context) PreviousDraft() string { return c.previousDraft }

// StudentRequest returns the student's refinement request (homework_draft only).
func (c Context) StudentRequest() string { return c.studentRequest }
