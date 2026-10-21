package tests

// Scenario tests of the model download (TASK-027) on the real SQLite adapter, the real file store and the real HTTP
// adapter against an httptest TLS server; only the clock, the id generator and the catalog source are fakes (ARCH-61).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

var (
	_ modelcatalog.ModelSource = (*httpdownload.Client)(nil)
	_ modelcatalog.DiskSpace   = (*filestore.Store)(nil)
)

// fileDownload is the model the download scenarios fetch: large enough that half of it crosses many reads.
var fileDownload = modelFile{id: "base-q5_1", name: "base", content: bytes.Repeat([]byte("0123456789abcdef"), 64<<10)}

// modelServer serves one model file with Range support. When dropAt > 0, the first request without a Range header
// gets dropAt bytes under the full Content-Length and then a closed connection.
type modelServer struct {
	*httptest.Server

	mu     sync.Mutex
	body   []byte
	dropAt int
	ranges []string
}

func newModelServer(t *testing.T, body []byte, dropAt int) *modelServer {
	t.Helper()
	s := &modelServer{body: body, dropAt: dropAt}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *modelServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	body, drop := s.body, s.dropAt
	if r.Header.Get("Range") == "" {
		s.dropAt = 0
	}
	s.mu.Unlock()

	if drop == 0 || r.Header.Get("Range") != "" {
		http.ServeContent(w, r, "model.bin", time.Time{}, bytes.NewReader(body))
		return
	}

	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body[:drop])
	_ = http.NewResponseController(w).Flush()
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func (s *modelServer) requestedRanges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

// manifest is the catalog of one model downloadable from this server.
func (s *modelServer) manifest(f modelFile) []byte {
	return []byte(fmt.Sprintf(`{"schema_version": 1, "models": [{"id": %q, "name": %q, "version": "1",
		"size_bytes": %d, "sha256": %q, "source_url": "%s/models/%s.bin", "quantization": "q5_1"}]}`,
		f.id, f.name, len(f.content), f.sum(), s.URL, f.id))
}

// downloadEnv is one run of the app with the model manager and the downloader.
type downloadEnv struct {
	t     *testing.T
	dir   string
	db    *sqlite.DB
	files *filestore.Store
	svc   *modelcatalog.Service
	dl    *modelcatalog.Downloader
}

func openDownloadEnv(t *testing.T, dir string, srv *modelServer, ids *seqIDs) *downloadEnv {
	t.Helper()
	db, err := openSQLite(context.Background(), t, sqlite.Options{Path: filepath.Join(dir, "app.db"), Migrate: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	files, err := filestore.New(filepath.Join(dir, "data"))
	if err != nil {
		_ = db.Close()
		t.Fatalf("open file store: %v", err)
	}
	e := &downloadEnv{t: t, dir: dir, db: db, files: files}

	e.svc, err = modelcatalog.New(modelcatalog.Deps{
		Models: sqlite.NewModelRepository(db), Pins: sqlite.NewModelPins(db), Settings: sqlite.NewSettingsRepository(db),
		Files: files, Catalog: staticCatalog(srv.manifest(fileDownload)), Clock: testenv.NewFakeClock(catalogT0),
		IDs: ids, Tx: db,
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	e.dl, err = modelcatalog.NewDownloader(e.svc, modelcatalog.DownloadDeps{
		Source: httpdownload.New(httpdownload.WithHTTPClient(srv.Client())), Space: files,
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	return e
}

func (e *downloadEnv) close() {
	if err := e.files.Close(); err != nil {
		e.t.Errorf("close file store: %v", err)
	}
	if err := e.db.Close(); err != nil {
		e.t.Errorf("close database: %v", err)
	}
}

// modelFiles lists every file below the models directory of the data root.
func (e *downloadEnv) modelFiles() []string {
	e.t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(e.dir, "data", "models"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// TestModelDownloadResumesAfterDrop is test step 2 of TASK-027: the server drops the connection at 50%, the app
// restarts, the repeated download resumes the file with a ranged request and the checksum matches.
func TestModelDownloadResumesAfterDrop(t *testing.T) {
	ctx := context.Background()
	dir, ids := t.TempDir(), &seqIDs{}
	half := len(fileDownload.content) / 2
	srv := newModelServer(t, fileDownload.content, half)

	e := openDownloadEnv(t, dir, srv, ids)
	id := values.CatalogID(fileDownload.id)
	plan, err := e.dl.Preflight(ctx, id)
	if err != nil || !plan.Enough || plan.HaveBytes != 0 {
		t.Fatalf("Preflight = %+v, %v", plan, err)
	}
	if _, err := e.dl.Download(ctx, id); !errors.Is(err, modelcatalog.ErrDownloadInterrupted) {
		t.Fatalf("first Download = %v, want ErrDownloadInterrupted", err)
	}
	if installed, err := e.svc.ListInstalled(ctx); err != nil || len(installed) != 0 {
		t.Fatalf("installed after a broken download: %+v, %v", installed, err)
	}
	e.close()

	e = openDownloadEnv(t, dir, srv, ids)
	defer e.close()
	plan, err = e.dl.Preflight(ctx, id)
	if err != nil || plan.HaveBytes != int64(half) {
		t.Fatalf("Preflight after restart = %+v, %v; want %d bytes kept", plan, err, half)
	}

	events, unsubscribe := e.dl.Subscribe()
	defer unsubscribe()
	m, err := e.dl.Download(ctx, id)
	if err != nil {
		t.Fatalf("resumed Download = %v", err)
	}
	if got, want := srv.requestedRanges(), []string{"", fmt.Sprintf("bytes=%d-", half)}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requests = %q, want %q", got, want)
	}
	if m.SHA256 != fileDownload.sum() || m.SizeBytes != int64(len(fileDownload.content)) {
		t.Fatalf("installed %+v, want the digest of the file", m)
	}
	if p := <-events; p.Phase != modelcatalog.PhaseInstalled {
		t.Fatalf("last progress = %+v", p)
	}

	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 1 || installed[0].ID != m.ID || installed[0].CatalogID != id {
		t.Fatalf("ListInstalled = %+v, %v", installed, err)
	}
	rc, err := e.files.Open(ctx, installedRef(t, "base-q5_1-model-1.bin"))
	if err != nil {
		t.Fatalf("installed file: %v", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(buf.Bytes(), fileDownload.content) {
		t.Fatalf("installed file differs from the source: %v", err)
	}
	if files := e.modelFiles(); len(files) != 1 {
		t.Fatalf("model files = %v, want only the installed one", files)
	}
}

// TestModelDownloadRejectsTamperedFile is test step 3 of TASK-027: a file that does not match the catalog SHA-256 is
// rejected and deleted, and the model does not appear among the installed models.
func TestModelDownloadRejectsTamperedFile(t *testing.T) {
	ctx := context.Background()
	tampered := bytes.Clone(fileDownload.content)
	tampered[len(tampered)/3] ^= 0xff
	srv := newModelServer(t, tampered, 0)

	e := openDownloadEnv(t, t.TempDir(), srv, &seqIDs{})
	defer e.close()

	_, err := e.dl.Download(ctx, values.CatalogID(fileDownload.id))
	if !errors.Is(err, values.ErrChecksumMismatch) {
		t.Fatalf("Download = %v, want ErrChecksumMismatch", err)
	}
	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 0 {
		t.Fatalf("ListInstalled = %+v, %v; want none", installed, err)
	}
	available, err := e.svc.ListAvailable(ctx, values.Machine{})
	if err != nil || len(available) != 1 || available[0].InstalledID != "" {
		t.Fatalf("ListAvailable = %+v, %v; want the entry not installed", available, err)
	}
	if files := e.modelFiles(); len(files) != 0 {
		t.Fatalf("model files left: %v", files)
	}
}

func installedRef(t *testing.T, name string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactModels, name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
