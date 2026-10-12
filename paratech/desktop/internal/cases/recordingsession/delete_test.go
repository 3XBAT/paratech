package recordingsession

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func (e *env) writeFile(ref string) shared.ArtifactRef {
	e.t.Helper()
	r, err := shared.ParseArtifactRef(ref)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), r, strings.NewReader("x")); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// completedWithTranscript records rec-1 to the end and gives it a transcript file; other.txt belongs to nobody.
func completedWithTranscript(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a", "b")
	if _, err := e.svc.Stop(testCtx(t), info.ID); err != nil {
		t.Fatal(err)
	}
	e.tr.files[info.ID] = []shared.ArtifactRef{e.writeFile("transcripts/t1.txt")}
	e.writeFile("transcripts/other.txt")
	return e
}

func TestDeleteRemovesFilesTranscriptsAndRow(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := completedWithTranscript(t)

	if err := e.svc.Delete(testCtx(t), "rec-1"); err != nil {
		t.Fatal(err)
	}
	if e.repo.has("rec-1") || e.tr.deletes != 1 {
		t.Fatalf("rows left: recording %v, transcript deletes %d", e.repo.has("rec-1"), e.tr.deletes)
	}
	if want := []string{"transcripts/other.txt"}; !slices.Equal(e.files.Paths(), want) {
		t.Fatalf("files = %v, want %v", e.files.Paths(), want)
	}

	if err := e.svc.Delete(testCtx(t), "rec-1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteRefusesARecordingInProgress(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")

	if err := e.svc.Delete(testCtx(t), info.ID); !errors.Is(err, recording.ErrNotDeletable) {
		t.Fatalf("Delete while recording = %v", err)
	}
	if len(e.files.Paths()) != 1 || !e.repo.has(info.ID) {
		t.Fatal("a refused delete touched files or rows")
	}
}

func TestDeleteOfAnInterruptedRecordingRemovesAStrayTrack(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	r, err := recording.New(recording.Spec{ID: "crashed", MaxDuration: time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Begin(t0); err != nil {
		t.Fatal(err)
	}
	if err := r.RequireRecovery(t0.Add(time.Minute), "stale heartbeat"); err != nil {
		t.Fatal(err)
	}
	e.repo.put(r)
	e.writeFile("recordings/crashed/chunk-0001")
	e.writeFile("recordings/crashed.wav") // written by a finalization whose save was lost

	if err := e.svc.Delete(testCtx(t), "crashed"); err != nil {
		t.Fatal(err)
	}
	if len(e.files.Paths()) != 0 || e.repo.has("crashed") {
		t.Fatalf("left: files %v, row %v", e.files.Paths(), e.repo.has("crashed"))
	}
}

// The aggregate is asked again inside the transaction: a recovery that restarted the finalization meanwhile wins.
func TestDeleteRechecksInsideTheTransaction(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	r, err := recording.New(recording.Spec{ID: "crashed", MaxDuration: time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Begin(t0); err != nil {
		t.Fatal(err)
	}
	if err := r.RequireRecovery(t0.Add(time.Minute), "stale heartbeat"); err != nil {
		t.Fatal(err)
	}
	e.repo.put(r)
	e.tx.before = func() {
		if err := r.Finalize(t0.Add(2 * time.Minute)); err != nil {
			t.Error(err)
		}
		e.repo.put(r)
	}

	if err := e.svc.Delete(testCtx(t), "crashed"); !errors.Is(err, recording.ErrNotDeletable) {
		t.Fatalf("Delete = %v, want ErrNotDeletable", err)
	}
	if !e.repo.has("crashed") || e.tr.deletes != 0 {
		t.Fatal("rows were deleted after the re-check refused")
	}
}

func TestDeleteFileErrorKeepsTheRows(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := completedWithTranscript(t)
	failing := &failingDelete{MemArtifacts: e.files, err: shared.ErrStoragePermission}
	e.svc.artifacts = failing

	if err := e.svc.Delete(testCtx(t), "rec-1"); !errors.Is(err, shared.ErrStoragePermission) {
		t.Fatalf("Delete = %v", err)
	}
	if !e.repo.has("rec-1") || e.tr.deletes != 0 {
		t.Fatal("rows were deleted although the files were not")
	}
}

type failingDelete struct {
	*testenv.MemArtifacts
	err error
}

func (f *failingDelete) Delete(context.Context, shared.ArtifactRef) error { return f.err }
