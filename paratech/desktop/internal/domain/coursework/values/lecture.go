package values

import (
	"strconv"
	"strings"
)

// LessonKind is the kind of a lesson as the app knows it (TASK-090). It only selects a constant sentence of the
// trusted instructions of the lecture text (prompts); it never adds content. The zero value is LessonKindUnknown.
type LessonKind string

// Lesson kinds.
const (
	LessonKindUnknown  LessonKind = ""
	LessonKindLecture  LessonKind = "lecture"
	LessonKindSeminar  LessonKind = "seminar"
	LessonKindPractice LessonKind = "practice"
	LessonKindLab      LessonKind = "lab"
)

// Valid reports whether k is one of the kinds above.
func (k LessonKind) Valid() bool {
	switch k {
	case LessonKindUnknown, LessonKindLecture, LessonKindSeminar, LessonKindPractice, LessonKindLab:
		return true
	}
	return false
}

// LessonKindOf maps the stable kind code of a lesson ("lecture", "seminar", "practice", "lab"; the code the schedule
// and the recording snapshot keep) to a LessonKind. Anything else, "unknown" and "" included, is LessonKindUnknown.
func LessonKindOf(code string) LessonKind {
	k := LessonKind(code)
	if k.Valid() {
		return k
	}
	return LessonKindUnknown
}

// LectureTextCheckpoint is the job checkpoint of a lecture_text job: how many of its parts are formatted, "3/12".
// The recordings screen reads it for the part progress; it holds counts only, never text.
func LectureTextCheckpoint(done, total int) string {
	return strconv.Itoa(done) + "/" + strconv.Itoa(total)
}

// ParseLectureTextCheckpoint reads a checkpoint of LectureTextCheckpoint; ok is false for anything else, for example
// the empty checkpoint of a job that has not reported yet.
func ParseLectureTextCheckpoint(s string) (done, total int, ok bool) {
	a, b, found := strings.Cut(s, "/")
	if !found {
		return 0, 0, false
	}
	done, errA := strconv.Atoi(a)
	total, errB := strconv.Atoi(b)
	if errA != nil || errB != nil || done < 0 || total < 1 || done > total || strconv.Itoa(done) != a || strconv.Itoa(total) != b {
		return 0, 0, false
	}
	return done, total, true
}
