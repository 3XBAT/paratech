package assignment

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

// origins builds one assignment of every origin (ADR-0029): extracted from a transcript, found in a material, added by
// the student (by hand or through the agent, both NewByStudent).
func origins(t *testing.T) map[string]*Assignment {
	t.Helper()
	material, err := NewExtracted(Spec{
		ID: "a2", Origin: Origin{Material: "m1", Index: 0}, PromptVersion: 1, Details: details(t, "Task 2", "", time.Time{}),
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*Assignment{
		"extracted":    extracted(t),
		"material":     material,
		"by student":   byStudent(t, StudentSpec{ID: "a3", Title: "Mine", SubjectName: "Алгебра"}),
		"by the agent": byStudent(t, StudentSpec{ID: "ag-1", Title: "Agent's"}),
	}
}

func TestDeleteAndRestore(t *testing.T) {
	for name, a := range origins(t) {
		t.Run(name, func(t *testing.T) {
			before := a.State()
			later := t0.Add(time.Hour)

			if a.Deleted() || a.NotDeleted() != nil {
				t.Fatal("a new assignment is deleted")
			}
			if err := a.Restore(later); !errors.Is(err, ErrNotDeleted) {
				t.Fatalf("Restore of one not deleted: err = %v", err)
			}
			if err := a.Delete(later); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if !a.Deleted() || !a.DeletedAt().Equal(later) || !a.UpdatedAt().Equal(later) {
				t.Fatalf("after Delete: deleted %v at %v, updated %v", a.Deleted(), a.DeletedAt(), a.UpdatedAt())
			}
			if err := a.Delete(later.Add(time.Minute)); !errors.Is(err, ErrDeleted) || !a.DeletedAt().Equal(later) {
				t.Fatalf("second Delete: err = %v, deleted at %v", err, a.DeletedAt())
			}
			if !errors.Is(a.NotDeleted(), ErrDeleted) {
				t.Fatal("NotDeleted of a deleted one is nil")
			}

			restoredAt := later.Add(time.Hour)
			if err := a.Restore(restoredAt); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			after := a.State()
			if a.Deleted() || !after.DeletedAt.IsZero() || !after.UpdatedAt.Equal(restoredAt) {
				t.Fatalf("after Restore: %+v", after)
			}
			after.UpdatedAt = before.UpdatedAt
			if after != before {
				t.Fatalf("Delete and Restore changed more than the time:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

// A deleted assignment is not changed: an edit is ErrDeleted, a correction, a reextraction and new hints change
// nothing, and reprocessing never withdraws it (a tombstone, ADR-0029).
func TestDeletedAssignmentIsNotChanged(t *testing.T) {
	a := extracted(t)
	if err := a.Delete(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := a.State()
	later := t0.Add(2 * time.Hour)

	if err := a.Edit(details(t, "Mine", "", time.Time{}), later); !errors.Is(err, ErrDeleted) {
		t.Fatalf("Edit: err = %v", err)
	}
	if a.Correct(values.NewLessonNames("Геометрия", ""), later) {
		t.Fatal("Correct changed a deleted assignment")
	}
	if changed, err := a.Reextract(details(t, "Model title", "", time.Time{}), 2, later); err != nil || changed {
		t.Fatalf("Reextract = %v, %v; want unchanged", changed, err)
	}
	if changed, err := a.SetHints(values.NewLessonNames("Алгебра", "Петров"), later); err != nil || changed {
		t.Fatalf("SetHints = %v, %v; want unchanged", changed, err)
	}
	if a.Superseded(0) {
		t.Fatal("a deleted assignment is superseded")
	}
	if a.State() != before {
		t.Fatalf("state changed:\n got %+v\nwant %+v", a.State(), before)
	}
}

func TestRestoreKeepsDeletedAt(t *testing.T) {
	a := byStudent(t, StudentSpec{ID: "a3", Title: "Mine"})
	if err := a.Delete(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	st := a.State()
	st.Version = 2 // as the repository stores it after the create and the delete; Restore refuses version 0
	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Deleted() || !got.DeletedAt().Equal(t0.Add(time.Hour)) {
		t.Fatalf("restored: deleted %v at %v", got.Deleted(), got.DeletedAt())
	}

	// A deleted assignment is never a repeat of the same one added again.
	again := byStudent(t, StudentSpec{ID: "a3", Title: "Mine"})
	if got.SameByStudent(again) || again.SameByStudent(got) {
		t.Fatal("a deleted assignment counts as a repeat")
	}
}
