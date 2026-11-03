package library

// The name of a removed subject is free once its restore window has ended (TASK-308, ADR-0027): a check that meets it
// erases the subject through ExpiredSubjects first. RemovedSubjects lists the ones that can still come back.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// fakeExpired erases the removed subjects due at the clock, as subjectpurge does; keep leaves them (a purge that
// failed for them) and err is returned either way.
type fakeExpired struct {
	subjects *fakeSubjects
	clock    *fakeClock
	calls    int
	keep     bool
	err      error
}

func (f *fakeExpired) Purge(context.Context) (int, error) {
	f.calls++
	n := 0
	if !f.keep {
		f.subjects.mu.Lock()
		for id, st := range f.subjects.rows {
			if s, err := subject.Restore(st); err == nil && s.PurgeDue(f.clock.now) {
				delete(f.subjects.rows, id)
				n++
			}
		}
		f.subjects.mu.Unlock()
	}
	return n, f.err
}

// withExpired wires a fakeExpired into the service of e.
func withExpired(e *env) *fakeExpired {
	f := &fakeExpired{subjects: e.subjects, clock: e.clock}
	e.svc.expired = f
	return f
}

// discard removes sub at the clock of e.
func discard(t *testing.T, e *env, sub *subject.Subject) {
	t.Helper()
	got, err := e.subjects.GetAny(context.Background(), sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.DiscardSubject(context.Background(), sub.ID(), got.Version()); err != nil {
		t.Fatal(err)
	}
}

func TestNameOfAnExpiredSubjectIsFreed(t *testing.T) {
	ctx := context.Background()
	end := t0.Add(subject.RestoreWindow)

	tests := []struct {
		name string
		// setup makes the removed subject (at t0) and returns the use of its name.
		setup func(t *testing.T, e *env) (old *subject.Subject, use func() error)
	}{
		{"create", func(t *testing.T, e *env) (*subject.Subject, func() error) {
			old := createSubject(t, e, "Физика", "Физ")
			discard(t, e, old)
			return old, func() error {
				_, err := e.svc.CreateSubject(ctx, NewSubject{Name: "физика"})
				return err
			}
		}},
		{"create with an alias", func(t *testing.T, e *env) (*subject.Subject, func() error) {
			old := createSubject(t, e, "Физика", "Физ")
			discard(t, e, old)
			return old, func() error {
				_, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Общая физика", Aliases: []string{"физ"}})
				return err
			}
		}},
		{"alias", func(t *testing.T, e *env) (*subject.Subject, func() error) {
			mech := createSubject(t, e, "Механика")
			old := createSubject(t, e, "Физика", "Физ")
			discard(t, e, old)
			return old, func() error {
				_, err := e.svc.AddAlias(ctx, mech.ID(), "Физ")
				return err
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			purge := withExpired(e)
			old, use := tt.setup(t, e)

			// A second before the end the name is still the removed subject's, and «Вернуть» is possible.
			e.clock.now = end.Add(-time.Second)
			var gone *RemovedNameError
			if err := use(); !errors.As(err, &gone) || gone.Subject != old.ID() {
				t.Fatalf("before the end = %v, want a RemovedNameError of %s", err, old.ID())
			}
			if purge.calls != 0 {
				t.Fatal("the purge ran inside the window")
			}

			// At the end it is erased and the name is free.
			e.clock.now = end
			if err := use(); err != nil {
				t.Fatalf("at the end = %v, want the name free", err)
			}
			if purge.calls != 1 {
				t.Fatalf("purge calls = %d, want 1", purge.calls)
			}
			if _, err := e.subjects.GetAny(ctx, old.ID()); !errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("the expired subject is still stored: %v", err)
			}
		})
	}
}

// A purge that leaves the subject holding the name is ErrUnavailable with its error, never «удалён»; a purge error
// about another subject does not stop the name.
func TestNameOfAnExpiredSubjectPurgeFailure(t *testing.T) {
	ctx := context.Background()
	disk := errors.New("disk busy")

	e := newEnv(t)
	purge := withExpired(e)
	old := createSubject(t, e, "Физика")
	discard(t, e, old)
	e.clock.now = t0.Add(subject.RestoreWindow)

	purge.keep, purge.err = true, disk
	_, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Физика"})
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, disk) || errors.Is(err, ErrNameTaken) {
		t.Fatalf("CreateSubject with a failing purge = %v, want ErrUnavailable with the cause", err)
	}
	purge.keep, purge.err = true, nil
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Физика"}); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNameTaken) {
		t.Fatalf("CreateSubject with a purge that left it = %v, want ErrUnavailable", err)
	}

	purge.keep, purge.err = false, disk // failed for another subject only
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Физика"}); err != nil {
		t.Fatalf("CreateSubject = %v, want the name free", err)
	}
}

// Without the port the name waits for the daily purge; «Вернуть» refuses from the end of the window on.
func TestNameOfAnExpiredSubjectWithoutThePurge(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	old := createSubject(t, e, "Физика")
	discard(t, e, old)
	removed, err := e.subjects.GetAny(ctx, old.ID())
	if err != nil {
		t.Fatal(err)
	}

	e.clock.now = t0.Add(subject.RestoreWindow)
	var gone *RemovedNameError
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Физика"}); !errors.As(err, &gone) {
		t.Fatalf("CreateSubject = %v, want a RemovedNameError", err)
	}
	if _, err := e.svc.RestoreSubject(ctx, old.ID(), removed.Version()); !errors.Is(err, ErrRestoreExpired) {
		t.Fatalf("RestoreSubject at the end = %v, want ErrRestoreExpired", err)
	}
	e.clock.now = t0.Add(subject.RestoreWindow - time.Second)
	if _, err := e.svc.RestoreSubject(ctx, old.ID(), removed.Version()); err != nil {
		t.Fatalf("RestoreSubject a second before = %v", err)
	}
}

// The subjects from the schedule: a spelling, an alias or the derived ID of an expired subject is free again.
func TestScheduleOfAnExpiredSubject(t *testing.T) {
	ctx := context.Background()
	end := t0.Add(subject.RestoreWindow)

	t.Run("name", func(t *testing.T) {
		e, hist := scheduleEnv(t)
		withExpired(e)
		old := createSubject(t, e, "Физика")
		discard(t, e, old)
		e.clock.now = end
		p := propose(t, e)
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || len(res.Skipped) != 0 || len(res.Created) != 2 || len(res.Aliased) != 1 || res.Aliased[0].Subject != hist.ID() {
			t.Fatalf("CreateFromSchedule = %+v, %v; want everything done", res, err)
		}
	})

	t.Run("alias", func(t *testing.T) {
		e, hist := scheduleEnv(t)
		withExpired(e)
		old := createSubject(t, e, "Древняя история", "История (лек.)")
		discard(t, e, old)
		e.clock.now = end
		p := propose(t, e)
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || len(res.Skipped) != 0 || !reflect.DeepEqual(res.Aliased, []ScheduleAliased{{Subject: hist.ID(), Aliases: []string{"История (лек.)"}}}) {
			t.Fatalf("CreateFromSchedule = %+v, %v; want the alias added", res, err)
		}
	})

	t.Run("own subject", func(t *testing.T) {
		e, _ := scheduleEnv(t)
		withExpired(e)
		p := propose(t, e)
		phys := groupNamed(t, p, "Физика")
		skipOthers := []schedulesubjects.Choice{
			{Group: groupNamed(t, p, "Матанализ").Key, Action: schedulesubjects.GroupSkip},
			{Group: groupNamed(t, p, "История").Key, Action: schedulesubjects.GroupSkip},
		}
		first, err := e.svc.CreateFromSchedule(ctx, p.Token, skipOthers)
		if err != nil || len(first.Created) != 1 {
			t.Fatalf("CreateFromSchedule(Физика) = %+v, %v", first, err)
		}
		physID := first.Created[0].ID
		got, err := e.subjects.Get(ctx, physID)
		if err != nil {
			t.Fatal(err)
		}
		discard(t, e, got)

		e.clock.now = end
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, skipOthers)
		want := []ScheduleCreated{{ID: physID, Key: phys.Key, Name: "Физика"}}
		if err != nil || len(res.Skipped) != 0 || !reflect.DeepEqual(res.Created, want) {
			t.Fatalf("CreateFromSchedule = %+v, %v; want %s made again", res, err, physID)
		}
		if s, err := e.subjects.Get(ctx, physID); err != nil || !s.Active() {
			t.Fatalf("the subject made again = %v, %v", s, err)
		}
	})
}

func TestRemovedSubjects(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	createSubject(t, e, "Химия") // active
	expired := createSubject(t, e, "История")
	discard(t, e, expired) // at t0
	first := createSubject(t, e, "Физика")
	e.clock.now = t0.Add(2 * time.Hour)
	discard(t, e, first)
	second := createSubject(t, e, "Механика")
	e.clock.now = t0.Add(3 * time.Hour)
	discard(t, e, second)

	e.clock.now = t0.Add(subject.RestoreWindow) // the window of «История» has just ended
	list, err := e.svc.RemovedSubjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id               shared.SubjectID
		removed, purgeAt time.Time
	}
	var got []row
	for _, r := range list {
		got = append(got, row{r.Subject.ID(), r.RemovedAt, r.PurgeAt})
	}
	want := []row{
		{second.ID(), t0.Add(3 * time.Hour), t0.Add(3*time.Hour + subject.RestoreWindow)},
		{first.ID(), t0.Add(2 * time.Hour), t0.Add(2*time.Hour + subject.RestoreWindow)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RemovedSubjects = %+v\nwant %+v", got, want)
	}

	e.subjects.err = errors.New("disk")
	if _, err := e.svc.RemovedSubjects(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RemovedSubjects = %v, want ErrUnavailable", err)
	}
}
