package tests

// Scenario test of the recording session with the real recorder adapter: the recorder child is this test binary with
// a fake tone source (testenv.RunFakeRecorderChild), the chunks and the track go through the real Storage Manager and
// the state through the real SQLite adapter. No audio device is touched.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

var (
	_ recordingsession.Recorder = (*audiocapture.Recorder)(nil)
	_ audiocapture.ChunkFiles   = (*filestore.Store)(nil)
	_ audiocapture.TrackFiles   = (*filestore.Store)(nil)
)

func TestRecordingWithTheRecorderChild(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dataDir, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	store, err := filestore.New(filepath.Join(dataDir, "artifacts")) // where the child's Storage Manager writes
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := audiocapture.DefaultConfig()
	cfg.ChunkSeconds, cfg.StartTimeout = 30, 30*time.Second
	rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: &audiocapture.CommandLauncher{
		Path: exe, Args: []string{"--recorder", "--data-dir", dataDir}, Env: []string{testenv.EnvFakeRecorder + "=" + testenv.RecorderTone},
	}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewRecordingRepository(db)
	svc, err := recordingsession.New(recordingsession.Deps{
		Recordings: repo, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: rec, Artifacts: store, Tx: db,
		Clock: testenv.NewFakeClock(recT0), Sleeper: testenv.NewFakeClock(recT0), IDs: &recIDs{}, Completions: ignoredCompletions{},
	}, recordingsession.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc.Close(ctx)
		_ = store.Close()
		_ = db.Close()
	})

	info, err := svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	chunks := filepath.Join(dataDir, "artifacts", "recordings", string(info.ID))
	testenv.Eventually(t, "a chunk is closed", func() bool {
		_, err := os.Stat(filepath.Join(chunks, "chunk-000001.wav"))
		return err == nil
	}, func() { time.Sleep(20 * time.Millisecond) })

	got, err := svc.Stop(ctx, info.ID)
	if err != nil || got.Status != recording.StatusCompleted {
		t.Fatalf("Stop = %+v, %v; want COMPLETED", got, err)
	}

	// The duration is the frames of the chunks (8 kHz mono 16-bit: 2 bytes a frame after the 44-byte header).
	entries, err := os.ReadDir(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var frames int64
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".wav") {
			t.Fatalf("unexpected file %s after a clean stop", e.Name())
		}
		fi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		frames += (fi.Size() - 44) / 2
	}
	want := time.Duration(frames) * time.Second / 8000
	if got.Duration != want || got.Chunks != len(entries) {
		t.Fatalf("recording %+v; want %v over %d chunks", got, want, len(entries))
	}
	track, err := os.Stat(filepath.Join(dataDir, "artifacts", "recordings", string(info.ID)+".wav"))
	if err != nil || track.Size() != 44+2*frames*2 { // 16 kHz mono 16-bit
		t.Fatalf("track: %v, %v; want %d bytes", track, err, 44+4*frames)
	}

	stored, err := repo.Get(ctx, info.ID)
	if err != nil || stored.Status() != recording.StatusCompleted {
		t.Fatalf("stored = %v, %v", stored, err)
	}
}

// AC2 (TASK-058) end to end: the recorder child switches the whole-computer capture (EXCLUDE) to classic loopback
// after an activation failure or a healthy-but-mute stream; the recording keeps running and is COMPLETED with the
// fixed note as its problem in SQLite.
func TestRecordingNotesTheFallbackOfTheRecorderChild(t *testing.T) {
	for _, tt := range []struct {
		scenario string
		cause    values.SwitchCause
	}{
		{testenv.RecorderFallback, values.SwitchActivationFailed},
		{testenv.RecorderMute, values.SwitchHealthyButMute},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			ctx := context.Background()
			dataDir := t.TempDir()
			db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dataDir, "app.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			store, err := filestore.New(filepath.Join(dataDir, "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cfg := audiocapture.DefaultConfig()
			cfg.ChunkSeconds, cfg.StartTimeout = 30, 30*time.Second
			rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: &audiocapture.CommandLauncher{
				Path: exe, Args: []string{"--recorder", "--data-dir", dataDir}, Env: []string{testenv.EnvFakeRecorder + "=" + tt.scenario},
			}}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			repo := sqlite.NewRecordingRepository(db)
			svc, err := recordingsession.New(recordingsession.Deps{
				Recordings: repo, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: rec, Artifacts: store, Tx: db,
				Clock: testenv.NewFakeClock(recT0), Sleeper: testenv.NewFakeClock(recT0), IDs: &recIDs{}, Completions: ignoredCompletions{},
			}, recordingsession.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = svc.Close(ctx)
				_ = store.Close()
				_ = db.Close()
			})

			info, err := svc.Start(ctx, recordingsession.StartRequest{})
			if err != nil {
				t.Fatal(err)
			}
			var sw values.CaptureSwitch
			testenv.Eventually(t, "the capture falls back", func() bool {
				d, ok := rec.Diagnostics(ctx, info.ID)
				var fell bool
				sw, fell = d.Fallback()
				return ok && fell && d.Running && d.Mode == values.CaptureSystem && d.Chunks >= 1
			}, func() { time.Sleep(20 * time.Millisecond) })
			if sw.Cause != tt.cause || sw.From != values.CaptureExclude {
				t.Fatalf("switch %+v", sw)
			}
			if active, ok := svc.Active(); !ok || active.Status != recording.StatusRecording {
				t.Fatalf("active %+v, %v: the recording must go on", active, ok)
			}

			got, err := svc.Stop(ctx, info.ID)
			if err != nil || got.Status != recording.StatusCompleted || got.Problem != sw.Note() {
				t.Fatalf("Stop = %+v, %v; want COMPLETED with the note %q", got, err, sw.Note())
			}
			stored, err := repo.Get(ctx, info.ID)
			if err != nil || stored.Problem() != sw.Note() {
				t.Fatalf("stored = %v, %v", stored, err)
			}
		})
	}
}
