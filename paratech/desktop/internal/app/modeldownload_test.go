package app

// TASK-030: the background download of the model manager (state, cancel, close, one at a time) over a real
// modelcatalog.Downloader on the wired SQLite and filestore. The catalog lists one test entry whose SHA-256 is computed
// here from the served bytes; the source is a fake that stops in the middle of the file (no network).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// testCatalogID is the only model of testCatalog.
const testCatalogID values.CatalogID = "tiny-q5_1"

// The test model is 64 bytes; the source stops after gateAt of them until it is released. Untyped: it is compared
// with byte counters of several integer types.
const gateAt = 24

var testModelBytes = bytes.Repeat([]byte("whisper!"), 8)

// testCatalog is a manifest with the test model only.
type testCatalog struct{}

func (testCatalog) Manifest(context.Context) ([]byte, error) {
	sum := sha256.Sum256(testModelBytes)
	return fmt.Appendf(nil, `{"schema_version": 1, "models": [{"id": %q, "name": "tiny", "version": "1",
		"size_bytes": %d, "sha256": %q, "source_url": "https://example.com/ggml-tiny-q5_1.bin", "quantization": "q5_1",
		"capabilities": ["multilingual"], "recommendation": {"min_cpu_cores": 1, "min_ram_mb": 512, "preferred": true}}]}`,
		testCatalogID, len(testModelBytes), hex.EncodeToString(sum[:])), nil
}

// gatedSource serves testModelBytes from the requested offset; a body stops at gateAt until release is closed or
// the request ends.
type gatedSource struct {
	release     chan struct{}
	releaseOnce sync.Once

	mu      sync.Mutex
	offsets []int64 // of every Fetch
	opened  atomic.Int32
	closed  atomic.Int32
}

func (s *gatedSource) Fetch(ctx context.Context, _ values.SourceURL, offset int64, _ values.HostAllowlist) (values.RangeReply, io.ReadCloser, error) {
	s.mu.Lock()
	s.offsets = append(s.offsets, offset)
	s.mu.Unlock()
	s.opened.Add(1)

	reply := values.RangeReply{Status: values.RangeFull, Total: int64(len(testModelBytes))}
	if offset > 0 {
		reply = values.RangeReply{Status: values.RangePartial, Start: offset, Total: int64(len(testModelBytes))}
	}
	return reply, &gatedBody{ctx: ctx, s: s, pos: offset}, nil
}

func (s *gatedSource) open() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *gatedSource) fetchOffsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.offsets...)
}

type gatedBody struct {
	ctx context.Context
	s   *gatedSource
	pos int64
}

func (b *gatedBody) Read(p []byte) (int, error) {
	if b.pos >= int64(len(testModelBytes)) {
		return 0, io.EOF
	}
	if b.pos == gateAt {
		select {
		case <-b.s.release:
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		}
	}
	end := int64(len(testModelBytes))
	if b.pos < gateAt {
		end = gateAt
	}
	n := copy(p, testModelBytes[b.pos:end])
	b.pos += int64(n)
	return n, nil
}

func (b *gatedBody) Close() error {
	b.s.closed.Add(1)
	return nil
}

// plentySpace is a disk with room for any test model.
type plentySpace struct{}

func (plentySpace) FreeBytes(context.Context) (int64, error) { return 1 << 40, nil }

// passSignal records that the transcription pass ran.
type passSignal chan struct{}

func (p passSignal) EnqueuePending(context.Context) (int, error) {
	select {
	case p <- struct{}{}:
	default:
	}
	return 0, nil
}

// noLinks opens nothing.
type noLinks struct{}

func (noLinks) OpenLink(context.Context, values.SourceURL) error { return nil }

// downloadFixture is a model manager whose catalog lists the test model, and the models page on it.
type downloadFixture struct {
	m      *modelManager
	src    *gatedSource
	passes passSignal
	page   *wails.ModelsService
}

func newDownloadFixture(t *testing.T) *downloadFixture {
	t.Helper()
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	c := a.core
	svc, err := modelcatalog.New(modelcatalog.Deps{
		Models: sqlite.NewModelRepository(c.db), Pins: sqlite.NewModelPins(c.db), Settings: sqlite.NewSettingsRepository(c.db),
		Files: c.store, Catalog: testCatalog{}, Clock: a.env.clock, IDs: a.env.ids, Tx: c.db,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &downloadFixture{src: &gatedSource{release: make(chan struct{})}, passes: make(passSignal, 1)}
	dl, err := modelcatalog.NewDownloader(svc, modelcatalog.DownloadDeps{Source: f.src, Space: plentySpace{}})
	if err != nil {
		t.Fatal(err)
	}

	f.m = newModelManager(&core{models: svc, downloads: dl}, f.passes, slog.New(slog.DiscardHandler), 10*time.Second)
	if _, err := f.m.markReady(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.m.close() }) // runs before the app's cleanup closes the storage
	f.page = wails.NewModelsService(wails.ModelsDeps{
		Catalog: f.m, Downloads: f.m, Imports: f.m, Links: noLinks{}, Picker: &modelPicker{},
	}, slog.New(slog.DiscardHandler))
	return f
}

// waitState waits until the download state satisfies cond and returns it.
func (f *downloadFixture) waitState(t *testing.T, what string, cond func(wails.DownloadState) bool) wails.DownloadState {
	t.Helper()
	var st wails.DownloadState
	testenv.Eventually(t, what, func() bool {
		st = f.m.DownloadState()
		return cond(st)
	}, nil)
	return st
}

// startStalled starts the download of the test model and waits until the source stalls at gateAt.
func (f *downloadFixture) startStalled(t *testing.T) {
	t.Helper()
	if err := f.m.StartDownload(testCatalogID); err != nil {
		t.Fatal(err)
	}
	f.waitState(t, "the download stalls at the gate", func(st wails.DownloadState) bool {
		return st.Running && st.Phase == modelcatalog.PhaseDownloading && st.DoneBytes == gateAt &&
			st.TotalBytes == int64(len(testModelBytes))
	})
}

func (f *downloadFixture) waitPass(t *testing.T) {
	t.Helper()
	select {
	case <-f.passes:
	case <-time.After(10 * time.Second):
		t.Fatal("no transcription pass after the install")
	}
}

func (f *downloadFixture) noPass(t *testing.T) {
	t.Helper()
	if len(f.passes) != 0 {
		t.Fatal("a transcription pass ran without an install")
	}
}

// One download at a time with its progress, then the installed model and the transcription pass (AC-07).
func TestModelDownloadInstallsAndRunsThePass(t *testing.T) {
	ctx := context.Background()
	f := newDownloadFixture(t)
	f.startStalled(t)

	if err := f.m.StartDownload(testCatalogID); !errors.Is(err, modelcatalog.ErrDownloadInProgress) {
		t.Fatalf("second StartDownload = %v, want ErrDownloadInProgress", err)
	}
	if err := f.page.StartDownload(ctx, string(testCatalogID)); err == nil || err.Error() != string(wails.ErrModelDownloading) {
		t.Fatalf("second StartDownload of the page = %v, want %q", err, wails.ErrModelDownloading)
	}
	view, err := f.page.List(ctx)
	if d := view.Download; err != nil || !d.Running || d.Phase != "downloading" || d.DoneBytes != gateAt || d.Error != "" {
		t.Fatalf("List = %+v, %v; want the running download at %d bytes", view.Download, err, gateAt)
	}
	f.noPass(t)

	f.src.open()
	st := f.waitState(t, "the download ends", func(st wails.DownloadState) bool { return !st.Running })
	size := int64(len(testModelBytes))
	if st.Phase != modelcatalog.PhaseInstalled || st.DoneBytes != size || st.TotalBytes != size || st.Err != nil || st.Cancelled {
		t.Fatalf("state %+v, want installed with all %d bytes", st, size)
	}
	f.waitPass(t)

	view, err = f.page.List(ctx)
	if err != nil || len(view.Installed) != 1 || view.Installed[0].CatalogID != string(testCatalogID) || !view.Installed[0].InUse {
		t.Fatalf("List = %+v, %v; want the downloaded model installed and in use", view.Installed, err)
	}
	if d := view.Download; d.Running || d.Phase != "installed" || d.Error != "" {
		t.Fatalf("download %+v, want installed", d)
	}
	if opened, closed := f.src.opened.Load(), f.src.closed.Load(); opened != 1 || closed != 1 {
		t.Fatalf("%d bodies opened, %d closed; want one, closed", opened, closed)
	}
}

// A cancelled download keeps its bytes, and starting it again resumes from them (PRD section 8: resume support).
func TestModelDownloadCancelKeepsThePartialFile(t *testing.T) {
	ctx := context.Background()
	f := newDownloadFixture(t)
	f.startStalled(t)

	f.m.CancelDownload()
	st := f.waitState(t, "the download stops", func(st wails.DownloadState) bool { return !st.Running })
	if !st.Cancelled || !errors.Is(st.Err, modelcatalog.ErrDownloadInterrupted) || st.DoneBytes != gateAt {
		t.Fatalf("state %+v, want cancelled at %d bytes", st, gateAt)
	}
	view, err := f.page.List(ctx)
	if d := view.Download; err != nil || d.Running || d.Phase != "cancelled" || d.Error != string(wails.ErrModelDownloadStopped) {
		t.Fatalf("download %+v, %v; want cancelled", view.Download, err)
	}
	plan, err := f.m.Preflight(ctx, testCatalogID)
	if err != nil || plan.HaveBytes != gateAt {
		t.Fatalf("Preflight = %+v, %v; want the %d bytes kept", plan, err, gateAt)
	}
	f.noPass(t)

	f.src.open()
	if err := f.m.StartDownload(testCatalogID); err != nil {
		t.Fatal(err)
	}
	st = f.waitState(t, "the resumed download ends", func(st wails.DownloadState) bool { return !st.Running })
	if st.Phase != modelcatalog.PhaseInstalled || st.Cancelled || st.Err != nil {
		t.Fatalf("state %+v, want installed", st)
	}
	if got := f.src.fetchOffsets(); len(got) != 2 || got[0] != 0 || got[1] != gateAt {
		t.Fatalf("fetch offsets %v, want 0 then the resume at %d", got, gateAt)
	}
	f.waitPass(t)
}

// close cuts a running download short and returns once its goroutines are gone; nothing starts afterwards.
func TestModelManagerCloseStopsADownload(t *testing.T) {
	f := newDownloadFixture(t)
	f.startStalled(t)

	// nil means the work group drained: the download and the progress goroutines both ended.
	if err := f.m.close(); err != nil {
		t.Fatal(err)
	}
	st := f.m.DownloadState()
	if st.Running || st.Cancelled || !errors.Is(st.Err, modelcatalog.ErrDownloadInterrupted) {
		t.Fatalf("state %+v, want interrupted by the shutdown", st)
	}
	if opened, closed := f.src.opened.Load(), f.src.closed.Load(); opened != 1 || closed != 1 {
		t.Fatalf("%d bodies opened, %d closed; want one, closed", opened, closed)
	}
	if err := f.m.StartDownload(testCatalogID); !errors.Is(err, errManagerClosed) {
		t.Fatalf("StartDownload after close = %v, want errManagerClosed", err)
	}
	f.noPass(t)
}
