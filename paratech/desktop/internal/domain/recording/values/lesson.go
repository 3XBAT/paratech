package values

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Bounds of the texts of a LessonSnapshot.
const (
	MaxLessonTitleBytes   = 512
	MaxLessonSubjectBytes = 256
	MaxLessonTeacherBytes = 256
	MaxLessonKindBytes    = 32
)

// LessonSnapshot is what a recording keeps of its lesson at the start (TASK-088): the schedule may delete or change
// the lesson later, and the recording still shows what was recorded. The zero value is no snapshot (an ad-hoc
// recording, or one stored before TASK-088). The texts come from an imported schedule: untrusted, plain text only.
type LessonSnapshot struct {
	title   string
	subject string
	kind    string
	teacher string
	start   time.Time
	end     time.Time
}

// LessonSnapshotSpec is the raw lesson data for NewLessonSnapshot. All fields are optional.
type LessonSnapshotSpec struct {
	Title   string
	Subject string
	Kind    string // short code of the lesson kind, for example "lecture"
	Teacher string
	Start   time.Time // scheduled start; zero if unknown
	End     time.Time // scheduled end; zero if unknown
}

// NewLessonSnapshot makes a snapshot of untrusted lesson data; it never fails, since a recording must start anyway.
// Texts become one line without control and format characters, spaces collapsed and cut to their bounds. A kind that is
// not a short code of ASCII letters, digits, '-' or '_' is dropped (it is lower-cased first). An end that is not after
// the start is dropped. Applying it to the data of a snapshot gives the same snapshot.
func NewLessonSnapshot(spec LessonSnapshotSpec) LessonSnapshot {
	s := LessonSnapshot{
		title:   cleanLine(spec.Title, MaxLessonTitleBytes),
		subject: cleanLine(spec.Subject, MaxLessonSubjectBytes),
		kind:    lessonKind(spec.Kind),
		teacher: cleanLine(spec.Teacher, MaxLessonTeacherBytes),
		start:   spec.Start,
		end:     spec.End,
	}
	if !s.start.IsZero() && !s.end.IsZero() && !s.end.After(s.start) {
		s.end = time.Time{}
	}
	return s
}

// Title returns the lesson title; empty if unknown.
func (s LessonSnapshot) Title() string { return s.title }

// Subject returns the subject of the lesson; empty if unknown.
func (s LessonSnapshot) Subject() string { return s.subject }

// Kind returns the short code of the lesson kind; empty if unknown.
func (s LessonSnapshot) Kind() string { return s.kind }

// Teacher returns the teacher of the lesson; empty if unknown.
func (s LessonSnapshot) Teacher() string { return s.teacher }

// ScheduledStart returns the start of the lesson by the schedule; zero if unknown.
func (s LessonSnapshot) ScheduledStart() time.Time { return s.start }

// ScheduledEnd returns the end of the lesson by the schedule; zero if unknown.
func (s LessonSnapshot) ScheduledEnd() time.Time { return s.end }

// IsZero reports that there is no snapshot.
func (s LessonSnapshot) IsZero() bool {
	return s.title == "" && s.subject == "" && s.kind == "" && s.teacher == "" && s.start.IsZero() && s.end.IsZero()
}

// Equal reports whether both snapshots hold the same data (times compared as instants).
func (s LessonSnapshot) Equal(o LessonSnapshot) bool {
	return s.title == o.title && s.subject == o.subject && s.kind == o.kind && s.teacher == o.teacher &&
		s.start.Equal(o.start) && s.end.Equal(o.end)
}

// lessonKind is the kind code lower-cased, or empty when it is not a short code.
func lessonKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if len(kind) > MaxLessonKindBytes || strings.ContainsFunc(kind, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	}) {
		return ""
	}
	return kind
}

// cleanLine makes untrusted text one safe line: no control and format characters (bidi overrides, zero-width ones),
// spaces collapsed, at most limit bytes of valid UTF-8.
func cleanLine(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut])
	}
	return s
}
