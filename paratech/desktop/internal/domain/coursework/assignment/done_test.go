package assignment

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
)

func TestMarkDoneAndUndone(t *testing.T) {
	for name, a := range origins(t) {
		t.Run(name, func(t *testing.T) {
			before := a.State()
			doneAt := t0.Add(time.Hour)

			if a.Done() || !a.DoneAt().IsZero() {
				t.Fatal("a new assignment is done")
			}
			if changed, err := a.MarkUndone(doneAt); err != nil || changed || a.State() != before {
				t.Fatalf("MarkUndone of one not done = %v, %v; want unchanged", changed, err)
			}

			if changed, err := a.MarkDone(doneAt); err != nil || !changed {
				t.Fatalf("MarkDone = %v, %v; want changed", changed, err)
			}
			if !a.Done() || !a.DoneAt().Equal(doneAt) || !a.UpdatedAt().Equal(doneAt) {
				t.Fatalf("after MarkDone: done %v at %v, updated %v", a.Done(), a.DoneAt(), a.UpdatedAt())
			}
			done := a.State()
			if changed, err := a.MarkDone(doneAt.Add(time.Hour)); err != nil || changed || a.State() != done {
				t.Fatalf("second MarkDone = %v, %v, state %+v; want unchanged", changed, err, a.State())
			}

			undoneAt := doneAt.Add(2 * time.Hour)
			if changed, err := a.MarkUndone(undoneAt); err != nil || !changed {
				t.Fatalf("MarkUndone = %v, %v; want changed", changed, err)
			}
			after := a.State()
			if a.Done() || !after.DoneAt.IsZero() || !after.UpdatedAt.Equal(undoneAt) {
				t.Fatalf("after MarkUndone: %+v", after)
			}
			after.UpdatedAt = before.UpdatedAt
			if after != before {
				t.Fatalf("MarkDone and MarkUndone changed more than the time:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

// A clock set back before the creation stores the creation time, so the state still restores.
func TestMarkDoneBeforeCreation(t *testing.T) {
	a := extracted(t)
	early := t0.Add(-time.Hour)
	if changed, err := a.MarkDone(early); err != nil || !changed {
		t.Fatalf("MarkDone = %v, %v", changed, err)
	}
	if !a.DoneAt().Equal(t0) || !a.UpdatedAt().Equal(early) {
		t.Fatalf("done at %v, updated %v; want the creation time and now", a.DoneAt(), a.UpdatedAt())
	}
	st := a.State()
	st.Version = 1
	if _, err := Restore(st); err != nil {
		t.Fatalf("Restore: %v", err)
	}
}

func TestDeletedAssignmentIsNotMarked(t *testing.T) {
	tests := []struct {
		name      string
		doneFirst bool
		mark      func(a *Assignment, now time.Time) (bool, error)
	}{
		{"done", false, (*Assignment).MarkDone},
		{"undone", true, (*Assignment).MarkUndone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := extracted(t)
			if tc.doneFirst {
				if _, err := a.MarkDone(t0.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Delete(t0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			before := a.State()
			if changed, err := tc.mark(a, t0.Add(2*time.Hour)); !errors.Is(err, ErrDeleted) || changed {
				t.Fatalf("mark of a deleted one = %v, %v; want ErrDeleted", changed, err)
			}
			if a.State() != before {
				t.Fatalf("state changed:\n got %+v\nwant %+v", a.State(), before)
			}
		})
	}
}

// Edit, EditByStudent, Correct, Delete and «Вернуть» keep the mark.
func TestChangesKeepTheMark(t *testing.T) {
	doneAt := t0.Add(time.Hour)
	later := t0.Add(2 * time.Hour)
	tests := []struct {
		name   string
		change func(t *testing.T, a *Assignment)
	}{
		{"edit", func(t *testing.T, a *Assignment) {
			if err := a.Edit(details(t, "Mine", "", time.Time{}), later); err != nil {
				t.Fatal(err)
			}
		}},
		{"edit by student", func(t *testing.T, a *Assignment) {
			if err := a.EditByStudent(StudentEdit{Title: "Mine", SubjectName: "Алгебра"}, later); err != nil {
				t.Fatal(err)
			}
		}},
		{"correct", func(t *testing.T, a *Assignment) {
			if !a.Correct(values.NewLessonNames("Геометрия", ""), later) {
				t.Fatal("Correct changed nothing")
			}
		}},
		{"delete", func(t *testing.T, a *Assignment) {
			if err := a.Delete(later); err != nil {
				t.Fatal(err)
			}
		}},
		{"delete and restore", func(t *testing.T, a *Assignment) {
			if err := a.Delete(later); err != nil {
				t.Fatal(err)
			}
			if err := a.Restore(later.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := extracted(t)
			if _, err := a.MarkDone(doneAt); err != nil {
				t.Fatal(err)
			}
			tc.change(t, a)
			if !a.Done() || !a.DoneAt().Equal(doneAt) {
				t.Fatalf("after %s: done %v at %v; want the mark kept", tc.name, a.Done(), a.DoneAt())
			}
		})
	}
}

func TestRestoreStateDoneAt(t *testing.T) {
	base := extracted(t).State()
	base.Version = 3
	tests := []struct {
		name    string
		doneAt  time.Time
		wantErr bool
	}{
		{"not done", time.Time{}, false},
		{"done at creation", t0, false},
		{"done later", t0.Add(time.Hour), false},
		{"done before creation", t0.Add(-time.Millisecond), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := base
			st.DoneAt = tc.doneAt
			got, err := Restore(st)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidAssignment) {
					t.Fatalf("Restore = %v; want ErrInvalidAssignment", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Done() != !tc.doneAt.IsZero() || !got.DoneAt().Equal(tc.doneAt) || got.State() != st {
				t.Fatalf("restored: done %v at %v, state %+v", got.Done(), got.DoneAt(), got.State())
			}
		})
	}
}
