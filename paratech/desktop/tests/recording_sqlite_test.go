package tests

// Scenario tests of the recording session on the real SQLite adapter and the real Storage Manager (filestore): only the
// clock and the audio source are fake (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ recordingsession.RecordingRepository  = (*sqlite.RecordingRepository)(nil)
	_ recordingsession.RecordingTranscripts = (*sqlite.RecordingTranscripts)(nil)
	_ recordingsession.Tx                   = (*sqlite.DB)(nil)
	_ recordingsession.Recorder             = (*testenv.FakeRecorder)(nil)
	_ recordingsession.ArtifactStore        = (*testenv.MemArtifacts)(nil)
	_ recordingsession.Clock                = (*testenv.FakeClock)(nil)
	_ recordingsession.Sleeper              = (*testenv.FakeClock)(nil)
	_ testenv.ArtifactFiles                 = (*filestore.Store)(nil)
)

var recT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

type recIDs struct {
	mu sync.Mutex
	n  int
}

func (g *recIDs) NewRecordingID() shared.RecordingID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return shared.RecordingID(fmt.Sprintf("rec-%d", g.n))
}

type recordingEnv struct {
	t     *testing.T
	root  string
	db    *sqlite.DB
	repo  *sqlite.RecordingRepository
	store *filestore.Store
	clock *testenv.FakeClock
	rec   *testenv.FakeRecorder
	svc   *recordingsession.Service
}

// newRecordingEnv opens the database and the data root below dir. The cleanup closes the service before the files.
func newRecordingEnv(t *testing.T, dir string) *recordingEnv {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(dir, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "data")
	store, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &recordingEnv{
		t: t, root: root, db: db, repo: sqlite.NewRecordingRepository(db), store: store,
		clock: testenv.NewFakeClock(recT0),
	}
	e.rec = testenv.NewFakeRecorder(store, time.Minute)
	e.svc, err = recordingsession.New(recordingsession.Deps{
		Recordings: e.repo, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: e.rec, Artifacts: store, Tx: db,
		Clock: e.clock, Sleeper: e.clock, IDs: &recIDs{},
	}, recordingsession.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.close(context.Background()) })
	return e
}

// close shuts the service down with ctx and releases the files; it may run more than once.
func (e *recordingEnv) close(ctx context.Context) error {
	err := e.svc.Close(ctx)
	_ = e.store.Close()
	_ = e.db.Close()
	return err
}

func (e *recordingEnv) feed(id shared.RecordingID, chunks ...string) {
	e.t.Helper()
	for _, c := range chunks {
		if _, err := e.rec.Feed(context.Background(), id, []byte(c)); err != nil {
			e.t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// AC-04 end to end: chunks land in the data root through the Storage Manager, the cap finalizes the recording and
// the final state is in the database.
func TestRecordingReachesTheCapOnDisk(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecordingEnv(t, t.TempDir())
	ctx := context.Background()

	info, err := e.svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e.feed(info.ID, "one-", "two-", "three")
	for i, want := range []string{"one-", "two-", "three"} {
		path := filepath.Join(e.root, "recordings", string(info.ID), fmt.Sprintf("chunk-%04d", i+1))
		if got := readFile(t, path); got != want {
			t.Fatalf("chunk %d = %q, want %q", i+1, got, want)
		}
	}

	if err := e.clock.AwaitSleepers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(3 * time.Hour)
	testenv.Eventually(t, "COMPLETED", func() bool {
		r, err := e.repo.Get(ctx, info.ID)
		return err == nil && r.Status() == recording.StatusCompleted
	}, nil)

	got, err := e.svc.Stop(ctx, info.ID)
	if err != nil || got.Status != recording.StatusCompleted || got.Chunks != 3 || got.Duration != 3*time.Minute ||
		!got.StoppedAt.Equal(recT0.Add(3*time.Hour)) {
		t.Fatalf("after the cap: %+v, %v", got, err)
	}
	if track := readFile(t, filepath.Join(e.root, "recordings", string(info.ID)+".wav")); track != "one-two-three" {
		t.Fatalf("final track = %q", track)
	}
}

// A shutdown that cannot wait leaves the recording unfinished with its chunks; after a restart it is found for the
// recovery (TASK-014).
func TestInterruptedRecordingSurvivesRestart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dir := t.TempDir()
	e := newRecordingEnv(t, dir)
	ctx := context.Background()

	info, err := e.svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e.feed(info.ID, "a", "b")
	entered, release := e.rec.HoldFinalize()
	defer release()

	shutdown, cancel := context.WithCancel(ctx)
	closed := make(chan error, 1)
	go func() { closed <- e.close(shutdown) }()
	<-entered
	cancel()
	if err := <-closed; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close = %v", err)
	}

	again := newRecordingEnv(t, dir)
	unfinished, err := again.repo.ListByStatus(ctx, recording.UnfinishedStatuses()...)
	if err != nil || len(unfinished) != 1 || unfinished[0].ID() != info.ID || unfinished[0].Status() != recording.StatusFinalizing {
		t.Fatalf("unfinished after restart: %v, %v", unfinished, err)
	}
	chunks, err := again.store.List(ctx, unfinished[0].ChunksDir())
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks after restart: %v, %v", chunks, err)
	}
	if _, err := again.svc.Stop(ctx, info.ID); !errors.Is(err, recordingsession.ErrNotRunning) {
		t.Fatalf("Stop of the interrupted recording = %v", err)
	}
}

func TestDeleteRecordingEndToEnd(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecordingEnv(t, t.TempDir())
	ctx := context.Background()

	info, err := e.svc.Start(ctx, recordingsession.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e.feed(info.ID, "a")
	if _, err := e.svc.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}

	transcript, err := shared.ParseArtifactRef("transcripts/t1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Write(ctx, transcript, strings.NewReader("text")); err != nil {
		t.Fatal(err)
	}
	err = e.db.Do(ctx, func(ctx context.Context) error {
		_, err := e.db.DBTX(ctx).ExecContext(ctx, `INSERT INTO transcripts
			(id, recording_id, model_name, model_version, text_ref, created_at) VALUES ('t1', ?, 'small', '1', ?, 0)`,
			string(info.ID), transcript.String())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.Delete(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.Get(ctx, info.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
	for _, p := range []string{"recordings/" + string(info.ID), "recordings/" + string(info.ID) + ".wav", "transcripts/t1.txt"} {
		if _, err := os.Stat(filepath.Join(e.root, filepath.FromSlash(p))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists: %v", p, err)
		}
	}
	var left int
	if err := e.db.DBTX(ctx).QueryRowContext(ctx, "SELECT count(*) FROM transcripts").Scan(&left); err != nil || left != 0 {
		t.Fatalf("transcripts left: %d, %v", left, err)
	}
}
