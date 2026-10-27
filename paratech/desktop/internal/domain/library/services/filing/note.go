package filing

import "paratech/desktop/internal/domain/library/subject"

// NoteFolder is the system folder a part of the student's own notes goes to when it arrives in «Входящие»
// (ADR-0027): «Семинары» for a seminar or practice, «Лабораторные» for a lab, «Лекции» for a lecture and anything
// else. lessonKind is the stable code of the schedule kind ("lecture", "seminar", "practice", "lab"); the library
// context cannot import the schedule, so the codes are spelled here.
func NoteFolder(lessonKind string) subject.System {
	switch lessonKind {
	case "seminar", "practice":
		return subject.SystemSeminars
	case "lab":
		return subject.SystemLabs
	}
	return subject.SystemLectures
}
