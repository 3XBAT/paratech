package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/cases/library"
)

// The page of a subject is wired to the library and the visits file of the data directory: an opening survives a
// restart, and the overview itself records none (TASK-225).
func TestOpenCoreWiresTheSubjectPage(t *testing.T) {
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
	v, err := core.SubjectOverview.Overview(ctx, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "Физика" || !v.OpenedAt.IsZero() || len(v.Deadlines)+len(v.Schedule)+len(v.Lessons)+len(v.Fresh) != 0 {
		t.Fatalf("overview = %+v, want an empty page never opened", v)
	}
	visits := filepath.Join(dir, visitsFile)
	if _, err := os.Stat(visits); !os.IsNotExist(err) {
		t.Fatalf("the overview wrote the visits file: %v", err)
	}
	if err := core.SubjectOverview.MarkOpened(ctx, sub.ID()); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b := newTestApp(t, dir)
	core, err = b.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v, err = core.SubjectOverview.Overview(ctx, sub.ID()); err != nil || v.OpenedAt.IsZero() {
		t.Fatalf("after restart: opened %v, %v", v.OpenedAt, err)
	}
}
