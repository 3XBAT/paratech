package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// removalLibrary is s1 «Физика» (alias «Физ», own folder f1) and s2 «Химия»; m1 and m3 in «Материалы» of s1, m2 in
// f1, m4 in «Входящие»; note n1 of s1 (m1, anchored to recording r1 and lesson l1 through two notes) and n2 of s2
// with m3, a file of s1 (moved there after it was added).
func removalLibrary(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	subjects, materials, notes := NewSubjectRepository(db), NewMaterialRepository(db), NewNoteRepository(db)

	phys := newLibSubject(t, "s1", "Физика", "Физ")
	if _, err := phys.AddFolder("f1", libFolderName(t, "Своя"), libT0); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*subject.Subject{phys, newLibSubject(t, "s2", "Химия")} {
		if err := subjects.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	matFolder := subject.SystemFolderID("s1", subject.SystemMaterials)
	for _, m := range []struct {
		id     shared.MaterialID
		sub    shared.SubjectID
		folder shared.FolderID
	}{{"m1", "s1", matFolder}, {"m2", "s1", "f1"}, {"m3", "s1", matFolder}, {"m4", "", ""}} {
		mat := newLibMaterial(t, m.id, "content of "+string(m.id))
		if m.sub != "" {
			if _, err := mat.Place(m.sub, m.folder, libT0); err != nil {
				t.Fatal(err)
			}
		}
		if err := materials.Save(ctx, mat); err != nil {
			t.Fatal(err)
		}
	}

	rec := noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: noteDate(t, "2026-09-12")})
	lesson := noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l1", Date: noteDate(t, "2026-09-13")})
	day := noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorDay, Date: noteDate(t, "2026-09-14")})
	for _, n := range []*note.Note{
		newLibNote(t, "n1", "s1", rec, "m1"), newLibNote(t, "n1b", "s1", lesson, "m1"), newLibNote(t, "n2", "s2", day, "m3"),
	} {
		if err := notes.Save(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
}

// setRemoved removes (at) or restores (zero at) s1 through the aggregate and the repository.
func setRemoved(t *testing.T, repo *SubjectRepository, at time.Time) *subject.Subject {
	t.Helper()
	ctx := context.Background()
	s, err := repo.GetAny(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if at.IsZero() {
		_, err = s.Restore(libT0.Add(2 * time.Hour))
	} else {
		_, err = s.Remove(at)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return s
}

func TestRemovedSubjectIsHiddenWithItsNotesAndFiles(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	removalLibrary(t, db)
	subjects, materials, notes := NewSubjectRepository(db), NewMaterialRepository(db), NewNoteRepository(db)
	rowsBefore := scanInt(t, db.sql, "SELECT count(*) FROM materials WHERE subject_id = 's1'") +
		scanInt(t, db.sql, "SELECT count(*) FROM notes WHERE subject_id = 's1'")

	removedAt := libT0.Add(time.Hour)
	setRemoved(t, subjects, removedAt)

	// The subject: only GetAny and ListRemoved see it, with its name, alias, folders and removal time.
	if list, err := subjects.List(ctx); err != nil || len(list) != 1 || list[0].ID() != "s2" {
		t.Fatalf("List = %v, %v; want only s2", list, err)
	}
	if _, err := subjects.Get(ctx, "s1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get(removed) = %v, want ErrNotFound", err)
	}
	gone, err := subjects.ListRemoved(ctx)
	if err != nil || len(gone) != 1 || gone[0].ID() != "s1" || !gone[0].RemovedAt().Equal(removedAt) ||
		len(gone[0].Aliases()) != 1 || len(gone[0].Folders()) != len(subject.Systems())+1 {
		t.Fatalf("ListRemoved = %v, %v", gone, err)
	}
	if s, err := subjects.GetAny(ctx, "s1"); err != nil || s.Status() != subject.StatusRemoved || s.Version() != 2 {
		t.Fatalf("GetAny(removed) = %v, %v", s, err)
	}

	// The files: hidden, also the one in the own folder; m3 stays visible as a part of the active note of s2.
	for _, id := range []shared.MaterialID{"m1", "m2"} {
		if _, err := materials.Get(ctx, id); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("Get(%s) = %v, want ErrNotFound", id, err)
		}
	}
	for _, id := range []shared.MaterialID{"m3", "m4"} {
		if _, err := materials.Get(ctx, id); err != nil {
			t.Fatalf("Get(%s) = %v, want it visible", id, err)
		}
	}
	if list, err := materials.ListIn(ctx, "s1"); err != nil || len(list) != 0 {
		t.Fatalf("ListIn(removed) = %d, %v", len(list), err)
	}
	if m, err := materials.GetByDigest(ctx, material.DigestOf([]byte("content of m2"))); err != nil || m.ID() != "m2" {
		t.Fatalf("GetByDigest(hidden) = %v, %v; want it found for the dedupe", m, err)
	}

	// The notes of s1: hidden from every read.
	if _, err := notes.Get(ctx, "n1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get(note of a removed subject) = %v", err)
	}
	if list, err := notes.ListBySubject(ctx, "s1"); err != nil || len(list) != 0 {
		t.Fatalf("ListBySubject = %d, %v", len(list), err)
	}
	if list, err := notes.ListByRecording(ctx, "r1"); err != nil || len(list) != 0 {
		t.Fatalf("ListByRecording = %d, %v", len(list), err)
	}
	if list, err := notes.ListByLesson(ctx, "l1"); err != nil || len(list) != 0 {
		t.Fatalf("ListByLesson = %d, %v", len(list), err)
	}
	if all, _, err := notes.ListAll(ctx); err != nil || len(all) != 1 || all[0].ID() != "n2" {
		t.Fatalf("ListAll = %v, %v; want only n2", all, err)
	}

	// A removed note of s2 no longer keeps m3 visible.
	n2, err := notes.Get(ctx, "n2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n2.Remove(libT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := notes.Save(ctx, n2); err != nil {
		t.Fatal(err)
	}
	if _, err := materials.Get(ctx, "m3"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get(m3) with its note removed = %v, want ErrNotFound", err)
	}

	// No row under the subject was rewritten.
	if n := scanInt(t, db.sql, "SELECT count(*) FROM materials WHERE subject_id = 's1'") +
		scanInt(t, db.sql, "SELECT count(*) FROM notes WHERE subject_id = 's1'"); n != rowsBefore {
		t.Fatalf("%d rows under s1 after the removal, want %d", n, rowsBefore)
	}

	// Restored, everything is back as it was.
	back := setRemoved(t, subjects, time.Time{})
	if !back.Active() || back.Version() != 3 {
		t.Fatalf("restored %v v%d", back.Status(), back.Version())
	}
	if s, err := subjects.Get(ctx, "s1"); err != nil || !s.RemovedAt().IsZero() || len(s.Aliases()) != 1 {
		t.Fatalf("Get(restored) = %v, %v", s, err)
	}
	if list, err := materials.ListIn(ctx, "s1"); err != nil || len(list) != 3 {
		t.Fatalf("ListIn(restored) = %d, %v", len(list), err)
	}
	if m, err := materials.Get(ctx, "m2"); err != nil || m.Folder() != "f1" {
		t.Fatalf("Get(m2) after restore = %v, %v", m, err)
	}
	if list, err := notes.ListByLesson(ctx, "l1"); err != nil || len(list) != 1 {
		t.Fatalf("ListByLesson after restore = %d, %v", len(list), err)
	}
	if list, err := subjects.ListRemoved(ctx); err != nil || len(list) != 0 {
		t.Fatalf("ListRemoved after restore = %v, %v", list, err)
	}
}

func TestRemovedSubjectKeepsItsNameInTheStore(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	removalLibrary(t, db)
	subjects := NewSubjectRepository(db)
	setRemoved(t, subjects, libT0.Add(time.Hour))

	if err := subjects.Save(ctx, newLibSubject(t, "s3", "физика")); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("Save(name of a removed subject) = %v, want ErrDuplicate", err)
	}

	never := newLibSubject(t, "s4", "Биология")
	if _, err := never.Remove(libT0); err != nil {
		t.Fatal(err)
	}
	if err := subjects.Save(ctx, never); err == nil {
		t.Fatal("Save of a removed subject never stored succeeded")
	}
}

// Version 49 adds subjects.removed_at: a subject of version 48 stays active, and it goes back down to 48.
func TestMigrationAddsSubjectRemoval(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, subjectRemovalVersion-1); err != nil {
		t.Fatalf("up to %d: %v", subjectRemovalVersion-1, err)
	}
	mustExec(t, db, `INSERT INTO subjects (id, name, name_key, semester, version, created_at, updated_at)
		VALUES ('s1', 'Физика', 'физика', '2026-autumn', 1, 5, 5)`)

	if _, err := p.UpTo(ctx, subjectRemovalVersion); err != nil {
		t.Fatalf("migrate to %d: %v", subjectRemovalVersion, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects WHERE removed_at IS NULL"); n != 1 {
		t.Fatal("the stored subject is not active after the migration")
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE subjects SET removed_at = 0"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("removed_at = 0: err = %v, want ErrConstraint", err)
	}
	mustExec(t, db, "UPDATE subjects SET removed_at = 7")

	if _, err := p.DownTo(ctx, subjectRemovalVersion-1); err != nil {
		t.Fatalf("down to %d: %v", subjectRemovalVersion-1, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('subjects') WHERE name = 'removed_at'"); n != 0 {
		t.Fatal("subjects.removed_at still exists after down")
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects"); n != 1 {
		t.Fatal("the subject was lost going down")
	}
}
