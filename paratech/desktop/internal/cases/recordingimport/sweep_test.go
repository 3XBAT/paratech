package recordingimport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

func TestImportedCopyNames(t *testing.T) {
	tests := []struct {
		ref  string
		id   shared.RecordingID
		copy bool
	}{
		{"recordings/imp-abc.wav", "imp-abc", true},
		{"recordings/imp-abc.mp3", "imp-abc", true},
		{"recordings/imp-abc.flac", "imp-abc", true},
		{"recordings/imp-abc.ogg", "imp-abc", true},
		{"recordings/abc.wav", "", false},     // a capture track
		{"recordings/abc", "", false},         // a capture chunk directory
		{"recordings/imp-abc", "", false},     // no format
		{"recordings/imp-.wav", "", false},    // no id after the prefix
		{"recordings/imp-abc.m4a", "", false}, // not a format of an import
		{"recordings/imp-abc.WAV", "", false}, // the import writes lower case
		{"recordings/imp-abc/chunk-0001.wav", "", false},
		{"transcripts/imp-abc.wav", "", false},
		{"recordings/notes.txt", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			id, ok := importedCopyOf(mustRef(t, tc.ref))
			if ok != tc.copy || ok && id != tc.id {
				t.Fatalf("importedCopyOf = %q, %v; want %q, %v", id, ok, tc.id, tc.copy)
			}
		})
	}
}

// A crash between the copy and the stored recording leaves recordings/imp-<id>.<ext>: the start-up sweep deletes it
// once it is old enough and no recording has its id; a capture's files and anything else are kept.
func TestSweepOrphans(t *testing.T) {
	old := now.Add(-OrphanAge - time.Second)
	tests := []struct {
		name string
		ref  string
		at   time.Time
		row  bool // a recording of the id is stored
		gone bool
	}{
		{name: "orphan copy", ref: "recordings/imp-crash.wav", at: old, gone: true},
		{name: "orphan mp3 copy", ref: "recordings/imp-crash.mp3", at: old, gone: true},
		{name: "young copy", ref: "recordings/imp-young.wav", at: now.Add(-OrphanAge + time.Second)},
		{name: "copy written exactly the age ago", ref: "recordings/imp-edge.wav", at: now.Add(-OrphanAge)}, // not older
		{name: "copy with its recording", ref: "recordings/imp-kept.wav", at: old, row: true},
		{name: "capture track without a recording", ref: "recordings/abcdefgh.wav", at: old},
		{name: "unrelated name", ref: "recordings/readme.txt", at: old},
		{name: "import-like name of another format", ref: "recordings/imp-x.m4a", at: old},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			ref := mustRef(t, tc.ref)
			w.store.files[ref] = []byte("audio")
			w.store.modTimes[ref] = tc.at
			if tc.row {
				saveImported(t, w, ref)
			}

			n, err := w.svc.SweepOrphans(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, kept := w.store.files[ref]
			if kept == tc.gone || n != len(w.store.deleted) || (n == 1) != tc.gone {
				t.Fatalf("kept %v, swept %d, deleted %v; want gone %v", kept, n, w.store.deleted, tc.gone)
			}
		})
	}
}

// A capture's chunk directory is never a copy; a directory that looks like one is not deleted either.
func TestSweepKeepsDirectories(t *testing.T) {
	w := newWorld(t)
	w.store.dirs = []shared.ArtifactRef{mustRef(t, "recordings/abcdefgh"), mustRef(t, "recordings/imp-dir.wav")}
	if n, err := w.svc.SweepOrphans(context.Background()); err != nil || n != 0 || len(w.store.deleted) != 0 {
		t.Fatalf("SweepOrphans = %d, %v; deleted %v", n, err, w.store.deleted)
	}
}

// The failure of one file is reported after the others were swept; a failed listing sweeps nothing.
func TestSweepFailures(t *testing.T) {
	old := now.Add(-time.Hour)
	tests := []struct {
		name  string
		set   func(w *world)
		swept int
	}{
		{"list fails", func(w *world) { w.store.listErr = errors.New("volume gone") }, 0},
		{"modification time fails", func(w *world) { w.store.modErr = errors.New("device not ready") }, 0},
		{"recording lookup fails", func(w *world) { w.recordings.getErr = errors.New("database is locked") }, 0},
		{"delete fails", func(w *world) { w.store.deleteErr = errors.New("file in use") }, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			for _, s := range []string{"recordings/imp-a.wav", "recordings/imp-b.ogg"} {
				ref := mustRef(t, s)
				w.store.files[ref] = []byte("audio")
				w.store.modTimes[ref] = old
			}
			tc.set(w)

			n, err := w.svc.SweepOrphans(context.Background())
			if !errors.Is(err, ErrStorage) || n != tc.swept || len(w.store.files) != 2 {
				t.Fatalf("SweepOrphans = %d, %v; %d files left", n, err, len(w.store.files))
			}
		})
	}
}

// One file failing does not stop the others; the error is returned after them.
func TestSweepGoesOnAfterAFailure(t *testing.T) {
	w := newWorld(t)
	bad, good := mustRef(t, "recordings/imp-a.wav"), mustRef(t, "recordings/imp-b.wav")
	for _, ref := range []shared.ArtifactRef{bad, good} {
		w.store.files[ref] = []byte("audio")
		w.store.modTimes[ref] = now.Add(-time.Hour)
	}
	w.store.failDelete = bad

	n, err := w.svc.SweepOrphans(context.Background())
	_, badKept := w.store.files[bad]
	_, goodKept := w.store.files[good]
	if !errors.Is(err, ErrStorage) || n != 1 || !badKept || goodKept {
		t.Fatalf("SweepOrphans = %d, %v; kept a %v, b %v", n, err, badKept, goodKept)
	}
}

func TestSweepStopsWhenCancelled(t *testing.T) {
	w := newWorld(t)
	ref := mustRef(t, "recordings/imp-a.wav")
	w.store.files[ref] = []byte("audio")
	w.store.modTimes[ref] = now.Add(-time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.svc.SweepOrphans(ctx); !errors.Is(err, context.Canceled) || len(w.store.deleted) != 0 {
		t.Fatalf("SweepOrphans = %v, deleted %v", err, w.store.deleted)
	}
}

// saveImported stores the imported recording whose copy is ref.
func saveImported(t *testing.T, w *world, ref shared.ArtifactRef) {
	t.Helper()
	id, ok := importedCopyOf(ref)
	if !ok {
		t.Fatalf("%s is not a copy", ref)
	}
	format := ref.Path()[strings.LastIndexByte(ref.Path(), '.')+1:]
	r, err := recording.NewImported(recording.ImportSpec{
		ID: id, StartedAt: now.Add(-2 * time.Hour), SHA256: digest([]byte(ref.String())),
		Audio: recording.ImportedAudio{Format: recording.AudioFormat(format)},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.recordings.Save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}
