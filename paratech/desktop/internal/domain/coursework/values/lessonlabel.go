package values

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxNameRunes bounds a subject or a teacher of an assignment (TASK-092), as the schedule bounds them.
const MaxNameRunes = 200

// maxKindBytes bounds the code of a lesson kind ("lecture", "seminar", ...).
const maxKindBytes = 32

// LessonNames are the subject and the teacher of an assignment that do not come from the schedule: the hints the
// homework search found in the transcript (model output, untrusted) or the user's correction. The zero value names
// nothing. Build it with NewLessonNames.
type LessonNames struct {
	subject string
	teacher string
}

// NewLessonNames cleans both texts with CleanName; it never fails, an unusable text becomes empty.
func NewLessonNames(subject, teacher string) LessonNames {
	return LessonNames{subject: CleanName(subject), teacher: CleanName(teacher)}
}

// Subject returns the subject; empty if none.
func (n LessonNames) Subject() string { return n.subject }

// Teacher returns the teacher; empty if none.
func (n LessonNames) Teacher() string { return n.teacher }

// IsZero reports that n names nothing.
func (n LessonNames) IsZero() bool { return n == LessonNames{} }

// CleanName makes untrusted text one plain line: control and format characters (bidi overrides, zero-width ones)
// and line separators removed or turned into spaces, spaces collapsed, cut to MaxNameRunes characters. Cleaning a
// cleaned text changes nothing.
func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r) || r == 0x2028 || r == 0x2029:
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxNameRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxNameRunes]))
	}
	return s
}

// NameSource tells where a shown subject or teacher comes from.
type NameSource string

// Sources of a subject or a teacher, in the order they win.
const (
	SourceNone       NameSource = ""
	SourceUser       NameSource = "user"       // the user's correction
	SourceSchedule   NameSource = "schedule"   // the lesson as recorded (or the live lesson)
	SourceTranscript NameSource = "transcript" // a hint of the homework search; shown marked «по записи»
)

// ScheduledLesson is what the schedule says about the lesson of an assignment, as plain values: the snapshot the
// recording kept (TASK-088), or the live lesson when there is no snapshot. Start is the lesson date shown («занятие
// 19.10»); the caller passes it in the student's time zone, and the recording start when the schedule has none.
type ScheduledLesson struct {
	Subject string
	Kind    string // code of the lesson kind ("lecture", "seminar", "practice", "lab"); empty if unknown
	Teacher string
	Start   time.Time
}

// UnknownLessonKind is the schedule's code of an unknown lesson kind; a live lesson with it names no kind.
const UnknownLessonKind = "unknown"

// ResolveScheduledLesson is the one rule of which lesson data an assignment shows (TASK-088, TASK-092): the snapshot
// the recording kept wins (nil: none); only without it the live lesson (nil: none), whose UnknownLessonKind becomes
// "". The date falls back to recorded (the recording start; zero if none) and is given in zone (nil: unchanged).
func ResolveScheduledLesson(snapshot, live *ScheduledLesson, recorded time.Time, zone *time.Location) ScheduledLesson {
	var out ScheduledLesson
	switch {
	case snapshot != nil:
		out = *snapshot
	case live != nil:
		out = *live
		if out.Kind == UnknownLessonKind {
			out.Kind = ""
		}
	}
	if out.Start.IsZero() {
		out.Start = recorded
	}
	if !out.Start.IsZero() && zone != nil {
		out.Start = out.Start.In(zone)
	}
	return out
}

// LessonLabel is what an assignment shows of its lesson: «Предмет · тип · преподаватель» and «занятие 19.10». Build
// it with NewLessonLabel; the zero value shows nothing.
type LessonLabel struct {
	subject       string
	subjectSource NameSource
	kind          string
	teacher       string
	teacherSource NameSource
	date          time.Time
}

// NewLessonLabel resolves the label. Per field the user's correction wins, then the schedule, then the transcript
// hint: a hint never replaces what the schedule names. Texts are cleaned with CleanName; a kind that is not a short
// lower-case code is dropped.
func NewLessonLabel(s ScheduledLesson, hints, correction LessonNames) LessonLabel {
	l := LessonLabel{kind: kindCode(s.Kind), date: s.Start}
	l.subject, l.subjectSource = pick(correction.subject, CleanName(s.Subject), hints.subject)
	l.teacher, l.teacherSource = pick(correction.teacher, CleanName(s.Teacher), hints.teacher)
	return l
}

func pick(user, schedule, transcript string) (string, NameSource) {
	switch {
	case user != "":
		return user, SourceUser
	case schedule != "":
		return schedule, SourceSchedule
	case transcript != "":
		return transcript, SourceTranscript
	}
	return "", SourceNone
}

func kindCode(s string) string {
	if s == "" || len(s) > maxKindBytes || strings.ContainsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && r != '-' && r != '_'
	}) {
		return ""
	}
	return s
}

// Subject returns the subject; empty if unknown.
func (l LessonLabel) Subject() string { return l.subject }

// SubjectSource tells where the subject comes from; SourceNone when it is unknown.
func (l LessonLabel) SubjectSource() NameSource { return l.subjectSource }

// Kind returns the code of the lesson kind; empty if unknown.
func (l LessonLabel) Kind() string { return l.kind }

// Teacher returns the teacher; empty if unknown.
func (l LessonLabel) Teacher() string { return l.teacher }

// TeacherSource tells where the teacher comes from; SourceNone when it is unknown.
func (l LessonLabel) TeacherSource() NameSource { return l.teacherSource }

// Date returns the lesson start in the zone the caller gave; zero if unknown.
func (l LessonLabel) Date() time.Time { return l.date }

// IsZero reports that the label shows nothing.
func (l LessonLabel) IsZero() bool { return l == LessonLabel{} }

// Fixed parts of the subject of a homework email (Russian UI).
const (
	subjectHomework = "домашнее задание"
	subjectDueDate  = "02.01"
)

// MaxEmailSubjectRunes bounds a homework email subject; it is the limit of a delivery subject.
const MaxEmailSubjectRunes = 200

// HomeworkEmailSubject is the subject of the homework email of an assignment: «<Предмет>: домашнее задание к
// <ДД.ММ>», without the subject «Домашнее задание к <ДД.ММ>», without a deadline «<Предмет>: домашнее задание»,
// with neither «Домашнее задание». due is formatted in its own zone: pass it in the student's. The subject may be a
// transcript hint: it is cleaned to one line and cut so that the whole subject fits MaxEmailSubjectRunes.
func HomeworkEmailSubject(subject string, due time.Time) string {
	tail := subjectHomework
	if !due.IsZero() {
		tail += " к " + due.Format(subjectDueDate)
	}
	subject = CleanName(subject)
	if subject == "" {
		r, size := utf8.DecodeRuneInString(tail)
		return string(unicode.ToUpper(r)) + tail[size:]
	}

	tail = ": " + tail
	room := MaxEmailSubjectRunes - utf8.RuneCountInString(tail)
	if utf8.RuneCountInString(subject) > room {
		subject = strings.TrimSpace(string([]rune(subject)[:room]))
	}
	return subject + tail
}
