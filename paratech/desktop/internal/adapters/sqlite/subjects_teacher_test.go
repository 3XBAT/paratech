package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// The teacher of a subject is stored on insert and on update, read back by Get and List, and cleared.
func TestSubjectTeacherRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)

	s := newLibSubject(t, "s1", "Физика")
	if _, err := s.SetTeacher("Петров П. П.", "petrov@example.invalid", libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "s1")
	if err != nil || !reflect.DeepEqual(got.State(), s.State()) || got.Teacher().Email() != "petrov@example.invalid" {
		t.Fatalf("Get = %+v, %v\nwant %+v", got.State(), err, s.State())
	}

	t1 := libT0.Add(time.Hour)
	if _, err := got.SetTeacher("Иванова А. С.", "ivanova@example.invalid", t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0].State(), got.State()) || list[0].Version() != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}

	if !list[0].ClearTeacher(t1.Add(time.Hour)) {
		t.Fatal("ClearTeacher changed nothing")
	}
	if err := repo.Save(ctx, list[0]); err != nil {
		t.Fatalf("Save of the clear: %v", err)
	}
	cleared, err := repo.Get(ctx, "s1")
	if err != nil || !cleared.Teacher().IsZero() || cleared.Version() != 3 {
		t.Fatalf("after clear: teacher %v, version %d, %v", cleared.Teacher(), cleared.Version(), err)
	}
}

// The table refuses a name without an address (or the other way round): the columns hold both or neither.
func TestSubjectTeacherColumnsGoTogether(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewSubjectRepository(db).Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{
		`UPDATE subjects SET teacher_name = 'Петров' WHERE id = 's1'`,
		`UPDATE subjects SET teacher_email = 'petrov@example.invalid' WHERE id = 's1'`,
	} {
		if _, err := db.sql.ExecContext(ctx, set); err == nil {
			t.Errorf("%s: no CHECK failure", set)
		}
	}
}
