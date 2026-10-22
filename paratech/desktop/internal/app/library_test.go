package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/cases/library"
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
