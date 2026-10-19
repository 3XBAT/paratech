package summaryview

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
)

// TASK-092: every assignment of the summary screen shows the subject, kind, teacher and date of its lesson: the
// snapshot of the recording first, the live lesson without one, the transcript hints only for what both lack.
func TestAssignmentsShowTheLesson(t *testing.T) {
	kept := time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		snapshot   recvalues.LessonSnapshotSpec
		live       lesson.Content
		hints      values.LessonNames
		correction values.LessonNames
		want       Assignment
	}{
		{
			name:     "snapshot",
			snapshot: recvalues.LessonSnapshotSpec{Title: "Матанализ", Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов", Start: kept},
			live:     lesson.Content{Subject: "География", Kind: lesson.KindSeminar, Teacher: "Сидоров"},
			hints:    values.NewLessonNames("Алгебра", "Петров"),
			want: Assignment{Subject: "Матанализ", SubjectSource: values.SourceSchedule, Kind: "lecture", Teacher: "Иванов",
				TeacherSource: values.SourceSchedule, LessonDate: kept.In(msk)},
		},
		{
			name:  "live lesson and a hint",
			live:  lesson.Content{Subject: "География", Kind: lesson.KindSeminar},
			hints: values.NewLessonNames("Алгебра", "Петров"),
			want: Assignment{Subject: "География", SubjectSource: values.SourceSchedule, Kind: "seminar", Teacher: "Петров",
				TeacherSource: values.SourceTranscript, LessonDate: t0.In(msk)},
		},
		{
			name:       "correction",
			live:       lesson.Content{Subject: "География", Teacher: "Сидоров"},
			correction: values.NewLessonNames("Физика", ""),
			want: Assignment{Subject: "Физика", SubjectSource: values.SourceUser, Teacher: "Сидоров",
				TeacherSource: values.SourceSchedule, LessonDate: t0.In(msk)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addRecording(t, "r1", "Урок")
			l := e.lessons.rows["lesson-r1"]
			c := l.Content()
			c.Subject, c.Kind, c.Teacher = tc.live.Subject, tc.live.Kind, tc.live.Teacher
			if _, err := l.ChangeContent(c, t0); err != nil {
				t.Fatal(err)
			}
			if tc.snapshot.Title != "" {
				r, err := recording.New(recording.Spec{
					ID: "r1", LessonID: "lesson-r1", MaxDuration: 3 * time.Hour, Lesson: recvalues.NewLessonSnapshot(tc.snapshot),
				}, t0)
				if err != nil {
					t.Fatal(err)
				}
				e.recordings["r1"] = r
			}
			tr := e.addTranscript(t, "r1")
			e.addAssignment(t, tr, "Задание", "", time.Time{})
			a := e.assignments.rows[tr][0]
			if _, err := a.SetHints(tc.hints, t0); err != nil {
				t.Fatal(err)
			}
			a.Correct(tc.correction, t0)

			v, err := e.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			got := v.Homework.Assignments[0]
			got.ID, got.Title = "", ""
			if got != tc.want || got.LessonDate.Location() != msk {
				t.Fatalf("assignment = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}
