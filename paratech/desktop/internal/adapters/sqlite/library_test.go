package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var libT0 = time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)

func newLibSubject(t *testing.T, id shared.SubjectID, name string, aliases ...string) *subject.Subject {
	t.Helper()
	s, err := subject.New(subject.Spec{ID: id, Name: name, Aliases: aliases, Semester: values.SemesterOf(libT0)}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newLibMaterial(t *testing.T, id shared.MaterialID, content string) *material.Material {
	t.Helper()
	m, err := material.New(material.Spec{
		ID: id, Digest: material.DigestOf([]byte(content)), Size: int64(len(content)), Type: material.TypeTXT, Name: "notes.txt",
	}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func libFolderName(t *testing.T, s string) subject.FolderName {
	t.Helper()
	n, err := subject.NewFolderName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubjectSaveGetListDelete(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	s := newLibSubject(t, "s1", "Физика", "Общая физика")
	if _, err := s.AddFolder("f1", libFolderName(t, "Конспекты"), libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.Version() != 1 {
		t.Fatalf("version after insert = %d", s.Version())
	}
	got, err := repo.Get(ctx, "s1")
	if err != nil || !reflect.DeepEqual(got.State(), s.State()) {
		t.Fatalf("Get = %+v, %v\nwant %+v", got.State(), err, s.State())
	}

	// An alias added, an own folder removed and another added: the order of the folders is kept.
	t1 := libT0.Add(time.Hour)
	if _, err := got.AddAlias("Физ", t1); err != nil {
		t.Fatal(err)
	}
	if err := got.RemoveFolder("f1", 0, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := got.AddFolder("f2", libFolderName(t, "Задачи"), t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	again, err := repo.Get(ctx, "s1")
	if err != nil || !reflect.DeepEqual(again.State(), got.State()) || again.Version() != 2 {
		t.Fatalf("after the change: %+v, %v", again.State(), err)
	}

	if err := repo.Save(ctx, newLibSubject(t, "s2", "Химия")); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID() != "s1" || list[1].ID() != "s2" {
		t.Fatalf("List = %v, %v", list, err)
	}
	if !reflect.DeepEqual(list[0].State(), again.State()) {
		t.Fatalf("List[0] = %+v, want %+v", list[0].State(), again.State())
	}

	// A stale copy is refused; the current one is deleted with its folders and aliases.
	if err := repo.Delete(ctx, s); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Delete(stale) = %v", err)
	}
	if err := repo.Delete(ctx, again); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "s1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_folders WHERE subject_id = 's1'"); n != 0 {
		t.Fatalf("%d folders left", n)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subject_aliases WHERE subject_id = 's1'"); n != 0 {
		t.Fatalf("%d aliases left", n)
	}
}

func TestSubjectSaveConflicts(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewSubjectRepository(db)
	if err := repo.Save(ctx, newLibSubject(t, "s1", "Физика", "Матан")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		s    *subject.Subject
		want error
	}{
		{"same id", newLibSubject(t, "s1", "Химия"), shared.ErrVersionConflict},
		{"same name key", newLibSubject(t, "s2", "ФИЗИКА"), shared.ErrDuplicate},
		{"same alias key", newLibSubject(t, "s3", "Матанализ", "матан"), shared.ErrDuplicate},
	}
	for _, tt := range tests {
		if err := repo.Save(ctx, tt.s); !errors.Is(err, tt.want) {
			t.Errorf("%s: Save = %v, want %v", tt.name, err, tt.want)
		}
	}
	// Nothing of a refused insert is kept.
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects"); n != 1 {
		t.Fatalf("%d subjects", n)
	}

	stale := newLibSubject(t, "s1", "Физика")
	stale.Persisted(7)
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Save(stale) = %v", err)
	}
}

func TestMaterialSaveGetAndPlaces(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	subjects, repo := NewSubjectRepository(db), NewMaterialRepository(db)
	if err := NewLessonRepository(db).Save(ctx, newTestLesson(t, "l1", "", 0)); err != nil {
		t.Fatal(err)
	}
	s := newLibSubject(t, "s1", "Физика")
	if err := subjects.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	m := newLibMaterial(t, "m1", "hello")
	if err := repo.Save(ctx, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByDigest(ctx, material.DigestOf([]byte("hello")))
	if err != nil || !reflect.DeepEqual(got.State(), m.State()) {
		t.Fatalf("GetByDigest = %+v, %v", got, err)
	}
	if inbox, err := repo.ListIn(ctx, ""); err != nil || len(inbox) != 1 {
		t.Fatalf("inbox = %v, %v", inbox, err)
	}

	lectures := subject.SystemFolderID("s1", subject.SystemLectures)
	if _, err := got.Place("s1", lectures, libT0); err != nil {
		t.Fatal(err)
	}
	if _, err := got.Link("l1", libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save placed: %v", err)
	}
	again, err := repo.Get(ctx, "m1")
	if err != nil || !reflect.DeepEqual(again.State(), got.State()) || again.Version() != 2 {
		t.Fatalf("Get = %+v, %v", again, err)
	}
	if list, err := repo.ListIn(ctx, "s1"); err != nil || len(list) != 1 {
		t.Fatalf("ListIn(s1) = %v, %v", list, err)
	}
	if inbox, err := repo.ListIn(ctx, ""); err != nil || len(inbox) != 0 {
		t.Fatalf("inbox after placing = %v, %v", inbox, err)
	}
	for _, c := range []struct {
		folder shared.FolderID
		want   int
	}{{"", 1}, {lectures, 1}, {subject.SystemFolderID("s1", subject.SystemTickets), 0}} {
		if n, err := repo.Count(ctx, "s1", c.folder); err != nil || n != c.want {
			t.Errorf("Count(s1, %q) = %d, %v; want %d", c.folder, n, err, c.want)
		}
	}

	// The subject and the folder that hold the file cannot be deleted, also while it is removed.
	if _, err := again.Remove(libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, again); err != nil {
		t.Fatal(err)
	}
	if err := subjects.Delete(ctx, s); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Delete of a subject holding a file = %v", err)
	}

	// A purged lesson takes its link with it.
	mustExec(t, db, "DELETE FROM schedule_entries WHERE id = 'l1'")
	if back, err := repo.Get(ctx, "m1"); err != nil || len(back.Lessons()) != 0 {
		t.Fatalf("links after the purge: %v, %v", back, err)
	}
}

func TestMaterialSaveConflicts(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewMaterialRepository(db)
	if err := NewSubjectRepository(db).Save(ctx, newLibSubject(t, "s1", "Физика")); err != nil {
		t.Fatal(err)
	}
	if err := NewSubjectRepository(db).Save(ctx, newLibSubject(t, "s2", "Химия")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, newLibMaterial(t, "m1", "a")); err != nil {
		t.Fatal(err)
	}

	place := func(m *material.Material, s shared.SubjectID, f shared.FolderID) *material.Material {
		if _, err := m.Place(s, f, libT0); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name string
		m    *material.Material
		want error
	}{
		{"same id", newLibMaterial(t, "m1", "b"), shared.ErrVersionConflict},
		{"same sha256", newLibMaterial(t, "m2", "a"), shared.ErrDuplicate},
		{"unknown subject", place(newLibMaterial(t, "m3", "c"), "nope", "nope-lectures"), shared.ErrConflict},
		{"folder of another subject", place(newLibMaterial(t, "m4", "d"), "s1", subject.SystemFolderID("s2", subject.SystemLectures)), shared.ErrConflict},
	}
	for _, tt := range tests {
		if err := repo.Save(ctx, tt.m); !errors.Is(err, tt.want) {
			t.Errorf("%s: Save = %v, want %v", tt.name, err, tt.want)
		}
	}
	linked := newLibMaterial(t, "m5", "e")
	if _, err := linked.Link("no-such-lesson", libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, linked); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Save with an unknown lesson = %v", err)
	}
	if _, err := repo.Get(ctx, "m5"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the refused material was kept: %v", err)
	}

	stale := newLibMaterial(t, "m1", "a")
	stale.Persisted(5)
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Save(stale) = %v", err)
	}
}

// SaveText writes the text status under the version without a new one; Save after it neither bumps it back nor
// overwrites the status with the one it loaded (TASK-122: inspecting a file must not break the Undo of placing it).
func TestMaterialSaveText(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewMaterialRepository(db)
	if err := repo.Save(ctx, newLibMaterial(t, "m1", "a")); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Get(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, "m1") // loaded before the text was read
	if err != nil {
		t.Fatal(err)
	}

	if _, err := loaded.SetText(material.TextExtracted); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveText(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "m1")
	if err != nil || got.Version() != 1 || got.TextStatus() != material.TextExtracted {
		t.Fatalf("after SaveText: %+v, %v", got.State(), err)
	}

	if _, err := stale.Remove(libT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, stale); err != nil {
		t.Fatalf("Save after SaveText = %v", err)
	}
	got, err = repo.Get(ctx, "m1")
	if err != nil || got.Version() != 2 || got.Active() || got.TextStatus() != material.TextExtracted {
		t.Fatalf("after Save: %+v, %v", got.State(), err)
	}

	tests := []struct {
		name     string
		id       shared.MaterialID
		version  int64
		conflict bool
	}{
		{"stale version", "m1", 1, true},
		{"missing material", "m9", 1, true},
		{"never stored", "m1", 0, false},
	}
	for _, tt := range tests {
		m := newLibMaterial(t, tt.id, "a")
		m.Persisted(tt.version)
		err := repo.SaveText(ctx, m)
		if err == nil || errors.Is(err, shared.ErrVersionConflict) != tt.conflict {
			t.Errorf("%s: SaveText = %v", tt.name, err)
		}
	}
}

// A removed folder that holds a file is refused by the database even if the aggregate let it go.
func TestSubjectSaveKeepsAFolderHoldingAFile(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	subjects := NewSubjectRepository(db)
	s := newLibSubject(t, "s1", "Физика")
	if _, err := s.AddFolder("f1", libFolderName(t, "Своя"), libT0); err != nil {
		t.Fatal(err)
	}
	if err := subjects.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	m := newLibMaterial(t, "m1", "x")
	if _, err := m.Place("s1", "f1", libT0); err != nil {
		t.Fatal(err)
	}
	if err := NewMaterialRepository(db).Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFolder("f1", 0, libT0); err != nil { // a stale count
		t.Fatal(err)
	}
	if err := subjects.Save(ctx, s); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Save = %v, want a conflict", err)
	}
	if got, err := subjects.Get(ctx, "s1"); err != nil || len(got.Folders()) != len(subject.Systems())+1 {
		t.Fatalf("folders after the refused save: %v, %v", got, err)
	}
}

func TestSubjectStrings(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	const lesson = `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, subject, deleted_at, created_at, updated_at)
		VALUES (?, 'x', 1, 2, 'UTC', ?, ?, 1, 1)`
	mustExec(t, db, lesson, "l1", "Физика", nil)
	mustExec(t, db, lesson, "l2", "Физика", nil)
	mustExec(t, db, lesson, "l3", "История", 5) // deleted
	mustExec(t, db, lesson, "l4", "", nil)
	const rec = `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, lesson_subject, user_subject,
		deleting_at, created_at, updated_at) VALUES (?, 'FAILED', 1, 1000, 'recordings/x', ?, ?, ?, 1, 1)`
	mustExec(t, db, rec, "r1", "Физика", nil, nil)
	mustExec(t, db, rec, "r2", "Физика", "Химия", nil) // the correction wins
	mustExec(t, db, rec, "r3", "Биология", nil, 9)     // marked for deletion
	mustExec(t, db, rec, "r4", nil, nil, nil)

	got, err := NewSubjectStrings(db).ListSubjectStrings(ctx)
	want := []subjectmatch.Candidate{
		{Name: "Физика", Lessons: 2, Recordings: 1},
		{Name: "Химия", Recordings: 1},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListSubjectStrings = %+v, %v; want %+v", got, err, want)
	}
}
