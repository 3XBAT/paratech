package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// The library is wired to SQLite and the artifact store: an uploaded file lands under artifacts/materials/<sha256>,
// never next to the database, and the subject survives a restart (TASK-121).
func TestOpenCoreWiresTheLibrary(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")

	a := newTestApp(t, dir)
	core, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := core.Library.CreateSubject(ctx, library.NewSubject{Name: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.Library.Import(ctx, library.Upload{Name: "notes.txt", Data: []byte("Лекция 1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Library.Place(ctx, res.Material.ID(), sub.ID(), ""); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, artifactsDir, "materials", res.Material.Digest().String())
	if data, err := os.ReadFile(blob); err != nil || !bytes.Equal(data, []byte("Лекция 1")) {
		t.Fatalf("blob %s: %q, %v", blob, data, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b := newTestApp(t, dir)
	core, err = b.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	list, err := core.Library.Materials(ctx, sub.ID())
	if err != nil || len(list) != 1 || list[0].ID() != res.Material.ID() {
		t.Fatalf("after restart: %v, %v", list, err)
	}
	if subjects, err := core.Knowledge.Subjects(ctx); err != nil || len(subjects) != 0 {
		t.Fatalf("knowledge base = %+v, %v; want no lessons", subjects, err)
	}
}

// The knowledge base reads the student's notes from SQLite and their texts through the library (TASK-152): a note of
// a day is a lecture of its own and its text is searched.
func TestOpenCoreWiresNotesIntoTheKnowledgeBase(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, filepath.Join(t.TempDir(), "data"))
	core, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := core.Library.CreateSubject(ctx, library.NewSubject{Name: "Матанализ"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.Library.Import(ctx, library.Upload{Name: "ряды.txt", Data: []byte("Ряд Тейлора сходится")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Library.Place(ctx, res.Material.ID(), sub.ID(), ""); err != nil {
		t.Fatal(err)
	}
	date, err := note.NewDate(2026, time.September, 12)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorDay, Date: date})
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.New(note.Spec{ID: "n1", Subject: sub.ID(), Anchor: anchor, Title: "Ряды", Parts: []shared.MaterialID{res.Material.ID()}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewNoteRepository(a.core.db).Save(ctx, n); err != nil {
		t.Fatal(err)
	}

	found, err := core.Knowledge.Search(ctx, "тейлор")
	if err != nil || len(found.Hits) != 1 {
		t.Fatalf("Search = %+v, %v", found, err)
	}
	h := found.Hits[0]
	if h.Section != kb.SectionOwnNote || h.Note != "n1" || h.Material != res.Material.ID() || h.Lesson.Key != kb.DayKey(sub.ID(), time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("hit = %+v", h)
	}
}
