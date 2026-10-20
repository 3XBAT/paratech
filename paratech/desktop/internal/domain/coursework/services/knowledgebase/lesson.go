package knowledgebase

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// NoSubjectName names the group of the lessons without a subject (Russian UI); its key is "".
const NoSubjectName = "Без предмета"

// Readiness is what is stored for the newest transcript of a recording.
type Readiness struct {
	Transcript  shared.TranscriptID // the newest transcript; empty: not transcribed
	Summary     bool                // it has a summary
	LectureText bool                // it has a finished lecture text
}

// Transcribed reports that the recording has a transcript.
func (r Readiness) Transcribed() bool { return r.Transcript != "" }

// Source is what one source names of a lesson: the snapshot the recording kept, the live lesson of the schedule or
// the user's correction (whose title is unused). Kind is a lesson kind code.
type Source struct {
	Title   string
	Subject string
	Kind    string
}

// Facts are what is known of the lesson of one recording.
type Facts struct {
	Recording shared.RecordingID
	StartedAt time.Time
	Duration  time.Duration // 0: unknown
	Snapshot  *Source       // nil: the recording kept no snapshot
	Live      *Source       // nil: an ad-hoc recording, or the lesson is gone
	// Correction is what the user set; an empty field corrects nothing.
	Correction Source
	Ready      Readiness
}

// Lesson is one lesson of the knowledge base: a recording with its title, subject and kind. Title is "" for an
// ad-hoc recording (the page names it by its date); Subject is "" for the group NoSubjectName.
type Lesson struct {
	Recording     shared.RecordingID
	StartedAt     time.Time
	Duration      time.Duration
	Title         string
	Subject       string
	Kind          string // lesson kind code; "" if unknown
	SubjectEdited bool   // the subject is the user's correction
	KindEdited    bool   // the kind is the user's correction
	Ready         Readiness
}

// SubjectKey is the key of the subject group of a lesson: the subject read case-insensitively ("ё" as "е") after it
// is cleaned to one line, so "Физика" and " физика " are one subject. "" is the group NoSubjectName.
func SubjectKey(subject string) string { return strings.Map(fold, values.CleanName(subject)) }

// NewLesson resolves what a lesson shows. Per field the user's correction wins; otherwise the snapshot names the
// lesson as a whole and the live lesson only without a snapshot (values.ResolveScheduledLesson, as its assignments
// show it). The title comes from the snapshot, else from the live lesson. Texts are cleaned to one line; a kind that
// is not a lesson kind code is dropped.
func NewLesson(f Facts) Lesson {
	var snapshot, live *values.ScheduledLesson
	if f.Snapshot != nil {
		snapshot = &values.ScheduledLesson{Subject: f.Snapshot.Subject, Kind: f.Snapshot.Kind}
	}
	if f.Live != nil {
		live = &values.ScheduledLesson{Subject: f.Live.Subject, Kind: f.Live.Kind}
	}
	sched := values.ResolveScheduledLesson(snapshot, live, time.Time{}, nil)
	label := values.NewLessonLabel(sched, values.LessonNames{}, values.NewLessonNames(f.Correction.Subject, ""))

	l := Lesson{
		Recording: f.Recording, StartedAt: f.StartedAt, Duration: f.Duration,
		Subject: label.Subject(), SubjectEdited: label.SubjectSource() == values.SourceUser,
		Kind: kindCode(sched.Kind), Ready: f.Ready,
	}
	if k := kindCode(f.Correction.Kind); k != "" {
		l.Kind, l.KindEdited = k, true
	}
	switch {
	case f.Snapshot != nil && values.CleanName(f.Snapshot.Title) != "":
		l.Title = values.CleanName(f.Snapshot.Title)
	case f.Live != nil:
		l.Title = values.CleanName(f.Live.Title)
	}
	return l
}

// SubjectKey returns the key of the subject group of the lesson.
func (l Lesson) SubjectKey() string { return SubjectKey(l.Subject) }

// Subject is one subject of the knowledge base with how many lessons it has. NoSubject marks the group NoSubjectName
// (key "").
type Subject struct {
	Key       string
	Name      string
	Lessons   int
	Latest    time.Time // the start of its newest lesson
	NoSubject bool
}

// Subjects groups the lessons by SubjectKey. A group is named as its newest lesson spells it; only groups with a lesson
// are listed, by name, the group NoSubjectName last.
func Subjects(lessons []Lesson) []Subject {
	groups := map[string]*Subject{}
	var order []string
	for _, l := range Newest(lessons) {
		key := l.SubjectKey()
		g, ok := groups[key]
		if !ok {
			g = &Subject{Key: key, Name: l.Subject, Latest: l.StartedAt, NoSubject: key == ""}
			if g.NoSubject {
				g.Name = NoSubjectName
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Lessons++
	}

	out := make([]Subject, 0, len(order))
	for _, key := range order {
		out = append(out, *groups[key])
	}
	slices.SortStableFunc(out, func(a, b Subject) int {
		if a.NoSubject != b.NoSubject {
			if a.NoSubject {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// LessonsOf returns the lessons of the subject with key (SubjectKey; "" for NoSubjectName), newest first.
func LessonsOf(lessons []Lesson, key string) []Lesson {
	key = SubjectKey(key)
	var out []Lesson
	for _, l := range Newest(lessons) {
		if l.SubjectKey() == key {
			out = append(out, l)
		}
	}
	return out
}

// Newest returns a copy of the lessons, newest start first; equal starts by recording id.
func Newest(lessons []Lesson) []Lesson {
	out := slices.Clone(lessons)
	slices.SortStableFunc(out, func(a, b Lesson) int {
		if c := b.StartedAt.Compare(a.StartedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Recording, b.Recording)
	})
	return out
}

// Oldest returns a copy of the lessons, oldest start first, as a subject is exported.
func Oldest(lessons []Lesson) []Lesson {
	out := Newest(lessons)
	slices.Reverse(out)
	return out
}

// kindCode keeps a lesson kind code ("lecture", "seminar", "practice", "lab"); anything else is "".
func kindCode(kind string) string {
	if k := values.LessonKind(kind); k != values.LessonKindUnknown && k.Valid() {
		return kind
	}
	return ""
}

// fold reads a rune case-insensitively for keys and search, "ё" as "е" and any space or control character as a
// space; it maps one rune to one rune, so positions stay.
func fold(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= 'а' && r <= 'я':
		return r
	case r >= 'A' && r <= 'Z', r >= 'А' && r <= 'Я': // common letters skip the table of unicode.ToLower
		return r + 'a' - 'A' // 'а' - 'А' is the same 32
	case r == 'ё' || r == 'Ё':
		return 'е'
	case unicode.IsSpace(r) || unicode.IsControl(r) || r == 0x2028 || r == 0x2029:
		return ' '
	}
	return unicode.ToLower(r)
}
