package knowledgebase

import (
	"cmp"
	"slices"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Prefixes of the lesson keys. A recording, a schedule lesson, a day of a subject and an undated note never share a
// key: the prefixes differ and none is a prefix of another.
const (
	recordingKeyPrefix = "recording:"
	lessonKeyPrefix    = "lesson:"
	dayKeyPrefix       = "day:"
	noteKeyPrefix      = "note:"
	dayKeyLayout       = "2006-01-02"
)

// RecordingKey is the key of a recorded lesson.
func RecordingKey(id shared.RecordingID) string { return recordingKeyPrefix + string(id) }

// LessonKey is the key of a note-only lecture of a schedule lesson without a recording.
func LessonKey(id shared.LessonID) string { return lessonKeyPrefix + string(id) }

// DayKey is the key of a note-only lecture of a subject on a local date (day is read as a calendar date).
func DayKey(subject shared.SubjectID, day time.Time) string {
	return dayKeyPrefix + string(subject) + ":" + day.Format(dayKeyLayout)
}

// NoteKey is the key of an undated lecture: each undated note is a lecture of its own.
func NoteKey(id shared.NoteID) string { return noteKeyPrefix + string(id) }

// NoteAnchor is what a note of the student is attached to (ADR-0027).
type NoteAnchor string

// Anchors of a note.
const (
	AnchorRecording NoteAnchor = "recording"
	AnchorLesson    NoteAnchor = "lesson"
	AnchorDay       NoteAnchor = "day"
	AnchorUndated   NoteAnchor = "undated"
)

// NoteFacts are what is known of one active note of the student, as plain values: the note is of the library context
// (ARCH-11). Day is the date of its anchor snapshot at local midnight, zero for an undated one; Kind and LessonTitle
// are the rest of the snapshot. LessonGone: the schedule no longer has the lesson of a lesson anchor. Every text is
// untrusted.
type NoteFacts struct {
	Note        shared.NoteID
	Title       string
	Subject     LibrarySubject
	Anchor      NoteAnchor
	Recording   shared.RecordingID // AnchorRecording only
	Lesson      shared.LessonID    // AnchorLesson only
	LessonGone  bool
	Day         time.Time
	Kind        string
	LessonTitle string
	Parts       []shared.MaterialID
	CreatedAt   time.Time
}

// Note is a note of the student as a lesson lists it; Subject is its own library subject (the lesson it joined may
// name another).
type Note struct {
	ID        shared.NoteID
	Title     string
	Subject   shared.SubjectID
	Parts     []shared.MaterialID
	CreatedAt time.Time
}

// WithNotes joins the student's notes to the recorded lessons and adds the lectures known only from notes (ADR-0027):
//   - a note anchored to a recording of the base, or to a schedule lesson a recording of the base was made in (the
//     newest such recording), joins that recorded lesson;
//   - a note of a schedule lesson without a recording is the lecture LessonKey; a lesson the schedule lost falls back
//     to its day, as does a recording that is no lesson of the base any more;
//   - notes of one subject on one day are the lecture DayKey; an undated note is the lecture NoteKey of its own.
//
// A note-only lecture takes the subject of its first note, the title and kind of its snapshot (else the note's title)
// and starts at its day. Notes without a library subject are left out. The result is ordered as Newest; a lesson
// lists its notes as they were made.
func WithNotes(recorded []Lesson, notes []NoteFacts) []Lesson {
	out := Newest(recorded)
	byRecording := make(map[shared.RecordingID]int, len(out))
	byLesson := map[shared.LessonID]int{}
	for i, l := range out {
		if l.Recording == "" {
			continue
		}
		byRecording[l.Recording] = i
		if _, ok := byLesson[l.ScheduleLesson]; l.ScheduleLesson != "" && !ok {
			byLesson[l.ScheduleLesson] = i // newest first: the newest recording of the lesson
		}
	}

	ordered := slices.Clone(notes)
	slices.SortStableFunc(ordered, func(a, b NoteFacts) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Note, b.Note)
	})

	byKey := map[string]int{}
	for _, n := range ordered {
		if n.Subject.ID == "" {
			continue
		}
		ref := Note{ID: n.Note, Title: n.Title, Subject: n.Subject.ID, Parts: slices.Clone(n.Parts), CreatedAt: n.CreatedAt}

		i, joined := -1, false
		switch n.Anchor {
		case AnchorRecording:
			i, joined = byRecording[n.Recording]
		case AnchorLesson:
			i, joined = byLesson[n.Lesson]
		}
		if joined {
			out[i].Notes = append(out[i].Notes, ref)
			continue
		}

		key := lectureKey(n)
		if j, ok := byKey[key]; ok {
			out[j].Notes = append(out[j].Notes, ref)
			continue
		}
		byKey[key] = len(out)
		out = append(out, noteLecture(key, n, ref))
	}
	return Newest(out)
}

// lectureKey is the key of the note-only lecture of a note that joined no recorded lesson.
func lectureKey(n NoteFacts) string {
	switch {
	case n.Anchor == AnchorLesson && !n.LessonGone && n.Lesson != "":
		return LessonKey(n.Lesson)
	case n.Anchor != AnchorUndated && !n.Day.IsZero():
		return DayKey(n.Subject.ID, n.Day)
	}
	return NoteKey(n.Note)
}

// noteLecture is a note-only lecture of its first note.
func noteLecture(key string, n NoteFacts, ref Note) Lesson {
	l := Lesson{
		Key: key, Subject: values.CleanName(n.Subject.Name), Library: n.Subject,
		Kind: string(values.LessonKindOf(n.Kind)), Notes: []Note{ref},
	}
	if key != NoteKey(n.Note) {
		l.StartedAt = n.Day
	}
	if key == LessonKey(n.Lesson) {
		l.ScheduleLesson = n.Lesson
	}
	l.Title = values.CleanName(n.LessonTitle)
	if l.Title == "" {
		l.Title = values.CleanName(n.Title)
	}
	return l
}
