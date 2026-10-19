package googlecal

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

// TASK-092: the deadline event names the lesson; transcript hints are marked, file and model text is escaped.
func TestDeadlineText(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	date := time.Date(2026, 10, 19, 10, 0, 0, 0, msk)
	cases := []struct {
		name  string
		label values.LessonLabel
		want  string
	}{
		{"nothing known", values.LessonLabel{}, deadlineDescription},
		{"schedule", values.NewLessonLabel(values.ScheduledLesson{Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов И. И.", Start: date},
			values.LessonNames{}, values.LessonNames{}),
			"Матанализ · лекция · Иванов И. И.\nЗанятие 19.10\n\n" + deadlineDescription},
		{"hints are marked", values.NewLessonLabel(values.ScheduledLesson{Kind: "lab"}, values.NewLessonNames("Физика", "Петров"), values.LessonNames{}),
			"Физика (по записи) · лабораторная · Петров (по записи)\n\n" + deadlineDescription},
		{"correction is not marked", values.NewLessonLabel(values.ScheduledLesson{}, values.NewLessonNames("Физика", ""), values.NewLessonNames("Химия", "")),
			"Химия\n\n" + deadlineDescription},
		{"date only", values.NewLessonLabel(values.ScheduledLesson{Start: date}, values.LessonNames{}, values.LessonNames{}),
			"Занятие 19.10\n\n" + deadlineDescription},
		{"unknown kind is not named", values.NewLessonLabel(values.ScheduledLesson{Subject: "Физика", Kind: "unknown"}, values.LessonNames{}, values.LessonNames{}),
			"Физика\n\n" + deadlineDescription},
		{"markup is escaped", values.NewLessonLabel(values.ScheduledLesson{}, values.NewLessonNames(`<a href="https://evil.example">x</a>`, "A & B"), values.LessonNames{}),
			`&lt;a href="https://evil.example"&gt;x&lt;/a&gt; (по записи) · A &amp; B (по записи)` + "\n\n" + deadlineDescription},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deadlineText(tc.label); got != tc.want {
				t.Fatalf("description =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
