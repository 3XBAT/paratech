package recording

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

func testSnapshot() values.LessonSnapshot {
	return values.NewLessonSnapshot(values.LessonSnapshotSpec{
		Title: "Тестовый урок", Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов",
		Start: t0, End: t0.Add(90 * time.Minute),
	})
}

func TestNewWithLessonSnapshot(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		ok   bool
	}{
		{"lesson with snapshot", Spec{ID: "r1", LessonID: "l1", Lesson: testSnapshot(), MaxDuration: testCap}, true},
		{"lesson without snapshot", Spec{ID: "r1", LessonID: "l1", MaxDuration: testCap}, true},
		{"ad-hoc without snapshot", Spec{ID: "r1", MaxDuration: testCap}, true},
		{"snapshot without lesson", Spec{ID: "r1", Lesson: testSnapshot(), MaxDuration: testCap}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(tc.spec, t0)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidRecording) {
					t.Fatalf("error = %v, want ErrInvalidRecording", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.LessonSnapshot() != tc.spec.Lesson {
				t.Fatalf("snapshot = %+v, want %+v", r.LessonSnapshot(), tc.spec.Lesson)
			}
		})
	}
}

// The snapshot is fixed at the start: the lifecycle never changes it.
func TestLessonSnapshotOutlivesTheLifecycle(t *testing.T) {
	r, err := New(Spec{ID: "r1", LessonID: "l1", Lesson: testSnapshot(), MaxDuration: testCap}, t0)
	if err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Second)
	for _, step := range []func() error{
		func() error { return r.Begin(at) },
		func() error { return r.Stop(at.Add(time.Minute)) },
		func() error { return r.Finalize(at.Add(time.Minute)) },
		func() error { return r.Complete(at.Add(2*time.Minute), mustRef(t, "recordings/r1.wav"), time.Minute) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		if r.LessonSnapshot() != testSnapshot() {
			t.Fatalf("snapshot changed in %s: %+v", r.Status(), r.LessonSnapshot())
		}
	}
}

func TestRestoreKeepsLessonSnapshot(t *testing.T) {
	r, err := New(Spec{ID: "r1", LessonID: "l1", Lesson: testSnapshot(), MaxDuration: testCap}, t0)
	if err != nil {
		t.Fatal(err)
	}
	r.Persisted(1)

	got, err := Restore(stateOf(r))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, r)
	}

	// The link to a lesson purged from the schedule may be gone (ON DELETE SET NULL); the snapshot stays.
	s := stateOf(r)
	s.LessonID = ""
	got, err = Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.LessonID() != "" || got.LessonSnapshot().Title() != "Тестовый урок" {
		t.Fatalf("restored lesson %q, snapshot %+v", got.LessonID(), got.LessonSnapshot())
	}
}
