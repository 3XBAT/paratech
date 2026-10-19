package tests

// Scenario tests of the subject, kind and teacher of lessons (TASK-089) on the real SQLite adapter, the real CSV,
// XLSX and ICS parsers and the real use case: imports store them, and a re-import that changes only them updates the
// lessons (a new content revision, so the calendar is written again).

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
)

type lessonDetails struct {
	subject string
	kind    lesson.Kind
	teacher string
}

func (e *scheduleEnv) checkDetails(uid string, want lessonDetails, revision int64) {
	e.t.Helper()
	r := e.byUID(uid)
	if r.Subject != want.subject || r.Kind != want.kind || r.Teacher != want.teacher || r.Revision != revision {
		e.t.Errorf("%s = %q, %v, %q, revision %d; want %q, %v, %q, revision %d", uid, r.Subject, r.Kind, r.Teacher,
			r.Revision, want.subject, want.kind, want.teacher, revision)
	}
}

func TestScheduleImportCSVLessonDetails(t *testing.T) {
	e := newScheduleEnv(t)

	// Without the columns the title tells.
	const plain = "title,date,start,end,timezone,uid\n" +
		"\"Матанализ, лекция\",2026-10-05,09:00,10:30,Europe/Moscow,m-1\n" +
		"Лабиринты истории,2026-10-05,10:45,12:15,Europe/Moscow,h-1\n"
	if res := e.importFile(plain); res.Added != 2 {
		t.Fatalf("first import = %+v", res)
	}
	e.checkDetails("csv:id:m-1", lessonDetails{"Матанализ", lesson.KindLecture, ""}, 1)
	e.checkDetails("csv:id:h-1", lessonDetails{"Лабиринты истории", lesson.KindUnknown, ""}, 1)

	// The same lessons with the columns: only subject, kind and teacher change.
	const detailed = "title,date,start,end,timezone,uid,subject,kind,teacher\n" +
		"\"Матанализ, лекция\",2026-10-05,09:00,10:30,Europe/Moscow,m-1,Математический анализ,,Иванов И. И.\n" +
		"Лабиринты истории,2026-10-05,10:45,12:15,Europe/Moscow,h-1,,сем.,\n"
	p := e.preview(detailed)
	if len(p.Plan.Changed) != 2 || len(p.Plan.Added)+len(p.Plan.Removed) != 0 {
		t.Fatalf("preview of the details = %+v", p.Plan)
	}
	if res, err := e.commit(detailed, p.ConfirmToken); err != nil || res.Changed != 2 {
		t.Fatalf("commit of the details = %+v, %v", res, err)
	}
	e.checkDetails("csv:id:m-1", lessonDetails{"Математический анализ", lesson.KindLecture, "Иванов И. И."}, 2)
	e.checkDetails("csv:id:h-1", lessonDetails{"Лабиринты истории", lesson.KindSeminar, ""}, 2)

	if again := e.preview(detailed); again.Plan.HasChanges() || len(again.Plan.Unchanged) != 2 {
		t.Fatalf("re-import preview = %+v", again.Plan)
	}

	// A bad kind blocks the whole file.
	bad := e.preview(strings.Replace(detailed, "сем.", "экзамен", 1))
	if bad.Plan.Valid() || len(bad.Plan.Errors) != 1 || bad.Plan.Errors[0].Field != "kind" || bad.Plan.Errors[0].Line != 3 {
		t.Fatalf("bad kind preview = %+v", bad.Plan)
	}
}

func TestScheduleImportXLSXLessonDetails(t *testing.T) {
	e := newXLSXScheduleEnv(t)
	book := func(teacher string) []byte {
		return xlsxBook(t, [][]any{
			{"Название", "title", "date", "start", "end", "timezone", "uid", "Предмет", "Тип", "Преподаватель"},
			{"", "Физика (лаб)", xlsxDay(10, 5), xlsxClock(9, 0), xlsxClock(10, 30), "Europe/Moscow", "p-1", nil, nil, teacher},
		})
	}
	if _, res := e.importAs(importplan.SourceXLSX, string(book("Петров"))); res.Added != 1 {
		t.Fatalf("first import = %+v", res)
	}
	e.checkDetails("xlsx:id:p-1", lessonDetails{"Физика", lesson.KindLab, "Петров"}, 1)

	if _, res := e.importAs(importplan.SourceXLSX, string(book("Сидорова"))); res.Changed != 1 {
		t.Fatalf("re-import with another teacher = %+v", res)
	}
	e.checkDetails("xlsx:id:p-1", lessonDetails{"Физика", lesson.KindLab, "Сидорова"}, 2)
}

func TestScheduleImportICSLessonDetails(t *testing.T) {
	e := newXLSXScheduleEnv(t)
	file := func(description string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
			"BEGIN:VEVENT\r\nUID:calc@uni.example\r\nSUMMARY:Lecture: Calculus\r\n" +
			"DTSTART:20261016T120000Z\r\nDTEND:20261016T133000Z\r\n" +
			"ORGANIZER;CN=Organizer Name:mailto:organizer@uni.example\r\n" + description +
			"END:VEVENT\r\nEND:VCALENDAR\r\n"
	}
	if _, res := e.importAs(importplan.SourceICS, file("")); res.Added != 1 {
		t.Fatalf("first import = %+v", res)
	}
	e.checkDetails("ics:calc@uni.example", lessonDetails{"Calculus", lesson.KindLecture, "Organizer Name"}, 1)

	if _, res := e.importAs(importplan.SourceICS, file("DESCRIPTION:Teacher: Dr. Smith\r\n")); res.Changed != 1 {
		t.Fatalf("re-import with a teacher line = %+v", res)
	}
	e.checkDetails("ics:calc@uni.example", lessonDetails{"Calculus", lesson.KindLecture, "Dr. Smith"}, 2)
}
