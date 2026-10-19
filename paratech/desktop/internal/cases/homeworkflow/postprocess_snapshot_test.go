package homeworkflow

import (
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// TASK-088: the lesson the recording kept at its start names the lesson and anchors the dates, also when the schedule
// dropped or renamed it since; the assignment links only a lesson that still exists.
func TestLessonContextFromTheSnapshot(t *testing.T) {
	kept := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) // 12:00 MSK
	for name, tc := range map[string]struct {
		lessonID   shared.LessonID
		wantLesson shared.LessonID
	}{
		"lesson removed from the schedule": {lessonID: "gone", wantLesson: ""},
		"lesson renamed in the schedule":   {lessonID: "l1", wantLesson: "l1"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newPPEnv(t)
			start := kept.Add(5 * time.Minute)
			e.addTranscript("r2", tc.lessonID, "t2", start, "Write an essay by tomorrow.")
			r, err := recording.New(recording.Spec{
				ID: "r2", LessonID: tc.lessonID, MaxDuration: 3 * time.Hour,
				Lesson: recvalues.NewLessonSnapshot(recvalues.LessonSnapshotSpec{
					Title: "Тестовый урок", Start: kept, End: kept.Add(90 * time.Minute),
				}),
			}, start)
			if err != nil {
				t.Fatal(err)
			}
			e.recordings["r2"] = r
			e.llm.Answer(values.OperationExtractHomework, `{"assignments":[{"title":"Essay","description":"","due":"2026-10-15T23:59:00+03:00"}]}`)

			if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t2")); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			data := e.llm.Requests()[0].UntrustedData()
			if !strings.Contains(data, "Тестовый урок") || strings.Contains(data, "Geography") ||
				!strings.Contains(data, `"lesson_start":"2026-10-14T12:00:00+03:00"`) {
				t.Fatalf("untrusted data = %s", data)
			}
			got := e.assignmentsOf("t2")
			if len(got) != 1 || got[0].Lesson != tc.wantLesson {
				t.Fatalf("assignments = %+v, want one of lesson %q", got, tc.wantLesson)
			}
		})
	}
}
