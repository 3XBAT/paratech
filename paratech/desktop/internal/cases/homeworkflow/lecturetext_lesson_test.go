package homeworkflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/services/prompts"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// TASK-090: the kind of the lesson selects the trusted wording, the title and subject are untrusted data only. The
// recording's snapshot wins over the schedule (like the title, TASK-088); without a snapshot the schedule is used.
func TestLectureTextTakesTheLessonKindAndSubject(t *testing.T) {
	kept := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	seminar := recvalues.LessonSnapshotSpec{
		Title: "Матанализ, семинар", Subject: "Матанализ", Kind: "seminar", Teacher: "Петров", Start: kept,
	}
	for name, tc := range map[string]struct {
		lessonID    shared.LessonID
		snapshot    recvalues.LessonSnapshotSpec
		wantKind    values.LessonKind
		wantTitle   string
		wantSubject string
	}{
		"seminar snapshot": {
			lessonID: "l1", snapshot: seminar,
			wantKind: values.LessonKindSeminar, wantTitle: "Матанализ, семинар", wantSubject: "Матанализ",
		},
		"lesson deleted from the schedule": {
			lessonID: "gone", snapshot: seminar,
			wantKind: values.LessonKindSeminar, wantTitle: "Матанализ, семинар", wantSubject: "Матанализ",
		},
		"snapshot of an unknown kind": {
			lessonID: "l2", snapshot: recvalues.LessonSnapshotSpec{Title: "Матанализ", Kind: "unknown", Start: kept},
			wantKind: values.LessonKindUnknown, wantTitle: "Матанализ",
		},
		"no snapshot: the schedule": {
			lessonID: "l2", wantKind: values.LessonKindLab, wantTitle: "Физика, лаб.", wantSubject: "Физика",
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newPPEnv(t)
			l, err := lesson.New(lesson.Spec{ID: "l2", Content: lesson.Content{
				Title: "Физика, лаб.", Subject: "Физика", Kind: lesson.KindLab, Teacher: "Иванова",
				Start: lessonStart.UTC(), End: lessonStart.Add(90 * time.Minute).UTC(), TimeZone: "Europe/Moscow",
			}}, ppT0)
			if err != nil {
				t.Fatal(err)
			}
			e.lessons["l2"] = l
			start := lessonStart.Add(3 * time.Minute)
			e.addTranscript("r2", tc.lessonID, "t2", start, "Today we solve problems.")
			r, err := recording.New(recording.Spec{
				ID: "r2", LessonID: tc.lessonID, MaxDuration: 3 * time.Hour, Lesson: recvalues.NewLessonSnapshot(tc.snapshot),
			}, start)
			if err != nil {
				t.Fatal(err)
			}
			e.recordings["r2"] = r
			e.llm.Answer(values.OperationLectureText, lectureAnswer("Problems", 0, "We solve problems."))

			if err := e.pp.Handle(context.Background(), e.snapshot("j1", LectureTextJobType, "t2"), (&reports{}).report); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			reqs := e.llm.Requests()
			if len(reqs) != 1 {
				t.Fatalf("LLM calls = %d, want 1", len(reqs))
			}
			if got := reqs[0].Instructions(); got != prompts.LectureTextInstructions(tc.wantKind) {
				t.Fatalf("instructions are not the wording of %q:\n%s", tc.wantKind, got)
			}
			data := untrusted(t, reqs[0])
			subject, _ := data["subject"].(string)
			if data["lesson_title"] != tc.wantTitle || subject != tc.wantSubject {
				t.Fatalf("untrusted data: title %v, subject %q; want %q, %q", data["lesson_title"], subject, tc.wantTitle, tc.wantSubject)
			}
			// Schedule text never reaches the trusted instructions; the teacher is not sent at all.
			for _, s := range []string{"Матанализ", "Физика", "Петров", "Иванова"} {
				if strings.Contains(reqs[0].Instructions(), s) {
					t.Fatalf("%q is in the trusted instructions", s)
				}
			}
			if d := reqs[0].UntrustedData(); strings.Contains(d, "Петров") || strings.Contains(d, "Иванова") {
				t.Fatalf("the teacher is in the data: %s", d)
			}
		})
	}
}
