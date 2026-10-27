package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

func noteAnchorOf(t *testing.T, spec note.AnchorSpec) note.Anchor {
	t.Helper()
	a, err := note.NewAnchor(spec)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func noteDate(t *testing.T, s string) note.Date {
	t.Helper()
	d, err := note.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newLibNote(t *testing.T, id shared.NoteID, sub shared.SubjectID, a note.Anchor, parts ...shared.MaterialID) *note.Note {
	t.Helper()
	n, err := note.New(note.Spec{ID: id, Subject: sub, Anchor: a, Title: "Конспект " + string(id), Parts: parts}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// seedNoteLibrary stores subjects s1 and s2 and materials m1…m3.
func seedNoteLibrary(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	subjects, materials := NewSubjectRepository(db), NewMaterialRepository(db)
	for _, s := range []struct {
		id   shared.SubjectID
		name string
	}{{"s1", "Физика"}, {"s2", "Химия"}} {
		if err := subjects.Save(ctx, newLibSubject(t, s.id, s.name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []shared.MaterialID{"m1", "m2", "m3"} {
		if err := materials.Save(ctx, newLibMaterial(t, m, "content of "+string(m))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoteSaveGetAndChange(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	seedNoteLibrary(t, db)
	repo := NewNoteRepository(db)

	rec := noteAnchorOf(t, note.AnchorSpec{
		Kind: note.AnchorRecording, Recording: "r1", Date: noteDate(t, "2026-09-12"), LessonKind: "lecture", Title: "Механика",
	})
	n := newLibNote(t, "n1", "s1", rec, "m2", "m1")
	if err := repo.Save(ctx, n); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if n.Version() != 1 {
		t.Fatalf("version after insert = %d", n.Version())
	}
	got, err := repo.Get(ctx, "n1")
	if err != nil || !reflect.DeepEqual(got.State(), n.State()) {
		t.Fatalf("Get = %+v, %v\nwant %+v", got.State(), err, n.State())
	}

	// Every field that can change is written: parts in the new order, the anchor, the title and the status.
	t1 := libT0.Add(time.Hour)
	for _, change := range []func() (bool, error){
		func() (bool, error) { return got.AddParts([]shared.MaterialID{"m3"}, t1) },
		func() (bool, error) { return got.Reorder([]shared.MaterialID{"m3", "m1", "m2"}, t1) },
		func() (bool, error) { return got.ReplacePart("m1", "m1", t1) },
		func() (bool, error) { return got.Reanchor(got.Anchor().FallBackToDay(), t1) },
		func() (bool, error) { return got.Retitle("Новое название", t1) },
		func() (bool, error) { return got.Remove(t1) },
	} {
		if _, err := change(); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("Save of a change: %v", err)
	}
	again, err := repo.Get(ctx, "n1")
	if err != nil || !reflect.DeepEqual(again.State(), got.State()) || again.Version() != 2 {
		t.Fatalf("after the change: %+v, %v\nwant %+v", again.State(), err, got.State())
	}
	if again.Anchor().Kind() != note.AnchorDay || again.Anchor().Title() != "Механика" || again.Active() {
		t.Fatalf("anchor %+v, status %s", again.Anchor(), again.Status())
	}

	// An undated note stores no date.
	undated := newLibNote(t, "n2", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorUndated, Title: "Без даты"}), "m1")
	if err := repo.Save(ctx, undated); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "n2"); err != nil || !reflect.DeepEqual(got.State(), undated.State()) {
		t.Fatalf("Get(undated) = %+v, %v", got, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM notes WHERE id = 'n2' AND anchor_date IS NULL"); n != 1 {
		t.Fatalf("undated note has a date")
	}

	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get(missing) = %v", err)
	}
}

func TestNoteSaveConflicts(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	seedNoteLibrary(t, db)
	repo := NewNoteRepository(db)
	day := noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorDay, Date: noteDate(t, "2026-09-12")})
	if err := repo.Save(ctx, newLibNote(t, "n1", "s1", day, "m1")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		n    *note.Note
		want error
	}{
		{"same id", newLibNote(t, "n1", "s2", day, "m2"), shared.ErrVersionConflict},
		{"unknown subject", newLibNote(t, "n2", "s9", day, "m1"), shared.ErrConflict},
		{"unknown material", newLibNote(t, "n3", "s1", day, "m1", "m9"), shared.ErrConflict},
	}
	for _, tt := range tests {
		if err := repo.Save(ctx, tt.n); !errors.Is(err, tt.want) || tt.n.Version() != 0 {
			t.Errorf("%s: Save = %v (version %d), want %v", tt.name, err, tt.n.Version(), tt.want)
		}
	}
	// Nothing of a refused insert is kept.
	if n := scanInt(t, db.sql, "SELECT count(*) FROM notes"); n != 1 {
		t.Fatalf("%d notes", n)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM note_parts"); n != 1 {
		t.Fatalf("%d parts", n)
	}

	// Two copies of one version: the first save wins, the second is stale and changes nothing.
	first, err := repo.Get(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Get(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	t1 := libT0.Add(time.Hour)
	if _, err := first.AddParts([]shared.MaterialID{"m2"}, t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Retitle("Другое", t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, second); !errors.Is(err, shared.ErrVersionConflict) || second.Version() != 1 {
		t.Fatalf("stale Save = %v, version %d", err, second.Version())
	}
	if got, err := repo.Get(ctx, "n1"); err != nil || !reflect.DeepEqual(got.State(), first.State()) {
		t.Fatalf("after the stale save: %+v, %v", got, err)
	}

	// A save whose part fails rolls the note back as well.
	if _, err := first.ReplacePart("m2", "m9", t1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, first); !errors.Is(err, shared.ErrConflict) || first.Version() != 2 {
		t.Fatalf("Save with an unknown part = %v, version %d", err, first.Version())
	}
	if got, err := repo.Get(ctx, "n1"); err != nil || got.Version() != 2 || len(got.Parts()) != 2 || got.Parts()[1] != "m2" {
		t.Fatalf("after the refused save: %+v, %v", got, err)
	}
}

func TestNoteLists(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	seedNoteLibrary(t, db)
	repo := NewNoteRepository(db)
	sep12, sep19 := noteDate(t, "2026-09-12"), noteDate(t, "2026-09-19")

	notes := []*note.Note{
		newLibNote(t, "undated", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorUndated}), "m1"),
		newLibNote(t, "late", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l1", Date: sep19}), "m1"),
		newLibNote(t, "early", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: sep12}), "m2"),
		newLibNote(t, "lesson2", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l1", Date: sep19}), "m3"),
		newLibNote(t, "other", "s2", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: sep12}), "m3"),
	}
	for _, n := range notes {
		if err := repo.Save(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(ns []*note.Note) []shared.NoteID {
		out := []shared.NoteID{}
		for _, n := range ns {
			out = append(out, n.ID())
		}
		return out
	}

	tests := []struct {
		name string
		list func() ([]*note.Note, error)
		want []shared.NoteID
	}{
		{"subject by date, undated last", func() ([]*note.Note, error) { return repo.ListBySubject(ctx, "s1") }, []shared.NoteID{"early", "late", "lesson2", "undated"}},
		{"other subject", func() ([]*note.Note, error) { return repo.ListBySubject(ctx, "s2") }, []shared.NoteID{"other"}},
		{"subject without notes", func() ([]*note.Note, error) { return repo.ListBySubject(ctx, "s9") }, []shared.NoteID{}},
		{"recording", func() ([]*note.Note, error) { return repo.ListByRecording(ctx, "r1") }, []shared.NoteID{"early", "other"}},
		{"lesson", func() ([]*note.Note, error) { return repo.ListByLesson(ctx, "l1") }, []shared.NoteID{"late", "lesson2"}},
		{"unknown lesson", func() ([]*note.Note, error) { return repo.ListByLesson(ctx, "l9") }, []shared.NoteID{}},
	}
	for _, tt := range tests {
		got, err := tt.list()
		if err != nil || !reflect.DeepEqual(ids(got), tt.want) {
			t.Errorf("%s: %v, %v; want %v", tt.name, ids(got), err, tt.want)
		}
	}
	got, err := repo.ListBySubject(ctx, "s1")
	if err != nil || !reflect.DeepEqual(got[0].State(), notes[2].State()) {
		t.Fatalf("listed note = %+v, want %+v", got[0].State(), notes[2].State())
	}

	// ListAll lists every subject by date, undated last; a row that cannot be restored is reported, not fatal.
	all, unreadable, err := repo.ListAll(ctx)
	if err != nil || len(unreadable) != 0 || !reflect.DeepEqual(ids(all), []shared.NoteID{"early", "other", "late", "lesson2", "undated"}) {
		t.Fatalf("ListAll = %v, %v, %v", ids(all), unreadable, err)
	}
	if !reflect.DeepEqual(all[0].State(), notes[2].State()) {
		t.Fatalf("ListAll note = %+v, want %+v", all[0].State(), notes[2].State())
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE notes SET title = ' padded' WHERE id = 'late'"); err != nil {
		t.Fatal(err)
	}
	all, unreadable, err = repo.ListAll(ctx)
	if err != nil || !reflect.DeepEqual(unreadable, []shared.NoteID{"late"}) || len(all) != 4 {
		t.Fatalf("ListAll with a broken row = %v, %v, %v", ids(all), unreadable, err)
	}
}

// The anchor carries no foreign key: purging the recording (ADR-0018) or the lesson of the schedule leaves the notes
// as they are. The subject and the materials of a note are kept by RESTRICT.
func TestNotesOutliveRecordingsAndLessons(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	seedNoteLibrary(t, db)
	repo := NewNoteRepository(db)
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, created_at, updated_at)
		VALUES ('r1', 'FAILED', 1, 1000, 'recordings/r1', 1, 1)`)
	mustExec(t, db, `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, created_at, updated_at)
		VALUES ('l1', 'Физика', 1, 2, 'UTC', 1, 1)`)
	day := noteDate(t, "2026-09-12")
	rec := newLibNote(t, "n1", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: day}), "m1")
	les := newLibNote(t, "n2", "s1", noteAnchorOf(t, note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l1", Date: day}), "m2")
	for _, n := range []*note.Note{rec, les} {
		if err := repo.Save(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	mustExec(t, db, "DELETE FROM recordings WHERE id = 'r1'")
	mustExec(t, db, "UPDATE schedule_entries SET deleted_at = 5 WHERE id = 'l1'")
	mustExec(t, db, "DELETE FROM schedule_entries WHERE id = 'l1' AND deleted_at IS NOT NULL")
	for _, want := range []*note.Note{rec, les} {
		got, err := repo.Get(ctx, want.ID())
		if err != nil || !reflect.DeepEqual(got.State(), want.State()) {
			t.Fatalf("%s after the purge: %+v, %v", want.ID(), got, err)
		}
	}

	subjects := NewSubjectRepository(db)
	s1, err := subjects.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := subjects.Delete(ctx, s1); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Delete of a subject with notes = %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, "DELETE FROM materials WHERE id = 'm1'"); err == nil {
		t.Fatal("a material that is a part of a note was deleted")
	}
}
