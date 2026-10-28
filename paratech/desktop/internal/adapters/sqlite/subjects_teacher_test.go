package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Fake teachers: invented names and a reserved domain.
const (
	lectureName, lectureEmail   = "Петров П. П.", "petrov@example.invalid"
	practiceName, practiceEmail = "Иванова А. С.", "ivanova@example.invalid"
	labName, labEmail           = "Сидоров С. С.", "sidorov@example.invalid"
)

func setTeacher(t *testing.T, s *subject.Subject, role subject.TeacherRole, name, email string, at time.Time) {
	t.Helper()
	if _, err := s.SetTeacher(role, name, email, at); err != nil {
		t.Fatal(err)
	}
}

// The teachers of a subject are stored on insert and on update, read back by Get and List, and cleared role by role.
func TestSubjectTeachersRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)

	s := newLibSubject(t, "s1", "Физика")
	setTeacher(t, s, subject.TeacherLecture, lectureName, lectureEmail, libT0)
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, "s1")
	if err != nil || !reflect.DeepEqual(got.State(), s.State()) || got.Teacher(subject.TeacherLecture).Email() != lectureEmail {
		t.Fatalf("Get = %v, %v", got, err)
	}

	t1 := libT0.Add(time.Hour)
	setTeacher(t, got, subject.TeacherLab, labName, labEmail, t1)
	setTeacher(t, got, subject.TeacherPractice, practiceName, practiceEmail, t1)
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0].State(), got.State()) || list[0].Version() != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if roles := list[0].Teachers().Roles(); len(roles) != 3 {
		t.Fatalf("roles = %v", roles)
	}

	if changed, err := list[0].ClearTeacher(subject.TeacherLecture, t1.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("ClearTeacher = %v, %v", changed, err)
	}
	if err := repo.Save(ctx, list[0]); err != nil {
		t.Fatalf("Save of the clear: %v", err)
	}
	cleared, err := repo.Get(ctx, "s1")
	if err != nil || !cleared.Teacher(subject.TeacherLecture).IsZero() || cleared.Teacher(subject.TeacherLab).Email() != labEmail ||
		cleared.Version() != 3 {
		t.Fatalf("after clear: teachers %v, version %d, %v", cleared.Teachers(), cleared.Version(), err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE teacher_name <> '' OR teacher_email <> ''"); n != 0 {
		t.Fatalf("%d subjects use the legacy columns", n)
	}
}

// A save at a stale version changes neither the subject nor its teachers: they are written under the version check.
func TestSubjectTeachersSaveUnderVersion(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	if err := repo.Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	first, err := repo.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	setTeacher(t, first, subject.TeacherLecture, lectureName, lectureEmail, libT0)
	if err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	setTeacher(t, second, subject.TeacherLab, labName, labEmail, libT0)
	if err := repo.Save(ctx, second); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v, want ErrVersionConflict", err)
	}
	stored, err := repo.Get(ctx, "s1")
	if err != nil || stored.Teachers() != first.Teachers() || stored.Version() != 2 {
		t.Fatalf("stored %v at version %d, %v", stored.Teachers(), stored.Version(), err)
	}
}

// Removing a subject removes its teachers (foreign keys are on in every connection); the table refuses an unknown
// role, an empty name or address and a second row of a role.
func TestSubjectTeachersTable(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	s := newLibSubject(t, "s1", "Физика")
	for _, r := range subject.TeacherRoles() {
		setTeacher(t, s, r, lectureName, lectureEmail, libT0)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, newLibSubject(t, "s2", "Химия")); err != nil {
		t.Fatal(err)
	}

	const insert = `INSERT INTO subject_teachers (subject_id, role, name, email) VALUES (?, ?, ?, ?)`
	tests := []struct {
		label                     string
		subject, role, name, mail string
	}{
		{"unknown role", "s2", "seminar", "x", "a@b.c"},
		{"empty name", "s2", "lab", "", "a@b.c"},
		{"empty email", "s2", "lab", "x", ""},
		{"long name", "s2", "lab", strings.Repeat("я", 201), "a@b.c"},
		{"role twice", "s1", "lab", "x", "a@b.c"},
		{"unknown subject", "nope", "lab", "x", "a@b.c"},
	}
	for _, tt := range tests {
		if _, err := db.sql.ExecContext(ctx, insert, tt.subject, tt.role, tt.name, tt.mail); err == nil {
			t.Errorf("%s: no constraint failure", tt.label)
		}
	}
	mustExec(t, db, insert, "s2", "lab", "x", "a@b.c")
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_teachers"); n != 4 {
		t.Fatalf("teacher rows = %d, want 4", n)
	}

	stored, err := repo.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_teachers WHERE subject_id = 's1'"); n != 0 {
		t.Fatalf("teacher rows after the subject was removed = %d", n)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_teachers WHERE subject_id = 's2'"); n != 1 {
		t.Fatalf("teacher rows of another subject = %d, want 1", n)
	}
}

// The legacy columns still refuse a name without an address (or the other way round): the migration blanks both at
// once.
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

// Migration 0044 on real SQLite: the one teacher of a subject goes into all three roles and the legacy columns are
// blanked; a subject without one gets no rows and its row stays as it was. Down puts back one teacher (lecture, else
// practice, else lab) and up again repeats the move.
func TestMigrationSubjectTeachers(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, nil)
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSubjectRepository(db)

	// Saved at the newest schema, so the subjects have their folders; down to 0043 then gives the legacy teacher.
	all := newLibSubject(t, "all", "Физика")
	setTeacher(t, all, subject.TeacherLecture, lectureName, lectureEmail, libT0)
	setTeacher(t, all, subject.TeacherPractice, practiceName, practiceEmail, libT0)
	setTeacher(t, all, subject.TeacherLab, labName, labEmail, libT0)
	practiceLab := newLibSubject(t, "pl", "Химия")
	setTeacher(t, practiceLab, subject.TeacherPractice, practiceName, practiceEmail, libT0)
	setTeacher(t, practiceLab, subject.TeacherLab, labName, labEmail, libT0)
	labOnly := newLibSubject(t, "lab", "Биология")
	setTeacher(t, labOnly, subject.TeacherLab, labName, labEmail, libT0)
	none := newLibSubject(t, "none", "История")
	for _, s := range []*subject.Subject{all, practiceLab, labOnly, none} {
		if err := repo.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := p.DownTo(ctx, subjectTeachersVersion-1); err != nil {
		t.Fatalf("down to %d: %v", subjectTeachersVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM sqlite_master WHERE name = 'subject_teachers'"); n != 0 {
		t.Fatal("subject_teachers exists after down")
	}
	legacy := map[string][2]string{
		"all": {lectureName, lectureEmail}, "pl": {practiceName, practiceEmail}, "lab": {labName, labEmail}, "none": {"", ""},
	}
	for id, want := range legacy {
		var name, email string
		if err := db.sql.QueryRowContext(ctx, "SELECT teacher_name, teacher_email FROM subjects WHERE id = ?", id).Scan(&name, &email); err != nil {
			t.Fatal(err)
		}
		if name != want[0] || email != want[1] {
			t.Errorf("%s after down: legacy teacher %q %q, want %q", id, name, email, want)
		}
	}

	rowsOf := func() map[string]string {
		rows, err := db.sql.QueryContext(ctx, "SELECT id, version, updated_at, name, semester FROM subjects ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		out := map[string]string{}
		for rows.Next() {
			var id, name, sem string
			var version, updated int64
			if err := rows.Scan(&id, &version, &updated, &name, &sem); err != nil {
				t.Fatal(err)
			}
			out[id] = fmt.Sprint(version, updated, name, sem)
		}
		return out
	}
	before := rowsOf()

	if _, err := p.UpTo(ctx, subjectTeachersVersion); err != nil {
		t.Fatalf("up to %d: %v", subjectTeachersVersion, err)
	}
	if got := rowsOf(); !reflect.DeepEqual(got, before) {
		t.Fatalf("subject rows changed: %v, want %v", got, before)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE teacher_name <> '' OR teacher_email <> ''"); n != 0 {
		t.Fatalf("%d subjects keep the legacy teacher", n)
	}
	for id, want := range legacy {
		n := scanInt(t, db.sql, "SELECT count(*) FROM subject_teachers WHERE subject_id = ?", id)
		same := scanInt(t, db.sql, "SELECT count(DISTINCT role) FROM subject_teachers WHERE subject_id = ? AND name = ? AND email = ?",
			id, want[0], want[1])
		switch {
		case want[0] == "" && n != 0:
			t.Errorf("%s: %d teacher rows, want none", id, n)
		case want[0] != "" && (n != 3 || same != 3):
			t.Errorf("%s: %d teacher rows, %d with the legacy teacher; want 3", id, n, same)
		}
	}

	// The repository reads the moved teachers: the one teacher in every role.
	got, err := repo.Get(ctx, "pl")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range subject.TeacherRoles() {
		if tch := got.Teacher(r); tch.Name() != practiceName || tch.Email() != practiceEmail {
			t.Errorf("role %s after the move: %v", r, tch)
		}
	}
	if none, err := repo.Get(ctx, "none"); err != nil || !none.Teachers().IsZero() {
		t.Fatalf("subject without a teacher: %v, %v", none, err)
	}

	// Down and up again keep the one teacher.
	if _, err := p.DownTo(ctx, subjectTeachersVersion-1); err != nil {
		t.Fatal(err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE id = 'pl' AND teacher_name = ? AND teacher_email = ?",
		practiceName, practiceEmail); n != 1 {
		t.Fatal("second down lost the teacher")
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_teachers"); n != 9 {
		t.Fatalf("teacher rows after up again = %d, want 9", n)
	}
}
