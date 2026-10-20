package recording

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

func mustCorrection(t *testing.T, subject, kind string) values.LessonCorrection {
	t.Helper()
	c, err := values.NewLessonCorrection(subject, kind)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCorrectLessonByStatus(t *testing.T) {
	at := t0.Add(5 * time.Hour)
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			r := inStatus(t, s)
			before := r.UpdatedAt()
			changed, err := r.CorrectLesson(at, mustCorrection(t, "Физика", "lab"))
			if s.InProgress() {
				if !errors.Is(err, ErrInvalidTransition) || changed || !r.LessonCorrection().IsZero() || !r.UpdatedAt().Equal(before) {
					t.Fatalf("CorrectLesson of %s = %v, %v; want ErrInvalidTransition and nothing changed", s, changed, err)
				}
				return
			}
			if err != nil || !changed {
				t.Fatalf("CorrectLesson of %s = %v, %v", s, changed, err)
			}
			if c := r.LessonCorrection(); c.Subject() != "Физика" || c.Kind() != "lab" || !r.UpdatedAt().Equal(at) {
				t.Fatalf("correction %+v, updated %v", c, r.UpdatedAt())
			}
		})
	}
}

func TestCorrectLessonKeepsTheSnapshot(t *testing.T) {
	r, err := New(Spec{ID: "r1", LessonID: "l1", Lesson: testSnapshot(), MaxDuration: testCap}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(t0.Add(time.Second), "device not found"); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Hour)
	if _, err := r.CorrectLesson(at, mustCorrection(t, "Химия", "seminar")); err != nil {
		t.Fatal(err)
	}
	if r.LessonSnapshot() != testSnapshot() {
		t.Fatalf("snapshot changed: %+v", r.LessonSnapshot())
	}

	// The same correction again changes nothing; the zero one clears it.
	if changed, err := r.CorrectLesson(at.Add(time.Minute), mustCorrection(t, " Химия ", "SEMINAR")); err != nil || changed || !r.UpdatedAt().Equal(at) {
		t.Fatalf("same correction: changed %v, %v, updated %v", changed, err, r.UpdatedAt())
	}
	if changed, err := r.CorrectLesson(at.Add(time.Minute), values.LessonCorrection{}); err != nil || !changed || !r.LessonCorrection().IsZero() {
		t.Fatalf("clear: changed %v, %v, %+v", changed, err, r.LessonCorrection())
	}
}

func TestCorrectLessonRefuses(t *testing.T) {
	deleting := inStatus(t, StatusCompleted)
	if err := deleting.MarkDeleting(t0.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := deleting.CorrectLesson(t0.Add(2*time.Hour), mustCorrection(t, "Физика", "")); !errors.Is(err, ErrDeleting) {
		t.Fatalf("marked for deletion: err = %v, want ErrDeleting", err)
	}
	if _, err := inStatus(t, StatusCompleted).CorrectLesson(time.Time{}, mustCorrection(t, "Физика", "")); !errors.Is(err, ErrInvalidRecording) {
		t.Fatalf("no time: err = %v, want ErrInvalidRecording", err)
	}
}

func TestRestoreKeepsLessonCorrection(t *testing.T) {
	r := inStatus(t, StatusCompleted)
	if _, err := r.CorrectLesson(t0.Add(time.Hour), mustCorrection(t, "Физика", "practice")); err != nil {
		t.Fatal(err)
	}
	r.Persisted(2)
	got, err := Restore(stateOf(r))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
	}
}

func TestInKnowledgeBase(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			if got := inStatus(t, s).InKnowledgeBase(); got != (s == StatusCompleted) {
				t.Fatalf("InKnowledgeBase of %s = %v", s, got)
			}
		})
	}

	silent := inStatus(t, StatusFinalizing)
	if err := silent.CompleteTrack(t0.Add(time.Hour), mustRef(t, "recordings/r1.wav"), silentTrack); err != nil {
		t.Fatal(err)
	}
	if silent.InKnowledgeBase() {
		t.Error("a silent recording is in the knowledge base")
	}

	deleting := inStatus(t, StatusCompleted)
	if err := deleting.MarkDeleting(t0.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if deleting.InKnowledgeBase() {
		t.Error("a recording marked for deletion is in the knowledge base")
	}
}
