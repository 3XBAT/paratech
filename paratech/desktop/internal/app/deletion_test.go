package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// storedRecording saves a FAILED recording with a chunk file and a stray track, as a crash may leave them.
func storedRecording(t *testing.T, c *core, id shared.RecordingID) []shared.ArtifactRef {
	t.Helper()
	ctx := context.Background()
	r, err := recording.New(recording.Spec{ID: id, MaxDuration: time.Hour}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(time.Now(), "no device"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewRecordingRepository(c.db).Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	var files []shared.ArtifactRef
	for _, s := range []string{"recordings/" + string(id) + "/chunk-0001.wav", "recordings/" + string(id) + ".wav"} {
		ref, err := shared.ParseArtifactRef(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.store.Write(ctx, ref, strings.NewReader("audio")); err != nil {
			t.Fatal(err)
		}
		files = append(files, ref)
	}
	return files
}

func checkGone(t *testing.T, c *core, id shared.RecordingID, files []shared.ArtifactRef) {
	t.Helper()
	ctx := context.Background()
	if _, err := sqlite.NewRecordingRepository(c.db).Get(ctx, id); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("recording %s after the deletion: %v, want not found", id, err)
	}
	for _, f := range files {
		if _, err := c.store.Size(ctx, f); !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("%s after the deletion: %v, want not found", f, err)
		}
	}
}

// TASK-095 on the wired core: «Удалить» of two recordings through the binding removes the rows and the files, and
// the rows leave the recordings page.
func TestDeleteRecordingsThroughTheWiredCore(t *testing.T) {
	e, _ := testEnv(t)
	u := openUICore(t, t.TempDir(), e)
	u.start(t)
	first, second := storedRecording(t, u.c, "rec-a"), storedRecording(t, u.c, "rec-b")
	svc := wails.NewRecordingDeleteService(u.c.ui.Deletion, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	p, err := svc.Preview(ctx, []string{"rec-a", "rec-b"})
	if err != nil || len(p.Recordings) != 2 || !p.Recordings[0].Deletable {
		t.Fatalf("Preview = %+v, %v", p, err)
	}
	res, err := svc.Delete(ctx, []wails.DeleteRequest{{RecordingID: "rec-a"}, {RecordingID: "rec-b"}})
	if err != nil || res.Deleted != 2 || res.Message != "Удалено 2" || len(res.Failures) != 0 {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	checkGone(t, u.c, "rec-a", first)
	checkGone(t, u.c, "rec-b", second)
	if recs, err := u.c.ui.Activity.ListRecordings(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("ListRecordings = %+v, %v; want none", recs, err)
	}
}

// A crash right after the deletion mark: the recording is hidden, and the next start finishes the deletion before
// the recording recovery runs.
func TestCrashMidDeleteIsFinishedAtTheNextStart(t *testing.T) {
	dir := t.TempDir()
	e, _ := testEnv(t)
	u := openUICore(t, dir, e)
	files := storedRecording(t, u.c, "rec-crash")
	ctx := context.Background()
	repo := sqlite.NewRecordingRepository(u.c.db)
	r, err := repo.Get(ctx, "rec-crash")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MarkDeleting(time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	if recs, err := u.c.ui.Activity.ListRecordings(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("ListRecordings with a marked recording = %+v, %v; want none", recs, err)
	}
	if err := u.life.shutdown(); err != nil { // the process ends before the files go
		t.Fatal(err)
	}

	again := openUICore(t, dir, e)
	again.start(t)
	checkGone(t, again.c, "rec-crash", files)
}
