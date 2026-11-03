package subject

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRemoveAndRestore(t *testing.T) {
	s := newSubject(t, "Физ")
	removedAt := t0.Add(time.Hour)

	if changed, err := s.Remove(removedAt); err != nil || !changed {
		t.Fatalf("Remove = %v, %v", changed, err)
	}
	if s.Active() || s.Status() != StatusRemoved || !s.RemovedAt().Equal(removedAt) || !s.UpdatedAt().Equal(removedAt) {
		t.Fatalf("after Remove: status %s, removedAt %v, updatedAt %v", s.Status(), s.RemovedAt(), s.UpdatedAt())
	}
	if changed, err := s.Remove(removedAt.Add(time.Minute)); err != nil || changed || !s.RemovedAt().Equal(removedAt) {
		t.Fatalf("Remove again = %v, %v; removedAt %v (want no change)", changed, err, s.RemovedAt())
	}

	// The removed subject is stored and loaded as removed.
	st := s.State()
	st.Version = 2
	back, err := Restore(st)
	if err != nil || back.Status() != StatusRemoved || !reflect.DeepEqual(back.State(), st) {
		t.Fatalf("Restore(removed state) = %v, %v", back, err)
	}

	restoredAt := removedAt.Add(RestoreWindow - time.Millisecond)
	if changed, err := s.Restore(restoredAt); err != nil || !changed {
		t.Fatalf("Restore = %v, %v", changed, err)
	}
	if !s.Active() || !s.RemovedAt().IsZero() || !s.UpdatedAt().Equal(restoredAt) {
		t.Fatalf("after Restore: status %s, removedAt %v", s.Status(), s.RemovedAt())
	}
	if changed, err := s.Restore(restoredAt); err != nil || changed {
		t.Fatalf("Restore of an active subject = %v, %v; want no change", changed, err)
	}
	if got := s.State().Aliases; !reflect.DeepEqual(got, []string{"Физ"}) {
		t.Fatalf("aliases after Restore = %q", got)
	}

	if _, err := newSubject(t).Remove(time.Time{}); !errors.Is(err, ErrInvalidSubject) {
		t.Fatalf("Remove(zero time) = %v, want ErrInvalidSubject", err)
	}
}

func TestRestoreWindow(t *testing.T) {
	if RestoreWindow != 72*time.Hour {
		t.Fatalf("RestoreWindow = %v, want 3 days", RestoreWindow)
	}
	removedAt := t0
	tests := []struct {
		name    string
		now     time.Time
		due     bool
		wantErr error
	}{
		{"right after", removedAt, false, nil},
		{"a moment before the window ends", removedAt.Add(RestoreWindow - time.Nanosecond), false, nil},
		{"exactly at the end", removedAt.Add(RestoreWindow), true, ErrRestoreExpired},
		{"after the window", removedAt.Add(RestoreWindow + time.Hour), true, ErrRestoreExpired},
		{"clock before the removal", removedAt.Add(-time.Hour), false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSubject(t)
			if s.PurgeDue(tt.now) || !s.PurgeAt().IsZero() {
				t.Fatal("PurgeDue or PurgeAt of an active subject")
			}
			if _, err := s.Remove(removedAt); err != nil {
				t.Fatal(err)
			}
			if got := s.PurgeDue(tt.now); got != tt.due {
				t.Fatalf("PurgeDue = %v, want %v", got, tt.due)
			}
			if !s.PurgeAt().Equal(removedAt.Add(RestoreWindow)) {
				t.Fatalf("PurgeAt = %v, want %v", s.PurgeAt(), removedAt.Add(RestoreWindow))
			}
			if cut := !s.RemovedAt().After(PurgeCutoff(tt.now)); cut != tt.due {
				t.Fatalf("RemovedAt not after PurgeCutoff = %v, want PurgeDue %v", cut, tt.due)
			}
			changed, err := s.Restore(tt.now)
			if !errors.Is(err, tt.wantErr) || (err == nil) != changed {
				t.Fatalf("Restore = %v, %v; want %v", changed, err, tt.wantErr)
			}
			if tt.wantErr != nil && (s.Active() || !s.RemovedAt().Equal(removedAt)) {
				t.Fatal("an expired Restore changed the subject")
			}
		})
	}
}

func TestRemovedSubjectRefusesChanges(t *testing.T) {
	s := newSubject(t, "Физ")
	if _, err := s.AddFolder("f1", folderName(t, "Своя"), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTeacher(TeacherLecture, "Иванов И. И.", "teacher@example.test", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(t0); err != nil {
		t.Fatal(err)
	}
	before := s.State()

	t1 := t0.Add(time.Minute)
	changes := []struct {
		name string
		do   func() error
	}{
		{"add alias", func() error { _, err := s.AddAlias("Общая физика", t1); return err }},
		{"remove alias", func() error { _, err := s.RemoveAlias("Физ", t1); return err }},
		{"add folder", func() error { _, err := s.AddFolder("f2", folderName(t, "Другая"), t1); return err }},
		{"remove folder", func() error { return s.RemoveFolder("f1", 0, t1) }},
		{"set teacher", func() error {
			_, err := s.SetTeacher(TeacherLab, "Петров П. П.", "lab@example.test", t1)
			return err
		}},
		{"clear teacher", func() error { _, err := s.ClearTeacher(TeacherLecture, t1); return err }},
		{"delete at once", func() error { return s.Removable(0) }},
	}
	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			if err := c.do(); !errors.Is(err, ErrRemoved) {
				t.Fatalf("%s on a removed subject = %v, want ErrRemoved", c.name, err)
			}
			if !reflect.DeepEqual(s.State(), before) {
				t.Fatalf("%s changed a removed subject", c.name)
			}
		})
	}
}

func TestStatusTransitionsTable(t *testing.T) {
	for from, tos := range transitions {
		for _, to := range []Status{StatusActive, StatusRemoved} {
			s := &Subject{status: from}
			err := s.transition(to, t0)
			allowed := false
			for _, ok := range tos {
				allowed = allowed || ok == to
			}
			if (err == nil) != allowed {
				t.Errorf("transition %s -> %s = %v, allowed %v", from, to, err, allowed)
			}
		}
	}
	if len(transitions) != 2 {
		t.Fatalf("transitions cover %d statuses, want 2", len(transitions))
	}
}
