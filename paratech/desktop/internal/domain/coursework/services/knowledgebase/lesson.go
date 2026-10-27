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
	// ScheduleLesson is the schedule lesson the recording was made in; "" for an ad-hoc one.
	ScheduleLesson shared.LessonID
	StartedAt      time.Time
	Duration       time.Duration // 0: unknown
	Snapshot       *Source       // nil: the recording kept no snapshot
	Live           *Source       // nil: an ad-hoc recording, or the lesson is gone
	// Correction is what the user set; an empty field corrects nothing.
	Correction Source
	Ready      Readiness
}

// Lesson is one lesson of the knowledge base: a recording with its title, subject and kind, or a lecture the student
// has only notes of (ADR-0027, WithNotes). Title is "" for an ad-hoc recording (the page names it by its date);
// Subject is "" for the group NoSubjectName. Library is the subject of the library its Subject string resolves to
// (ADR-0024); zero when it resolves to none.
//
// Key identifies the lesson (RecordingKey, LessonKey, DayKey or NoteKey). A note-only lecture has no Recording, starts
// at the local midnight of its day (zero when undated) and has no Duration nor Ready.
type Lesson struct {
	Key            string
	Recording      shared.RecordingID
	ScheduleLesson shared.LessonID
	StartedAt      time.Time
	Duration       time.Duration
	Title          string
	Subject        string
	Kind           string // lesson kind code; "" if unknown
	SubjectEdited  bool   // the subject is the user's correction
	KindEdited     bool   // the kind is the user's correction
	Ready          Readiness
	Library        LibrarySubject
	// Notes are the student's own notes of the lesson, as made; nil without any.
	Notes []Note
}

// Recorded reports that the lesson is a recording.
func (l Lesson) Recorded() bool { return l.Recording != "" }

// Undated reports that the lesson is a note-only lecture without a date.
func (l Lesson) Undated() bool { return l.Recording == "" && l.StartedAt.IsZero() }

// madeAt is when its first note was made: undated lectures are listed so.
func (l Lesson) madeAt() time.Time {
	if len(l.Notes) == 0 {
		return time.Time{}
	}
	return l.Notes[0].CreatedAt
}

// LibrarySubject is a subject of the library (ADR-0024) as the knowledge base groups by it: its id and name.
type LibrarySubject struct {
	ID   shared.SubjectID
	Name string
}

// libraryKeyPrefix starts the key of a library subject group. Keys of free strings are folded to lower case, so no
// such key starts with an upper-case letter and the two kinds never collide.
const libraryKeyPrefix = "ID:"

// LibraryKey is the key of the group of a library subject.
func LibraryKey(id shared.SubjectID) string { return libraryKeyPrefix + string(id) }

// WithLibrary returns a copy of the lessons with Library set for those whose Subject is a key of resolved (the
// subjects their strings resolve to). The strings themselves are kept: a stored subject is never rewritten.
func WithLibrary(lessons []Lesson, resolved map[string]LibrarySubject) []Lesson {
	out := slices.Clone(lessons)
	for i, l := range out {
		if sub, ok := resolved[l.Subject]; ok && l.Subject != "" && sub.ID != "" {
			out[i].Library = sub
		} else {
			out[i].Library = LibrarySubject{}
		}
	}
	return out
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
		Key: RecordingKey(f.Recording), Recording: f.Recording, ScheduleLesson: f.ScheduleLesson,
		StartedAt: f.StartedAt, Duration: f.Duration,
		Subject: label.Subject(), SubjectEdited: label.SubjectSource() == values.SourceUser,
		Kind: string(values.LessonKindOf(sched.Kind)), Ready: f.Ready,
	}
	if k := string(values.LessonKindOf(f.Correction.Kind)); k != "" {
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

// SubjectKey returns the key of the subject group of the lesson: LibraryKey of its library subject, else the key of
// its free string.
func (l Lesson) SubjectKey() string {
	if l.Library.ID != "" {
		return LibraryKey(l.Library.ID)
	}
	return SubjectKey(l.Subject)
}

// Subject is one subject of the knowledge base with how many recorded lessons it has (Lessons; 0 for a subject with
// note-only lectures only). NoSubject marks the group NoSubjectName (key ""). SubjectID is the library subject of the
// group; "" for a group of a free string.
type Subject struct {
	Key       string
	Name      string
	Lessons   int
	Latest    time.Time // the start of its newest recorded lesson; zero without one
	NoSubject bool
	SubjectID shared.SubjectID
}

// Subjects groups the lessons by SubjectKey. A library subject is named as the library names it, a free string as its
// newest lesson spells it; only groups with a lesson (recorded or note-only) are listed, by name, the group
// NoSubjectName last.
func Subjects(lessons []Lesson) []Subject {
	groups := map[string]*Subject{}
	var order []string
	for _, l := range Newest(lessons) {
		key := l.SubjectKey()
		g, ok := groups[key]
		if !ok {
			g = &Subject{Key: key, Name: l.Subject, NoSubject: key == "", SubjectID: l.Library.ID}
			switch {
			case g.NoSubject:
				g.Name = NoSubjectName
			case l.Library.ID != "":
				g.Name = l.Library.Name
			}
			groups[key] = g
			order = append(order, key)
		}
		if l.Recorded() {
			if g.Lessons == 0 {
				g.Latest = l.StartedAt
			}
			g.Lessons++
		}
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
		if c := cmp.Compare(SubjectKey(a.Name), SubjectKey(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// LessonsOf returns the lessons of the subject with key (Lesson.SubjectKey: LibraryKey of a library subject, else the
// key of a free string; "" for NoSubjectName), newest first.
func LessonsOf(lessons []Lesson, key string) []Lesson {
	if !strings.HasPrefix(key, libraryKeyPrefix) {
		key = SubjectKey(key)
	}
	var out []Lesson
	for _, l := range Newest(lessons) {
		if l.SubjectKey() == key {
			out = append(out, l)
		}
	}
	return out
}

// Newest returns a copy of the lessons, newest start first; equal starts by recording id, then by key. Undated
// lectures come last, as their first notes were made.
func Newest(lessons []Lesson) []Lesson {
	out := slices.Clone(lessons)
	slices.SortStableFunc(out, func(a, b Lesson) int {
		if au, bu := a.Undated(), b.Undated(); au != bu {
			if au {
				return 1
			}
			return -1
		} else if au {
			if c := a.madeAt().Compare(b.madeAt()); c != 0 {
				return c
			}
			return cmp.Compare(a.Key, b.Key)
		}
		if c := b.StartedAt.Compare(a.StartedAt); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Recording, b.Recording); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// Oldest returns a copy of the lessons, oldest start first, as a subject is exported; undated lectures still come
// last, in the order of Newest.
func Oldest(lessons []Lesson) []Lesson {
	out := Newest(lessons)
	dated := slices.IndexFunc(out, Lesson.Undated)
	if dated < 0 {
		dated = len(out)
	}
	slices.Reverse(out[:dated])
	return out
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
